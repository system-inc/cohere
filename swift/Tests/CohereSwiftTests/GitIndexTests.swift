import CryptoKit
import Foundation
import Testing

@testable import CohereSwift

/*
 git's index, read with no git process, from bytes real git wrote.

 The shared fixtures in `internal/gitignore/testdata/index` at the repository's root are the ones cohere's Go reader
 reads too, each `version-N.index` beside the `version-N.paths` it must yield; their README says how git made them.
 Reading the same bytes keeps the two readers from drifting.

 The samples in `Tests/GitIndexSamples` hold what the shared ones lack, written by git 2.54 in a repository whose
 `.gitignore` says `*.swift` and `Ignored/`, after `git add .gitignore`, `git add -f Sources Ignored/Tracked.swift`
 and a commit:
 - `sha256-version-2.index`, `-3` and `-4`, from a repository made with `git init --object-format=sha256`: version 3
   after `git update-index --skip-worktree Sources/Beta.swift` and `--index-version 3`, version 4 after
   `--index-version 4` on that;
 - `sha1-sparse.index` from a clone after `git sparse-checkout init --cone --sparse-index` and
   `git sparse-checkout set Sources`, which collapses `Ignored/` into one directory entry;
 - `sha1-split.index` from a clone after `git update-index --split-index`, whose entries moved to a shared index.
 Each lives outside the test target, as the contract fixtures do, so SwiftPM never asks to bundle it.
 */
struct GitIndexTests {
    static let samples = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("GitIndexSamples", isDirectory: true)

    static let sharedFixtures = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("internal/gitignore/testdata/index", isDirectory: true)

    static let sampleTracked: Set<String> = [
        ".gitignore", "Ignored/Tracked.swift", "Sources/Alpha/One.swift", "Sources/Alpha/Two.swift",
        "Sources/Beta.swift",
    ]

    static func sample(_ name: String) -> URL {
        samples.appendingPathComponent(name)
    }

    static func sharedIndex(version: Int) -> URL {
        sharedFixtures.appendingPathComponent("version-\(version).index")
    }

    /* The paths a shared fixture must yield, one a line, in the index's order. */
    static func sharedPaths(version: Int) throws -> [String] {
        let text = try String(
            contentsOf: sharedFixtures.appendingPathComponent("version-\(version).paths"),
            encoding: .utf8,
        )
        return text.split(separator: "\n").map(String.init)
    }

    /*
     An index git would read, tracking `paths` at stage 0 with every stat field and object name zero: git's own
     layout, written here so a test can name the paths it needs. Version 4 compresses each path against the one
     before it, with git's `encode_varint`.
     */
    static func indexBytes(paths: [String], version: UInt32 = 2) -> Data {
        var bytes = Array("DIRC".utf8)
        bytes += bigEndian(version) + bigEndian(UInt32(paths.count))
        var previous: [UInt8] = []
        for path in paths.sorted() {
            let name = Array(path.utf8)
            var entry = [UInt8](repeating: 0, count: 40 + 20)
            entry.replaceSubrange(24..<28, with: bigEndian(0o100644))
            let length = min(name.count, 0xFFF)
            entry += [UInt8(length >> 8), UInt8(length & 0xFF)]
            if version == 4 {
                let shared = zip(previous, name).prefix { $0 == $1 }.count
                entry += varint(previous.count - shared) + name[shared...] + [0]
            }
            else {
                entry += name
                entry += [UInt8](repeating: 0, count: ((62 + name.count + 8) & ~7) - entry.count)
            }
            bytes += entry
            previous = name
        }
        bytes += Array(Insecure.SHA1.hash(data: Data(bytes)))
        return Data(bytes)
    }

    static func bigEndian(_ value: UInt32) -> [UInt8] {
        [UInt8(value >> 24 & 0xFF), UInt8(value >> 16 & 0xFF), UInt8(value >> 8 & 0xFF), UInt8(value & 0xFF)]
    }

    /* git's `encode_varint`: seven bits a byte, most significant first, each continuation one less than it reads. */
    static func varint(_ value: Int) -> [UInt8] {
        var remaining = value
        var encoded = [UInt8(remaining & 0x7F)]
        remaining >>= 7
        while remaining > 0 {
            remaining -= 1
            encoded.insert(0x80 | UInt8(remaining & 0x7F), at: 0)
            remaining >>= 7
        }
        return encoded
    }

    static func tracked(_ reading: GitIndex.Reading) -> Set<String>? {
        guard case .read(let index) = reading else { return nil }
        return index.trackedFiles
    }

    static func reason(_ reading: GitIndex.Reading) -> String? {
        guard case .fellBack(let reason) = reading else { return nil }
        return reason
    }

    /* Every path the fixture's list names and no other, each once, stages 1 to 3 of a conflict among them. */
    @Test(arguments: [2, 3, 4])
    func everySharedFixtureYieldsItsPaths(version: Int) throws {
        let file = Self.sharedIndex(version: version)
        #expect(GitIndex.word(Array(try Data(contentsOf: file)), at: 4) == UInt32(version))
        let paths = try Self.sharedPaths(version: version)
        #expect(Set(paths).count == paths.count && !paths.isEmpty, "the list is not one path a line")
        let reading = GitIndex.read(file: file, objectFormat: "sha1")
        guard case .read(let index) = reading else {
            Issue.record("fell back: \(String(describing: Self.reason(reading)))")
            return
        }
        #expect(index.trackedFiles == Set(paths))
    }

    @Test(arguments: [2, 3, 4])
    func everySha256SampleReadsAsGitListsIt(version: Int) throws {
        let objectFormat = "sha256"
        let file = Self.sample("\(objectFormat)-version-\(version).index")
        let bytes = try Data(contentsOf: file)
        #expect(
            GitIndex.word(Array(bytes), at: 4) == UInt32(version),
            "the sample is not the version it is named for",
        )
        let reading = GitIndex.read(file: file, objectFormat: objectFormat)
        guard case .read(let index) = reading else {
            Issue.record("fell back: \(String(describing: Self.reason(reading)))")
            return
        }
        #expect(index.trackedFiles == Self.sampleTracked)
        #expect(index.trackedDirectories == ["Ignored", "Sources", "Sources/Alpha"])
    }

    /* Paths sharing prefixes, and a strip past 127 bytes, which takes git's varint two bytes and its quirk of one. */
    @Test func versionFourPrefixCompressionReadsExactly() throws {
        let long = String(repeating: "Deep/", count: 30)
        let paths = ["Sources/Alpha/One.swift", "Sources/Alpha/Two.swift", "Sources/Beta.swift", long + "A.swift", "Z"]
        #expect(Self.varint(150).count == 2)
        let bytes = Array(Self.indexBytes(paths: paths, version: 4))
        #expect(Self.tracked(GitIndex.parse(bytes, hashSize: 20)) == Set(paths))
        #expect(Self.tracked(GitIndex.parse(Array(Self.indexBytes(paths: paths)), hashSize: 20)) == Set(paths))
    }

    @Test func aSplitOrSparseIndexFallsBackAndSaysWhich() {
        let split = GitIndex.read(file: Self.sample("sha1-split.index"), objectFormat: "sha1")
        #expect(
            Self.reason(split)?.hasSuffix("is a split index, whose entries live in a shared index beside it") == true
        )
        let sparse = GitIndex.read(file: Self.sample("sha1-sparse.index"), objectFormat: "sha1")
        #expect(
            Self.reason(sparse)?.hasSuffix("is a sparse index, whose collapsed directories hide the paths below them")
                == true
        )
    }

    /* Every way an index can be wrong falls back with its reason, and none crashes. */
    @Test func aGarbageIndexFallsBackWithItsReason() throws {
        let good = try Array(Data(contentsOf: Self.sharedIndex(version: 4)))
        var flipped = good
        flipped[40] ^= 0xFF
        var unsigned = Array("DIRX".utf8) + good[4..<(good.count - 20)]
        unsigned += Array(Insecure.SHA1.hash(data: Data(unsigned)))
        var versionFive = Array(good[..<(good.count - 20)])
        versionFive[7] = 5
        versionFive += Array(Insecure.SHA1.hash(data: Data(versionFive)))
        var overcounted = Array(Self.indexBytes(paths: ["A.swift"]).dropLast(20))
        overcounted[11] = 2
        overcounted += Array(Insecure.SHA1.hash(data: Data(overcounted)))
        var shortOfEntries = Array(Self.indexBytes(paths: [String(repeating: "Long/", count: 20)]).dropLast(20))
        shortOfEntries[11] = 2
        shortOfEntries += Array(Insecure.SHA1.hash(data: Data(shortOfEntries)))
        var nameless = Array(Self.indexBytes(paths: ["A"]).dropLast(20))
        nameless.replaceSubrange(72..<75, with: [0, 0, 0])
        nameless += Array(Insecure.SHA1.hash(data: Data(nameless)))
        let cases: [(bytes: [UInt8], reason: String)] = [
            ([], "is 0 bytes, too short to be an index"),
            (Array("not an index at all, only a few words".utf8), "does not match its checksum"),
            (Array(good.dropLast(1)), "does not match its checksum"),
            (flipped, "does not match its checksum"),
            (unsigned, "does not begin with DIRC"),
            (versionFive, "is version 5, and only 2, 3 and 4 are read"),
            (overcounted, "counts 2 entries, more than its 104 bytes can hold"),
            (shortOfEntries, "ends inside entry 2 of 2"),
            (nameless, "gives entry 1 no path, and is no split index"),
        ]
        for (bytes, reason) in cases {
            #expect(Self.reason(GitIndex.parse(bytes, hashSize: 20)) == reason)
        }
        /* The sha1 sample read as sha256 takes the wrong bytes for its checksum. */
        #expect(Self.reason(GitIndex.parse(good, hashSize: 32)) == "does not match its checksum")
        let unknown = GitIndex.read(file: Self.sharedIndex(version: 2), objectFormat: "md5")
        #expect(Self.reason(unknown) == "the repository's object format is md5, which is not sha1 or sha256")
        let missing = Self.samples.appendingPathComponent("absent.index")
        #expect(
            Self.reason(GitIndex.read(file: missing, objectFormat: "sha1")) == "there is no index at \(missing.path)"
        )
    }

    /* `index.skipHash` writes zeros where the checksum goes, and git reads such an index unchecked. */
    @Test func aChecksumOfZerosIsReadUnchecked() {
        var bytes = Array(Self.indexBytes(paths: ["A.swift"]))
        bytes.replaceSubrange((bytes.count - 20)..., with: [UInt8](repeating: 0, count: 20))
        #expect(Self.tracked(GitIndex.parse(bytes, hashSize: 20)) == ["A.swift"])
    }
}
