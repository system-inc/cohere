import CryptoKit
import Foundation

/*
 A fingerprint of everything a `swift build` reads, so the types phase can tell that nothing changed since the
 last build that succeeded and read that build's compiler records instead of building again. The no-op build
 costs 5.1s on ahraos-macos and 7.0s on ahraos-presence warm, most of a warm run; the walk below costs 7ms
 and 39ms.

 What goes in, and why each is enough:

 - every file under each package root and each path dependency, by path, size and modification time, hidden
   directories and `.build` left out. That is the signal SwiftPM's own incremental build trusts, so this is
   exactly as safe as the build it replaces, and it catches what a source-only walk would miss: a header
   outside a target, a resource, `Packages/` from `swift package edit`;
 - the root's `Package.resolved` by content, which pins every remote dependency's checkout;
 - any other file a build reads that is rewritten whatever it says, by content, since its time would change on
   every run: an Xcode project xcodegen generates into the scratch afresh each run (`XcodeTypesPhase`);
 - the toolchain and the build's own arguments, so a toolchain change or `--no-fix`'s disabled resolution is
   a different build.

 The fingerprint is taken before a build starts and stored only after every build succeeded, so an edit made
 while a build ran, or a build that failed or was killed, always means building next time. A cache that
 never invalidates reports the last answer forever, which on a measurement looks exactly like a clean tree,
 so the test that matters is warm-after-a-change (`BuildInputSnapshotTests`, and the pipeline control that
 plants a type error after a warm run).
 */
struct BuildInputSnapshot: Equatable, Codable {
    let fingerprint: String
    /* The toolchain, the build's arguments, the resolved pins and each file taken by content, one line each. */
    let settings: [String]
    /* Every file the build reads, by path, to its size and modification time. */
    let files: [String: String]
    /*
     Each owned Swift file's `InterfaceFingerprint` as the build about to run compiles it, by resolved path. The
     types phase fills it in before building; it is what lets a later run tell that an edit stayed inside
     function bodies.
     */
    var interfaces: [String: String] = [:]
    /*
     Each owned Swift file's bytes as the same build compiles them, as a content hash by resolved path, filled
     in beside `interfaces`. It is what lets a file whose time moved while its bytes did not, an edit undone,
     count as unchanged (`changes(since:)`). Time and size alone never matched again after an edit and its
     revert, so every run until the next interface build checked the reverted file in sourcekitd once more.
     */
    var contents: [String: String] = [:]

    /* Equal when the build would read the same inputs. The interfaces and contents are derived from those inputs, so they never decide. */
    static func == (left: BuildInputSnapshot, right: BuildInputSnapshot) -> Bool {
        left.fingerprint == right.fingerprint
    }

    static func take(
        roots: [URL],
        resolved: URL,
        toolchain: String,
        arguments: [String],
        contents: [URL] = [],
    ) -> BuildInputSnapshot {
        let settings =
            [
                "toolchain\t\(toolchain)", "arguments\t\(arguments.joined(separator: " "))",
                "resolved\t\(PackageDescriptionCache.fingerprint(of: resolved))",
            ] + contents.map { "content\t\($0.path)\t\(PackageDescriptionCache.fingerprint(of: $0))" }
        var files: [String: String] = [:]
        let keys: [URLResourceKey] = [.isDirectoryKey, .fileSizeKey, .contentModificationDateKey]
        for root in roots {
            let walker = FileManager.default.enumerator(
                at: root,
                includingPropertiesForKeys: keys,
                options: [.skipsHiddenFiles],
            )
            while let item = walker?.nextObject() as? URL {
                guard let values = try? item.resourceValues(forKeys: Set(keys)) else {
                    files[item.path] = "unreadable"
                    continue
                }
                if values.isDirectory == true {
                    if item.lastPathComponent == ".build" {
                        walker?.skipDescendants()
                    }
                    continue
                }
                let modified = values.contentModificationDate?.timeIntervalSinceReferenceDate ?? -1
                files[item.path] = "\(values.fileSize ?? -1)\t\(modified)"
            }
        }
        let lines = settings + files.map { "\($0.key)\t\($0.value)" }.sorted()
        let digest = SHA256.hash(data: Data(lines.joined(separator: "\n").utf8))
        return BuildInputSnapshot(
            fingerprint: digest.map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }.joined(),
            settings: settings,
            files: files,
        )
    }

    /* What moved since an earlier snapshot, when only files did. */
    struct Changes: Equatable {
        /* Files whose time or size moved and whose bytes are not the ones `earlier` recorded, or that it recorded no bytes for, by walked path. */
        var edited: Set<String> = []
        /* Files whose time or size moved while their bytes are the ones `earlier` recorded, by resolved path, to the modification time `earlier` saw. */
        var restored: [String: Date] = [:]
        /* Files `earlier` walked that are gone, by walked path, when the file set may change. */
        var removed: Set<String> = []
    }

    /*
     The files that changed since `earlier`, when nothing else did: the same settings and the same set of
     files, only some of them edited. Nil when anything else moved (a file added or removed, a pin, the
     toolchain), because then only a build can say what the compiler thinks.

     A file whose bytes are again the ones the build compiled is restored, not edited: the build's records
     describe it exactly, judged against the time the build saw it at. Only owned Swift files carry recorded
     bytes, so any other file that moved stays edited, and the types phase builds for it.
     */
    func changes(since earlier: BuildInputSnapshot, allowingFileSetChanges: Bool = false) -> Changes? {
        guard settings == earlier.settings,
            allowingFileSetChanges || files.count == earlier.files.count
        else { return nil }
        var changes = Changes()
        /* A file gone, which only a scope can place, by the folder it left (`TypesPhase.scope(of:)`). */
        changes.removed = Set(earlier.files.keys.filter { files[$0] == nil })
        guard allowingFileSetChanges || changes.removed.isEmpty else { return nil }
        for (path, entry) in files {
            guard let before = earlier.files[path] else {
                /* A file added, which a scope takes as an edit of the target it joins (`TypesPhase.scope(of:)`). */
                guard allowingFileSetChanges else { return nil }
                changes.edited.insert(path)
                continue
            }
            guard before != entry else { continue }
            let resolved = URL(fileURLWithPath: path).resolvingSymlinksInPath()
            if let recorded = earlier.contents[resolved.path],
                PackageDescriptionCache.fingerprint(of: resolved) == recorded,
                let seconds = before.split(separator: "\t").last.flatMap({ Double($0) })
            {
                changes.restored[resolved.path] = Date(timeIntervalSinceReferenceDate: seconds)
            }
            else {
                changes.edited.insert(path)
            }
        }
        return changes
    }

    /*
     Which snapshot a store holds. `lastSuccess` is the last build or check that succeeded, whose records a run may read
     as its own. `lastRecorded` is the last check that left every file it checked a record, errors and all, which a
     check after an edit takes its scope from (`TypesPhase.scope(of:)`): a check, unlike a build, records every file
     even when a job fails, so after an error the next check looks again only at what changed since.
     */
    enum Kind: String {
        case lastSuccess = "build-inputs.json"
        case lastRecorded = "check-inputs.json"
    }

    /* Where a snapshot of this kind is kept, beside that build or check in the engine's scratch. */
    static func storeFile(scratchPath: URL, kind: Kind = .lastSuccess) -> URL {
        scratchPath.appendingPathComponent(kind.rawValue)
    }

    /* The stored snapshot of this kind. An older engine's store, a bare digest, does not decode and reads as none: the next run builds. */
    static func stored(scratchPath: URL, kind: Kind = .lastSuccess) -> BuildInputSnapshot? {
        guard let data = try? Data(contentsOf: storeFile(scratchPath: scratchPath, kind: kind)), !data.isEmpty else {
            return nil
        }
        return try? JSONDecoder().decode(BuildInputSnapshot.self, from: data)
    }

    func store(scratchPath: URL, kind: Kind = .lastSuccess) {
        do {
            try FileManager.default.createDirectory(at: scratchPath, withIntermediateDirectories: true)
            try JSONEncoder().encode(self).write(
                to: Self.storeFile(scratchPath: scratchPath, kind: kind),
                options: .atomic,
            )
        }
        catch {
            /* Not stored, so the next run builds: slower, and still right. */
        }
    }

    /*
     Forgotten after a build that did not succeed, so the next run cannot read a failed build's records as
     current. Overwritten with nothing rather than removed: an empty store reads as no snapshot, and a removal
     that failed would have left the old fingerprint standing.
     */
    static func forget(scratchPath: URL, kind: Kind = .lastSuccess) {
        let file = storeFile(scratchPath: scratchPath, kind: kind)
        guard FileManager.default.fileExists(atPath: file.path) else { return }
        do {
            try Data().write(to: file, options: .atomic)
        }
        catch {
            /* Only an unwritable scratch reaches here, and then nothing can be stored either, so the next run builds. */
        }
    }
}
