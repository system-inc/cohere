import CryptoKit
import Foundation

/*
 The paths git's index tracks, read from the index file itself with no git process (#08cv2mp, under Kirk's ruling
 #67vevpz), so a tracked file is never ignored, as `git ls-files` and `git check-ignore` have it: tracking outranks
 every ignore pattern, and a pre-commit hook on a force-added file checks that file.

 The format is git's (`gitformat-index`), read as `read-cache.c` reads it:
 - A 12-byte header: `DIRC`, the version, 2, 3 or 4, and the number of entries, each 32-bit and big-endian.
 - Each entry: ten 32-bit stat and mode fields, the object's name (20 bytes, or 32 in a repository whose
   `extensions.objectformat` is sha256), 16 bits of flags (assume-valid, extended, a 2-bit stage and a 12-bit name
   length, 0xFFF meaning that long or longer), then from version 3 a second 16 bits when the extended flag is set.
 - In versions 2 and 3 the path follows, ending in a NUL and padded with more until the entry is a multiple of 8
   bytes. In version 4 it is prefix-compressed and never padded: a varint counting the bytes to drop from the end of
   the previous entry's path, then the rest of this one's, ending in a NUL. The varint is git's own offset encoding
   (`varint.c`), in which each continuation adds one before shifting, not LEB128.
 - The extensions follow, each a 4-byte signature and a 32-bit size, then a checksum of everything before it. A
   checksum of zeros is an index written with `index.skipHash`, and git reads it unchecked too.

 Any stage counts as tracked: a path mid-merge is held at stages 1 to 3, and listed once. An intent-to-add entry
 and a skip-worktree one are tracked as well, since git lists both.

 What this does not read it says rather than guessing, and the caller falls back to the ignore files alone:
 - a split index (the `link` extension), whose entries live in a shared index file beside it;
 - a sparse index (a directory entry, mode 040000, or the `sdir` extension), whose collapsed directories hide the
   paths below them;
 - an index failing any check: its checksum, its header, its version, an entry or extension running past the end,
   a name length its flags disagree with, or extended flags in version 2, which has none.
 */
struct GitIndex: Equatable, Sendable {
    /* Every path the index tracks, relative to the work tree's root, as git spells it. */
    var trackedFiles: Set<String>
    /* Every directory above a tracked path, relative to the root, so a walk knows which ignored ones hold one. */
    var trackedDirectories: Set<String>

    /* An index read, or why it was not, in words a note can carry. */
    enum Reading: Equatable, Sendable {
        case read(GitIndex)
        case fellBack(reason: String)
    }

    /* What a repository whose index was not read tracks, as far as the walk can know: nothing. */
    static let empty = GitIndex(trackedFiles: [], trackedDirectories: [])

    init(trackedFiles: Set<String>, trackedDirectories: Set<String>) {
        self.trackedFiles = trackedFiles
        self.trackedDirectories = trackedDirectories
    }

    /*
     From paths in the index's sorted order, in which a path's directory is most often the one before's, so the
     directories above a path are added only where it is not: cohere's own index holds 11,819 paths in 2,830
     directories.
     */
    init(paths: [[UInt8]]) {
        var files = Set<String>(minimumCapacity: paths.count)
        var directories = Set<String>()
        var previousDirectory: ArraySlice<UInt8> = []
        for path in paths {
            files.insert(String(decoding: path, as: UTF8.self))
            guard let lastSlash = path.lastIndex(of: IgnoreRules.Byte.slash) else { continue }
            let directory = path[..<lastSlash]
            if directory.elementsEqual(previousDirectory) {
                continue
            }
            previousDirectory = directory
            for (offset, byte) in directory.enumerated() where byte == IgnoreRules.Byte.slash {
                directories.insert(String(decoding: directory[..<offset], as: UTF8.self))
            }
            directories.insert(String(decoding: directory, as: UTF8.self))
        }
        self.init(trackedFiles: files, trackedDirectories: directories)
    }

    /* The bytes an object's name takes in an `extensions.objectformat` of `objectFormat`, or nil for one git lacks. */
    static func hashSize(objectFormat: String) -> Int? {
        switch objectFormat.lowercased() {
            case "sha1": 20
            case "sha256": 32
            default: nil
        }
    }

    /* The index at `file`, in a repository whose objects are named in `objectFormat`. */
    static func read(file: URL, objectFormat: String) -> Reading {
        guard let hashSize = hashSize(objectFormat: objectFormat) else {
            return .fellBack(reason: "the repository's object format is \(objectFormat), which is not sha1 or sha256")
        }
        guard FileManager.default.fileExists(atPath: file.path) else {
            return .fellBack(reason: "there is no index at \(file.path)")
        }
        guard let contents = FileManager.default.contents(atPath: file.path) else {
            return .fellBack(reason: "the index at \(file.path) could not be read")
        }
        switch parse(Array(contents), hashSize: hashSize) {
            case .read(let index):
                return .read(index)
            case .fellBack(let reason):
                return .fellBack(reason: "the index at \(file.path) \(reason)")
        }
    }

    /* An index's bytes, whose object names take `hashSize` bytes. A reason reads after "the index at <path>". */
    static func parse(_ bytes: [UInt8], hashSize: Int) -> Reading {
        let headerSize = 12
        guard bytes.count >= headerSize + hashSize else {
            return .fellBack(reason: "is \(bytes.count) bytes, too short to be an index")
        }
        let end = bytes.count - hashSize
        let trailer = bytes[end...]
        if trailer.contains(where: { $0 != 0 }) && !Array(trailer).elementsEqual(checksum(bytes[..<end], hashSize)) {
            return .fellBack(reason: "does not match its checksum")
        }
        guard bytes.starts(with: Array("DIRC".utf8)) else {
            return .fellBack(reason: "does not begin with DIRC")
        }
        let version = word(bytes, at: 4)
        guard (2...4).contains(version) else {
            return .fellBack(reason: "is version \(version), and only 2, 3 and 4 are read")
        }
        let entryCount = Int(word(bytes, at: 8))
        /* The stat fields, the mode among them, then the object's name, then at least the flags and a NUL. */
        let statSize = 40
        let modeOffset = 24
        guard entryCount <= (end - headerSize) / (statSize + hashSize + 3) else {
            return .fellBack(reason: "counts \(entryCount) entries, more than its \(bytes.count) bytes can hold")
        }

        var offset = headerSize
        var previousPath: [UInt8] = []
        /*
         A split index writes an entry that replaces one in its shared index with no path, and says it is split only
         in its `link` extension, after the entries. So the first nameless entry is held until the extensions say
         which it is: split, or broken.
         */
        var firstNamelessEntry: Int?
        var paths: [[UInt8]] = []
        paths.reserveCapacity(entryCount)
        for entryNumber in stride(from: 1, through: entryCount, by: 1) {
            let entryStart = offset
            let flagsOffset = entryStart + statSize + hashSize
            guard flagsOffset + 2 <= end else {
                return .fellBack(reason: "ends inside entry \(entryNumber) of \(entryCount)")
            }
            if word(bytes, at: entryStart + modeOffset) == 0o040000 {
                return .fellBack(reason: "is a sparse index, whose collapsed directories hide the paths below them")
            }
            let flags = Int(bytes[flagsOffset]) << 8 | Int(bytes[flagsOffset + 1])
            let isExtended = flags & 0x4000 != 0
            if isExtended && version == 2 {
                return .fellBack(reason: "is version 2, which has no extended flags, yet entry \(entryNumber) has")
            }
            let flagsSize = isExtended ? 4 : 2
            let declaredLength = flags & 0xFFF
            var pathStart = flagsOffset + flagsSize
            guard pathStart <= end else {
                return .fellBack(reason: "ends inside entry \(entryNumber)'s flags")
            }
            let path: [UInt8]
            if version == 4 {
                guard let stripped = varint(bytes, at: pathStart, before: end), stripped.value <= previousPath.count
                else {
                    return .fellBack(reason: "gives entry \(entryNumber) a prefix it cannot take from the path before")
                }
                pathStart = stripped.next
                guard let terminator = bytes[pathStart..<end].firstIndex(of: 0) else {
                    return .fellBack(reason: "ends inside entry \(entryNumber)'s path")
                }
                path = Array(previousPath.dropLast(stripped.value)) + bytes[pathStart..<terminator]
                offset = terminator + 1
            }
            else {
                guard let terminator = bytes[pathStart..<end].firstIndex(of: 0) else {
                    return .fellBack(reason: "ends inside entry \(entryNumber)'s path")
                }
                path = Array(bytes[pathStart..<terminator])
                offset = entryStart + ((statSize + hashSize + flagsSize + path.count + 8) & ~7)
                guard offset <= end else {
                    return .fellBack(reason: "ends inside entry \(entryNumber)'s padding")
                }
            }
            guard declaredLength == 0xFFF ? path.count >= 0xFFF : path.count == declaredLength else {
                return .fellBack(reason: "gives entry \(entryNumber) a path its name length disagrees with")
            }
            if path.isEmpty {
                firstNamelessEntry = firstNamelessEntry ?? entryNumber
            }
            else {
                paths.append(path)
            }
            previousPath = path
        }

        /* The extensions, read only for their signatures, until exactly the checksum is left. */
        while offset < end {
            guard offset + 8 <= end else {
                return .fellBack(reason: "ends inside an extension's header")
            }
            let signature = String(decoding: bytes[offset..<(offset + 4)], as: UTF8.self)
            if signature == "link" {
                return .fellBack(reason: "is a split index, whose entries live in a shared index beside it")
            }
            if signature == "sdir" {
                return .fellBack(reason: "is a sparse index, whose collapsed directories hide the paths below them")
            }
            let size = Int(word(bytes, at: offset + 4))
            guard size <= end - offset - 8 else {
                return .fellBack(reason: "ends inside its \(signature) extension")
            }
            offset += 8 + size
        }
        if let firstNamelessEntry {
            return .fellBack(reason: "gives entry \(firstNamelessEntry) no path, and is no split index")
        }
        return .read(GitIndex(paths: paths))
    }

    /* The 32-bit big-endian word at `offset`, which the caller has checked lies within `bytes`. */
    static func word(_ bytes: [UInt8], at offset: Int) -> UInt32 {
        bytes[offset..<(offset + 4)].reduce(0) { $0 << 8 | UInt32($1) }
    }

    /* git's `decode_varint` from `offset`: the value and where the next bytes begin, or nil past `end` or 64 bits. */
    static func varint(_ bytes: [UInt8], at offset: Int, before end: Int) -> (value: Int, next: Int)? {
        var index = offset
        guard index < end else { return nil }
        var byte = bytes[index]
        var value = Int(byte & 0x7F)
        while byte & 0x80 != 0 {
            index += 1
            guard index < end, value < Int.max >> 8 else { return nil }
            byte = bytes[index]
            value = ((value + 1) << 7) | Int(byte & 0x7F)
        }
        return (value, index + 1)
    }

    /* The checksum git writes after an index: SHA-1, or SHA-256 in a sha256 repository. */
    static func checksum(_ bytes: ArraySlice<UInt8>, _ hashSize: Int) -> [UInt8] {
        hashSize == 32 ? Array(SHA256.hash(data: Data(bytes))) : Array(Insecure.SHA1.hash(data: Data(bytes)))
    }
}
