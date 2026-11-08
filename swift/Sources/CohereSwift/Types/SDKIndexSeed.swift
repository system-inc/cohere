import CryptoKit
import Foundation

/*
 The SDK's index units, kept once per toolchain and SDK outside every project, so a check into an empty index store
 does not index the SDK again (#f6pqraj, ruled 2026-11-07 by system_cohere and cache).

 A check indexes every module its files import, the SDK's among them (`-index-system-modules`), because `--unused`
 and the typed rules read which module declares what through those units: without them, ahra's four scripts lost
 their six unused-import findings while the phase said it ran. Into a store that already holds them the frontend
 skips them, but an empty store, every `--no-cache` run and every first run, writes them all again. Measured
 2026-11-07 on ahra's four scripts at load 10: 1.42s and 5.1 CPU seconds of 1.64s, against 0.20s into a store that
 held them; 30 MB in 2,719 files, the same units each time, a function of the toolchain and the SDK alone, like
 Swift's own module cache for the SDK, which every run already shares.

 So a seed is a store of the SDK units alone, never a project's, under `~/Library/Caches/cohere/sdk-index/<key>`,
 never in the project, so `--no-cache` still reads and writes nothing of the project's. A check into an empty store
 clones the seed's `v5` into it, one `clonefile` (31 to 42ms measured), and the frontend finds those units up to date.
 A seed serves one import set (`Key`), so the store it seeds is the one fresh indexing would have written. Nothing in the seed is taken on trust: a unit whose file differs from the one
 the seed recorded, or whose module file (the SDK's `.swiftinterface`, a `.pcm` the module cache built) is not the one
 it was indexed from, is taken out of the store again, so the frontend indexes that module afresh. A seed that cannot
 be read, or whose key differs in any part, is a miss: the check indexes everything, as before. A seed is written
 only from a check that started from an empty store and finished every job, into a folder of its own that becomes
 the seed in one rename, so a killed or racing run never leaves half a seed a reader can take.
 */
struct SDKIndexSeed: Sendable {
    /* The seed's own layout and meaning, raised whenever either changes, so no older seed is read as a newer one. */
    static let format = 1

    /* Where the seeds live: `COHERE_SDK_INDEX_SEED_ROOT` when a bench or a proof points it elsewhere, else the user's caches. */
    static func root(environment: [String: String] = ProcessInfo.processInfo.environment) -> URL {
        if let given = environment["COHERE_SDK_INDEX_SEED_ROOT"], !given.isEmpty {
            return URL(fileURLWithPath: given, isDirectory: true)
        }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(
            "Library/Caches/cohere/sdk-index",
            isDirectory: true,
        )
    }

    let root: URL
    /* What `swift --version` printed, whole (`EngineVersion.versionOutput`). */
    let swiftVersion: String
    var runner = ProcessRunner()

    /*
     Everything the SDK units depend on: the toolchain, whole as `swift --version` prints it; the SDK by its path and
     its build version, which a beta changes while the marketing version stays; the target, deployment version and
     all; the exact index flags; the job's whole argument list, its outputs and its file taken out, so no flag nobody
     thought of can share a seed it should not; the modules the checked files import; the engine's contract; and the
     seed's format. Compared part by part, never by name alone.

     The imports are in the key because what the frontend writes for an SDK header depends on what else the run
     imports, not on the toolchain and SDK alone. Measured 2026-11-07: a seed made from a script importing Foundation,
     used by one importing Foundation and MapKit, held the same units as fresh indexing of the second script, but its
     record of math.h held four macro occurrences (`isfinite`, `isinf`, `isnan`, `isnormal`) that fresh indexing of
     the second did not write, since Clang records them in one context and not in the other. With the imports fixed,
     fresh indexing is deterministic: eight fresh stores of ahra's four scripts, checked at once, held the same 2,588
     records. So a seed serves exactly the import set it was made from, and a store seeded from it holds what fresh
     indexing would have written, record for record (ruled on #f6pqraj, option a).
     */
    struct Key: Codable, Equatable, Sendable {
        var format: Int
        var contract: Int
        var swiftVersion: String
        var sdkPath: String
        var sdkBuild: String
        var target: String
        var indexFlags: [String]
        var arguments: [String]
        var imports: [String]

        /* The folder the seed of this key lives in: the SHA-256 of the key, its keys sorted. */
        var name: String {
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            let data = (try? encoder.encode(self)) ?? Data()
            return SDKIndexSeed.sha256(data)
        }
    }

    /* The arguments that belong to one job's file and outputs, each taken out of the key with its value. */
    static let ownArguments: Set<String> = [
        "-module-name", "-primary-file", "-o", "-serialize-diagnostics-path", "-index-unit-output-path",
        "-index-store-path",
    ]

    /*
     The key of a frontend job as the check runs it, whose files import `imports`, or nil when it names no SDK or
     target, or the SDK's build version cannot be read: then there is no seed to use or make.
     */
    static func key(
        arguments: [String],
        imports: [String],
        swiftVersion: String,
        contract: Int = EngineVersion.contract,
        format: Int = SDKIndexSeed.format,
        sdkBuild: (URL) -> String? = SDKIndexSeed.sdkBuild(at:),
    ) -> Key? {
        func value(of flag: String) -> String? {
            guard let index = arguments.firstIndex(of: flag), arguments.indices.contains(index + 1) else { return nil }
            return arguments[index + 1]
        }
        guard let sdk = value(of: "-sdk"), let target = value(of: "-target") else { return nil }
        let sdkPath = URL(fileURLWithPath: sdk).resolvingSymlinksInPath()
        guard let build = sdkBuild(sdkPath), !build.isEmpty else { return nil }
        let indexFlags = Set(
            arguments.filter {
                $0.hasPrefix("-index-") && !["-index-store-path", "-index-unit-output-path"].contains($0)
            }
        )
        var kept: [String] = []
        var remaining = arguments[...]
        while let argument = remaining.popFirst() {
            if ownArguments.contains(argument) {
                remaining = remaining.dropFirst()
                continue
            }
            kept.append(argument)
        }
        return Key(
            format: format,
            contract: contract,
            swiftVersion: swiftVersion,
            sdkPath: sdkPath.path,
            sdkBuild: build,
            target: target,
            indexFlags: indexFlags.sorted(),
            arguments: kept,
            imports: Set(imports).sorted(),
        )
    }

    /*
     The SDK's build version, `ProductBuildVersion` in its `SystemVersion.plist`: what `xcrun --show-sdk-build-version`
     reads (both said 26B118 for MacOSX27.2.sdk on 2026-11-07), without starting a process.
     */
    static func sdkBuild(at sdk: URL) -> String? {
        struct SystemVersion: Decodable {
            var build: String

            enum CodingKeys: String, CodingKey {
                case build = "ProductBuildVersion"
            }
        }
        let plist = sdk.appendingPathComponent("System/Library/CoreServices/SystemVersion.plist")
        guard let data = FileManager.default.contents(atPath: plist.path) else { return nil }
        return try? PropertyListDecoder().decode(SystemVersion.self, from: data).build
    }

    /* What one check did with the seed: its key, and whether the seed was there and used. */
    struct Use: Equatable, Sendable {
        var key: Key
        var hit: Bool
        /* The seed's units cloned in and kept, and those taken out again because they could not be vouched for. */
        var unitsSeeded = 0
        var unitsSetAside = 0
        /* Why the store holds units no one vouched for, which fails the check, nil when it does not. */
        var failure: String? = nil
    }

    /* What a seed holds, written last, so a folder without it is no seed. */
    struct Manifest: Codable, Equatable {
        var key: Key
        var units: [Unit]

        /* One SDK unit: its file, the module file it was indexed from, as both stood when the seed was made, and its records. */
        struct Unit: Codable, Equatable {
            var name: String
            var size: Int
            var sha256: String
            var moduleFile: String
            var moduleFileSize: Int
            var moduleFileModified: Double
            var records: [String]
            /* The units of the modules this one imports, which a check reaches only through this one. */
            var importedUnits: [String]
        }
    }

    static let manifestName = "seed.json"

    /* The folder of this key's seeds. Its `current` names the seed in use; each seed is a folder of its own beside it. */
    func directory(of key: Key) -> URL {
        root.appendingPathComponent(key.name, isDirectory: true)
    }

    /*
     The jobs' one key when they all share it, nil when they differ (a macOS script beside an iOS one) or any names
     none: a store is seeded from one key or not at all.
     */
    func key(of jobs: [[String]], imports: [String]) -> Key? {
        let keys = jobs.map { Self.key(arguments: $0, imports: imports, swiftVersion: swiftVersion) }
        guard let first = keys.first, let key = first, keys.allSatisfy({ $0 == key }) else { return nil }
        return key
    }

    /*
     The empty store at `store` seeded from the seed of `key`. A hit clones the seed's `v5` in, then takes back out
     every unit that is not, byte for byte, the one the seed recorded, or whose module file is not the one it was
     indexed from, so the frontend indexes those modules. A miss leaves the store empty: no seed, an unreadable one,
     one of another key, or one none of whose units holds.
     */
    func seed(store: URL, key: Key) -> Use {
        let miss = Use(key: key, hit: false)
        let current = directory(of: key).appendingPathComponent("current").resolvingSymlinksInPath()
        guard let data = FileManager.default.contents(atPath: current.appendingPathComponent(Self.manifestName).path),
            let manifest = try? JSONDecoder().decode(Manifest.self, from: data),
            manifest.key == key
        else { return miss }
        let units = current.appendingPathComponent("v5/units", isDirectory: true)
        let records = current.appendingPathComponent("v5/records", isDirectory: true)
        let setAside = Self.importers(
            of: Set(manifest.units.filter { !Self.holds($0, units: units, records: records) }.map(\.name)),
            in: manifest.units,
        )
        guard setAside.count < manifest.units.count else { return miss }
        let seeded = store.appendingPathComponent("v5", isDirectory: true)
        do {
            try FileManager.default.createDirectory(at: store, withIntermediateDirectories: true)
            guard unsafe clonefile(current.appendingPathComponent("v5").path, seeded.path, 0) == 0 else { return miss }
            for name in setAside {
                try Self.remove(seeded.appendingPathComponent("units").appendingPathComponent(name))
            }
        }
        catch {
            /* A unit that could not be taken back out could be read as vouched for, so nothing of the seed stays. */
            do {
                try FileManager.default.removeItem(at: seeded)
            }
            catch let left {
                var stuck = miss
                stuck.failure =
                    "the SDK index seed's units could not be taken back out of \(store.path), so the store cannot be trusted: \(left)"
                return stuck
            }
            return miss
        }
        return Use(
            key: key,
            hit: true,
            unitsSeeded: manifest.units.count - setAside.count,
            unitsSetAside: setAside.count,
        )
    }

    /*
     These units and every unit that imports one of them, through any chain: a check walks the modules a file imports
     from the top and stops at a module whose unit is up to date, never reaching what that module imports. Measured
     2026-11-07: CoreFoundation's unit taken out of a seeded store was not written again while Foundation's stood, so
     a unit set aside takes every unit above it out with it, and the check indexes that whole branch afresh.
     */
    static func importers(of names: Set<String>, in units: [Manifest.Unit]) -> Set<String> {
        var reached = names
        var grew = !names.isEmpty
        while grew {
            let more = units.filter { !reached.contains($0.name) && !reached.isDisjoint(with: $0.importedUnits) }
            reached.formUnion(more.map(\.name))
            grew = !more.isEmpty
        }
        return reached
    }

    /* Whether a seed's unit is still the one it recorded: its file, every record it names, and the module file it was indexed from. */
    static func holds(_ unit: Manifest.Unit, units: URL, records: URL) -> Bool {
        let file = units.appendingPathComponent(unit.name)
        guard let data = FileManager.default.contents(atPath: file.path), data.count == unit.size,
            sha256(data) == unit.sha256,
            unit.records.allSatisfy({ FileManager.default.fileExists(atPath: recordPath(in: records, $0).path) }),
            let module = moduleFileStamp(unit.moduleFile),
            module.size == unit.moduleFileSize, module.modified == unit.moduleFileModified
        else { return false }
        return true
    }

    /*
     Makes this key's seed from a store a check filled from empty after a miss, its jobs all finished: every SDK unit
     the store holds, never a project's (`isSystem` is the index store's own mark), with the records each names. A
     seed is immutable once made, so a key that already has one readable keeps it, and only a missing or unreadable
     one is replaced. Built in a folder of its own, its manifest written last, then renamed into place and named
     `current` by a second rename, so a reader takes a whole seed or none. Best effort: a seed not made is a miss next
     time, slower and still right.
     */
    func publish(from store: URL, key: Key) {
        guard let library = try? IndexStore.toolchainLibraryPath(runner: runner),
            let index = try? IndexStore(libraryPath: library, storePath: store)
        else { return }
        let storeUnits = store.appendingPathComponent("v5/units", isDirectory: true)
        let storeRecords = store.appendingPathComponent("v5/records", isDirectory: true)
        let system = index.units().filter(\.isSystem)
        guard !system.isEmpty else { return }
        let folder = directory(of: key)
        let current = folder.appendingPathComponent("current").resolvingSymlinksInPath()
        /* A seed is never changed once made: one another run made since this run's miss stands. */
        if let data = FileManager.default.contents(atPath: current.appendingPathComponent(Self.manifestName).path),
            let manifest = try? JSONDecoder().decode(Manifest.self, from: data), manifest.key == key
        {
            return
        }
        let identifier = UUID().uuidString
        let versions = folder.appendingPathComponent("versions", isDirectory: true)
        let making = versions.appendingPathComponent(".making-\(identifier)", isDirectory: true)
        let made = versions.appendingPathComponent(identifier, isDirectory: true)
        do {
            let units = making.appendingPathComponent("v5/units", isDirectory: true)
            let records = making.appendingPathComponent("v5/records", isDirectory: true)
            try FileManager.default.createDirectory(at: units, withIntermediateDirectories: true)
            try FileManager.default.createDirectory(at: records, withIntermediateDirectories: true)
            var manifest = Manifest(key: key, units: [])
            for unit in system.sorted(by: { $0.name < $1.name }) {
                guard let entry = try Self.copy(unit, from: (storeUnits, storeRecords), to: (units, records)) else {
                    continue
                }
                manifest.units.append(entry)
            }
            guard !manifest.units.isEmpty else {
                Self.discard(making)
                return
            }
            let encoder = JSONEncoder()
            encoder.outputFormatting = [.sortedKeys]
            try encoder.encode(manifest).write(to: making.appendingPathComponent(Self.manifestName), options: .atomic)
            try FileManager.default.moveItem(at: making, to: made)
            let link = folder.appendingPathComponent("current.\(identifier)")
            try FileManager.default.createSymbolicLink(
                atPath: link.path,
                withDestinationPath: "versions/\(identifier)",
            )
            /* `rename` replaces the old link in one step, so a reader resolves the old seed or the new one. */
            guard unsafe rename(link.path, folder.appendingPathComponent("current").path) == 0 else {
                Self.discard(link)
                return
            }
            prune(versions: versions, keeping: identifier)
        }
        catch {
            /* Not made, so the next check of this key indexes the SDK again: slower, and still right. */
            Self.discard(making)
        }
    }

    /*
     One SDK unit cloned into a seed being made, with every record it names, and its manifest entry. Nil when the unit
     or one of its records is missing, or its module file cannot be read: a unit the seed cannot vouch for stays out.
     */
    private static func copy(
        _ unit: IndexStore.Unit,
        from store: (units: URL, records: URL),
        to seed: (units: URL, records: URL),
    ) throws -> Manifest.Unit? {
        let source = store.units.appendingPathComponent(unit.name)
        guard let data = FileManager.default.contents(atPath: source.path),
            let module = moduleFileStamp(unit.outputFile),
            unit.allRecords.allSatisfy({
                FileManager.default.fileExists(atPath: recordPath(in: store.records, $0).path)
            })
        else { return nil }
        try clone(source, to: seed.units.appendingPathComponent(unit.name))
        for record in unit.allRecords {
            let target = recordPath(in: seed.records, record)
            guard !FileManager.default.fileExists(atPath: target.path) else { continue }
            try FileManager.default.createDirectory(
                at: target.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try clone(recordPath(in: store.records, record), to: target)
        }
        return Manifest.Unit(
            name: unit.name,
            size: data.count,
            sha256: sha256(data),
            moduleFile: unit.outputFile,
            moduleFileSize: module.size,
            moduleFileModified: module.modified,
            records: unit.allRecords.sorted(),
            importedUnits: unit.importedUnits.sorted(),
        )
    }

    /* Where a record lives in a store's `v5/records`: under the folder its name's last two characters name. */
    static func recordPath(in records: URL, _ record: String) -> URL {
        records.appendingPathComponent(String(record.suffix(2)), isDirectory: true).appendingPathComponent(record)
    }

    /* A module file's size and modification time, or nil when it is not there to read. */
    static func moduleFileStamp(_ path: String) -> (size: Int, modified: Double)? {
        guard !path.isEmpty,
            let values = try? URL(fileURLWithPath: path).resourceValues(forKeys: [
                .fileSizeKey, .contentModificationDateKey,
            ]),
            let size = values.fileSize, let modified = values.contentModificationDate
        else { return nil }
        return (size, modified.timeIntervalSince1970)
    }

    static func sha256(_ data: Data) -> String {
        SHA256.hash(data: data).map { String($0 >> 4, radix: 16) + String($0 & 0x0f, radix: 16) }.joined()
    }

    private static func clone(_ source: URL, to target: URL) throws {
        guard unsafe clonefile(source.path, target.path, 0) == 0 else {
            try FileManager.default.copyItem(at: source, to: target)
            return
        }
    }

    private static func remove(_ file: URL) throws {
        do {
            try FileManager.default.removeItem(at: file)
        }
        catch CocoaError.fileNoSuchFile {
            /* Already gone, which is what was wanted. */
        }
    }

    /* Every seed of this key but the current one, once nothing could still be cloning from it: an hour after it was made. */
    private func prune(versions: URL, keeping current: String) {
        let entries =
            (try? FileManager.default.contentsOfDirectory(
                at: versions,
                includingPropertiesForKeys: [.contentModificationDateKey],
            )) ?? []
        for entry in entries where entry.lastPathComponent != current {
            guard
                let modified = try? entry.resourceValues(forKeys: [.contentModificationDateKey])
                    .contentModificationDate,
                Date().timeIntervalSince(modified) > ScratchPrune.inUseWindow
            else { continue }
            Self.discard(entry)
        }
    }

    /*
     Removes a seed being made that will not be, a link not swapped in, or a seed no longer current. Nothing reads any
     of them: a reader takes only the folder `current` names, so one left behind costs only its space.
     */
    private static func discard(_ item: URL) {
        do {
            try FileManager.default.removeItem(at: item)
        }
        catch {
            /* Left for the next publish's prune, or the user's cache sweep; no reader reaches it. */
        }
    }
}
