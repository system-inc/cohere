import Foundation
import Testing

@testable import CohereSwift

/*
 The SDK index seed (#f6pqraj): a check into an empty store takes the SDK's index units from the toolchain's seed,
 and the store it leaves is the one fresh indexing would have left. Serialized, since each check runs frontends.
 */
@Suite(.serialized)
struct SDKIndexSeedTests {
    /* A folder of this test's own, resolved. */
    static func folder(_ name: String) throws -> URL {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-sdk-seed-\(name)-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return folder.resolvingSymlinksInPath()
    }

    /* One standalone script, checked for macOS. */
    static func script(_ text: String, named name: String, in root: URL) throws -> FileSet.OwnedFile {
        let url = root.appendingPathComponent(name)
        try text.write(to: url, atomically: true, encoding: .utf8)
        var file = FileSet.OwnedFile(url: url, targetName: String(name.dropLast(".swift".count)), targetKind: "script")
        file.scriptPlatform = .macOS
        return file
    }

    /* The scripts checked into a fresh scratch against the seeds under `seeds`, with the store the check left. */
    static func check(
        _ scripts: [FileSet.OwnedFile],
        root: URL,
        seeds: URL,
    ) throws -> (result: ScriptTypesPhase.Result, store: URL, started: Date) {
        let scratch = try folder("scratch")
        var phase = ScriptTypesPhase(
            scripts: scripts,
            scratchPath: scratch,
            workingDirectory: root,
            runner: ProcessRunner(),
        )
        phase.sdkIndexSeedRoot = seeds
        let started = Date()
        let result = try phase.run()
        let store = IndexStore.storePath(layout: .check(scratchPath: ScriptTypesPhase.scratchPath(in: scratch)))
        return (result, store, started)
    }

    static func store(_ path: URL) throws -> IndexStore {
        try IndexStore(libraryPath: IndexStore.toolchainLibraryPath(), storePath: path)
    }

    /* The records every SDK unit of a store names: content addresses, so equal sets are equal records. */
    static func sdkRecords(_ path: URL) throws -> Set<String> {
        Set(try store(path).units().filter(\.isSystem).flatMap(\.allRecords))
    }

    /* The names of a store's SDK units. */
    static func sdkUnits(_ path: URL) throws -> Set<String> {
        Set(try store(path).units().filter(\.isSystem).map(\.name))
    }

    /* The folder the current seed of `key` lives in. */
    static func current(_ key: SDKIndexSeed.Key, seeds: URL) -> URL {
        SDKIndexSeed(root: seeds, swiftVersion: key.swiftVersion).directory(of: key)
            .appendingPathComponent("current").resolvingSymlinksInPath()
    }

    static let foundationScript = "import Foundation\n\nprint(Date())\n"

    /*
     A store seeded from the toolchain's seed holds exactly the SDK records fresh indexing writes, and the same
     findings come of it. The first check misses and makes the seed; the seed holds SDK units only, none a project's;
     the second check hits, and every project unit in its store was written by that check itself.
     */
    @Test func aSeededStoreIsWhatFreshIndexingMakes() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]

        let fresh = try Self.check(scripts, root: root, seeds: seeds)
        let missed = try #require(fresh.result.sdkIndexSeed)
        #expect(!missed.hit)
        let seed = Self.current(missed.key, seeds: seeds)
        let seedUnits = try Self.store(seed).units()
        #expect(!seedUnits.isEmpty)
        let projectUnitsInSeed = seedUnits.filter { !$0.isSystem }.map(\.name)
        #expect(projectUnitsInSeed.isEmpty, "\(projectUnitsInSeed)")
        #expect(!seedUnits.contains { $0.mainFile.hasPrefix(root.path) })

        let seeded = try Self.check(scripts, root: root, seeds: seeds)
        let hit = try #require(seeded.result.sdkIndexSeed)
        #expect(hit.hit && hit.key == missed.key && hit.unitsSetAside == 0)
        #expect(try Self.sdkRecords(seeded.store) == (try Self.sdkRecords(fresh.store)))
        #expect(try Self.sdkUnits(seeded.store) == (try Self.sdkUnits(fresh.store)))
        #expect(seeded.result.findings == fresh.result.findings)
        #expect(seeded.result.filesWithoutRecord.isEmpty)
        let seedNames = Set(seedUnits.map(\.name))
        let projectUnits = try Self.store(seeded.store).units().filter { !$0.isSystem }
        #expect(!projectUnits.isEmpty)
        for unit in projectUnits {
            #expect(!seedNames.contains(unit.name), "\(unit.name) came from the seed")
            #expect(unit.written >= seeded.started.addingTimeInterval(0), "\(unit.name) is older than this run")
        }
    }

    /*
     A script that gains an import is checked against a key of its own, since the seed serves one import set: Clock's
     check made a seed of what importing Foundation indexes, then Clock imports CoreLocation too. That run is a miss,
     indexes everything afresh, CoreLocation included, and makes a seed of its own, leaving the first one as it was.
     The run after it is a hit whose store holds, record for record, what that fresh indexing wrote. Red under the
     mutant that leaves the imports out of the key: the second run takes Foundation's seed, and CoreLocation's units
     are never written, since the frontend stops at a module whose unit stands.
     */
    @Test func anImportTheSeedLacksIsIndexedThenSeeded() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let clock = try Self.script(Self.foundationScript, named: "Clock.swift", in: root)
        let first = try #require(try Self.check([clock], root: root, seeds: seeds).result.sdkIndexSeed)
        #expect(!first.hit)
        let located = try Self.script(
            "import CoreLocation\nimport Foundation\n\nprint(CLLocation(latitude: 0, longitude: 0).coordinate.latitude, Date())\n",
            named: "Clock.swift",
            in: root,
        )

        let gained = try Self.check([located], root: root, seeds: seeds)
        let missed = try #require(gained.result.sdkIndexSeed)
        #expect(!missed.hit && missed.key != first.key && missed.key.imports == ["CoreLocation", "Foundation"])
        let units = try Self.store(gained.store).units().filter(\.isSystem)
        #expect(units.contains { $0.module == "CoreLocation" }, "\(units.map(\.module))")
        #expect(
            try Self.store(Self.current(first.key, seeds: seeds)).units().allSatisfy { $0.module != "CoreLocation" }
        )
        #expect(
            try Self.store(Self.current(missed.key, seeds: seeds)).units().contains { $0.module == "CoreLocation" }
        )

        let next = try Self.check([located], root: root, seeds: seeds)
        let hit = try #require(next.result.sdkIndexSeed)
        #expect(hit.hit && hit.key == missed.key && hit.unitsSetAside == 0)
        #expect(try Self.sdkRecords(next.store) == (try Self.sdkRecords(gained.store)))
        #expect(try Self.sdkUnits(next.store) == (try Self.sdkUnits(gained.store)))
        #expect(next.result.findings == gained.result.findings)
        let written = try Self.store(next.store).units().filter { $0.isSystem && $0.written >= next.started }
        #expect(written.isEmpty, "\(written.map(\.module))")
    }

    /*
     A unit the seed cannot vouch for is indexed, never skipped: one taken out of the seed, and one whose bytes differ
     from what the seed recorded, are both written by the check. Red under the mutant that trusts every listed unit
     (`holds` answering yes): the corrupt unit stays, older than the run.
     */
    @Test func aUnitTheSeedCannotVouchForIsIndexed() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let first = try #require(try Self.check(scripts, root: root, seeds: seeds).result.sdkIndexSeed)
        let seed = Self.current(first.key, seeds: seeds)
        let manifest = try JSONDecoder().decode(
            SDKIndexSeed.Manifest.self,
            from: Data(contentsOf: seed.appendingPathComponent(SDKIndexSeed.manifestName)),
        )
        let foundation = try #require(
            manifest.units.first { $0.moduleFile.contains("Foundation.swiftmodule") || $0.name.contains("Foundation") }
        )
        let other = try #require(manifest.units.first { $0.name != foundation.name })
        try FileManager.default.removeItem(at: seed.appendingPathComponent("v5/units/\(foundation.name)"))
        var corrupt = try Data(contentsOf: seed.appendingPathComponent("v5/units/\(other.name)"))
        corrupt[corrupt.count / 2] ^= 0xff
        try corrupt.write(to: seed.appendingPathComponent("v5/units/\(other.name)"))

        let checked = try Self.check(scripts, root: root, seeds: seeds)
        let use = try #require(checked.result.sdkIndexSeed)
        #expect(use.hit && use.unitsSetAside >= 2)
        let fresh = try Self.check(scripts, root: root, seeds: try Self.folder("empty"))
        #expect(try Self.sdkUnits(checked.store) == (try Self.sdkUnits(fresh.store)))
        let units = Dictionary(
            try Self.store(checked.store).units().map { ($0.name, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        for name in [foundation.name, other.name] {
            let unit = try #require(
                units[name],
                "\(name) was not indexed; the store holds \(units.keys.filter { $0.hasPrefix(String(name.prefix(8))) })",
            )
            #expect(unit.written >= checked.started.addingTimeInterval(0), "\(name) is the seed's, not this run's")
        }
    }

    /*
     A record whose bytes are not the ones the seed recorded is never read: one record of the seed truncated, the hit
     sets aside every unit naming it with their importers, takes the record out of the store, and the check writes it
     again, so the store holds what fresh indexing writes. Red under the mutant that checks records only by name.
     */
    @Test func aTruncatedRecordSetsAsideTheUnitsNamingIt() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let fresh = try Self.check(scripts, root: root, seeds: seeds)
        let key = try #require(fresh.result.sdkIndexSeed).key
        let seed = Self.current(key, seeds: seeds)
        let manifest = try JSONDecoder().decode(
            SDKIndexSeed.Manifest.self,
            from: Data(contentsOf: seed.appendingPathComponent(SDKIndexSeed.manifestName)),
        )
        let record = try #require(manifest.records.first { $0.size > 64 })
        let path = SDKIndexSeed.recordPath(in: seed.appendingPathComponent("v5/records"), record.name)
        try Data(contentsOf: path).prefix(record.size / 2).write(to: path)
        let naming = Set(manifest.units.filter { $0.records.contains(record.name) }.map(\.name))
        #expect(!naming.isEmpty)

        let checked = try Self.check(scripts, root: root, seeds: seeds)
        let use = try #require(checked.result.sdkIndexSeed)
        #expect(use.hit && use.unitsSetAside >= naming.count)
        let units = Dictionary(
            try Self.store(checked.store).units().map { ($0.name, $0) },
            uniquingKeysWith: { first, _ in first },
        )
        for name in naming {
            #expect((units[name]?.written ?? .distantPast) >= checked.started, "\(name) was not indexed again")
        }
        let written = try Data(
            contentsOf: SDKIndexSeed.recordPath(in: checked.store.appendingPathComponent("v5/records"), record.name)
        )
        #expect(written.count == record.size && SDKIndexSeed.sha256(written) == record.sha256)
        #expect(try Self.sdkRecords(checked.store) == (try Self.sdkRecords(fresh.store)))
    }

    /*
     Seeds do not pile up: a publish removes every key's folder no hit or publish touched in fourteen days, keeps a
     fresh one, and never removes one a reader holds, since the prune takes a key's lock without waiting and skips a
     folder it cannot. A hit touches its key's folder, so a key in use is never stale. Red under the mutant that
     prunes without the lock: the held folder goes.
     */
    @Test func staleKeysGoAndHeldOnesStay() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let made = try Self.check(scripts, root: root, seeds: seeds)
        let key = try #require(made.result.sdkIndexSeed).key
        let seed = SDKIndexSeed(root: seeds, swiftVersion: key.swiftVersion)
        let old = Date().addingTimeInterval(-15 * 24 * 60 * 60)
        func plant(_ name: String, modified: Date) throws -> URL {
            let folder = seeds.appendingPathComponent(name, isDirectory: true)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            FileManager.default.createFile(atPath: folder.appendingPathComponent("lock").path, contents: Data())
            try FileManager.default.setAttributes([.modificationDate: modified], ofItemAtPath: folder.path)
            return folder
        }
        let stale = try plant("stale", modified: old)
        let fresh = try plant("fresh", modified: Date())
        let held = try plant("held", modified: old)
        try FileManager.default.setAttributes([.modificationDate: old], ofItemAtPath: seed.directory(of: key).path)

        let hit = try Self.check(scripts, root: root, seeds: seeds)
        #expect(hit.result.sdkIndexSeed?.hit == true)
        let touched =
            try seed.directory(of: key).resourceValues(forKeys: [.contentModificationDateKey])
            .contentModificationDate ?? .distantPast
        #expect(touched >= hit.started, "a hit left its key's folder untouched")

        do {
            /* As a reader in the middle of a hit holds it. */
            let reading = try #require(SDKIndexSeed.KeyLock(folder: held, exclusive: false))
            seed.pruneStaleKeys(keeping: key)
            #expect(!FileManager.default.fileExists(atPath: stale.path))
            #expect(FileManager.default.fileExists(atPath: fresh.path))
            #expect(FileManager.default.fileExists(atPath: held.path), "a folder being read was removed")
            #expect(FileManager.default.fileExists(atPath: seed.directory(of: key).path))
            withExtendedLifetime(reading) {}
        }
        seed.pruneStaleKeys(keeping: key)
        #expect(!FileManager.default.fileExists(atPath: held.path), "a stale folder no one holds stayed")
    }

    /* A seed whose manifest cannot be read is a miss: the check indexes everything, and finds what it found before. */
    @Test func aCorruptSeedIsAMiss() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let fresh = try Self.check(scripts, root: root, seeds: seeds)
        let key = try #require(fresh.result.sdkIndexSeed).key
        try Data("not a manifest".utf8).write(
            to: Self.current(key, seeds: seeds).appendingPathComponent(SDKIndexSeed.manifestName)
        )
        let checked = try Self.check(scripts, root: root, seeds: seeds)
        #expect(checked.result.sdkIndexSeed?.hit == false)
        #expect(try Self.sdkRecords(checked.store) == (try Self.sdkRecords(fresh.store)))
        #expect(checked.result.findings == fresh.result.findings)
    }

    /*
     Every part of the key is one a seed must match: a seed made under one key is a miss for a key that differs in any
     single part. Red under the mutant that leaves any one part out of the key.
     */
    @Test func aKeyDifferingInAnyPartMisses() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let key = try #require(try Self.check(scripts, root: root, seeds: seeds).result.sdkIndexSeed).key
        let seed = SDKIndexSeed(root: seeds, swiftVersion: key.swiftVersion)
        #expect(seed.seed(store: try Self.folder("store").appendingPathComponent("index-store"), key: key).hit)

        var changed: [String: SDKIndexSeed.Key] = [:]
        changed["format"] = {
            var other = key
            other.format += 1
            return other
        }()
        changed["contract"] = {
            var other = key
            other.contract += 1
            return other
        }()
        changed["swiftVersion"] = {
            var other = key
            other.swiftVersion += " (another build)"
            return other
        }()
        changed["sdkPath"] = {
            var other = key
            other.sdkPath += "-other"
            return other
        }()
        changed["sdkBuild"] = {
            var other = key
            other.sdkBuild += "a"
            return other
        }()
        changed["target"] = {
            var other = key
            other.target = "arm64-apple-macosx15.0"
            return other
        }()
        changed["indexFlags"] = {
            var other = key
            other.indexFlags.append("-index-include-locals")
            return other
        }()
        changed["arguments"] = {
            var other = key
            other.arguments.append("-enable-testing")
            return other
        }()
        changed["imports"] = {
            var other = key
            other.imports.append("CoreLocation")
            return other
        }()
        for (part, other) in changed.sorted(by: { $0.key < $1.key }) {
            let store = try Self.folder("store").appendingPathComponent("index-store")
            #expect(!seed.seed(store: store, key: other).hit, "a key differing in \(part) took the seed")
            #expect(!FileManager.default.fileExists(atPath: store.appendingPathComponent("v5").path))

            /* The same seed under the other key's folder, as a hand's copy or a collision would put it: still a miss. */
            try FileManager.default.copyItem(at: seed.directory(of: key), to: seed.directory(of: other))
            let planted = try Self.folder("store").appendingPathComponent("index-store")
            #expect(
                !seed.seed(store: planted, key: other).hit,
                "a seed of another \(part) under this key's folder was taken",
            )
        }
    }

    /*
     Each part of the key moves it: the SDK, its build, the toolchain, the target, the index flags, any other argument,
     the imports, the contract and the format; one job's own file and outputs do not. Red under the mutant that leaves
     any one part out of what `key` reads.
     */
    @Test func theKeyReadsEveryPartOfTheJob() {
        let arguments = [
            "-frontend", "-emit-sib", "-primary-file", "/root/Clock.swift", "-sdk", "/SDKs/A.sdk", "-target",
            "arm64-apple-macosx27.2.0", "-swift-version", "6", "-module-name", "Clock", "-o", "/dev/null",
            "-serialize-diagnostics-path", "/check/Clock.dia", "-index-unit-output-path", "/Clock/Clock.o",
            "-index-store-path", "/store", "-index-system-modules",
        ]
        func key(
            _ arguments: [String],
            imports: [String] = ["Foundation"],
            build: String = "26B118",
            version: String = "6.4.2",
            contract: Int = 3,
            format: Int = 1,
        ) -> SDKIndexSeed.Key? {
            SDKIndexSeed.key(
                arguments: arguments,
                imports: imports,
                swiftVersion: version,
                contract: contract,
                format: format,
                sdkBuild: { _ in build },
            )
        }
        func replacing(_ old: String, with new: String) -> [String] {
            arguments.map { $0 == old ? new : $0 }
        }
        let base = key(arguments)
        #expect(base?.indexFlags == ["-index-system-modules"])
        #expect(base?.target == "arm64-apple-macosx27.2.0")
        #expect(base?.sdkPath == "/SDKs/A.sdk" && base?.sdkBuild == "26B118" && base?.imports == ["Foundation"])
        #expect(base?.arguments.contains("/root/Clock.swift") == false)
        #expect(base?.arguments.contains("Clock") == false)
        #expect(key(replacing("/SDKs/A.sdk", with: "/SDKs/B.sdk")) != base)
        #expect(key(arguments, build: "26B119") != base)
        #expect(key(arguments, version: "6.4.3") != base)
        #expect(
            key(arguments, version: "Apple Swift\nversion  6.4.2\n")
                == key(arguments, version: "Apple Swift version 6.4.2")
        )
        #expect(key(replacing("arm64-apple-macosx27.2.0", with: "arm64-apple-ios27.0")) != base)
        #expect(key(arguments + ["-index-ignore-clang-modules"]) != base)
        #expect(key(replacing("6", with: "5")) != base)
        #expect(key(arguments, imports: ["CoreLocation", "Foundation"]) != base)
        #expect(key(arguments, imports: ["Foundation", "Foundation"]) == base)
        #expect(key(arguments, contract: 4) != base)
        #expect(key(arguments, format: 2) != base)
        #expect(key(replacing("/store", with: "/elsewhere")) == base)
        #expect(
            key(replacing("Clock", with: "Map").map { $0 == "/root/Clock.swift" ? "/root/Map.swift" : $0 }) == base
        )
        #expect(key(replacing("/Clock/Clock.o", with: "/Map/Map.o")) == base)
        #expect(key(arguments.filter { $0 != "-sdk" && $0 != "/SDKs/A.sdk" }) == nil)
        #expect(
            SDKIndexSeed.key(arguments: arguments, imports: [], swiftVersion: "6.4.2", sdkBuild: { _ in nil }) == nil
        )
    }

    /* Two runs making the same seed at once each finish a whole one, and a reader takes whichever was named last. */
    @Test func twoRunsMakingASeedAtOnceLeaveAWholeOne() throws {
        let root = try Self.folder("root")
        let seeds = try Self.folder("seeds")
        let scripts = [try Self.script(Self.foundationScript, named: "Clock.swift", in: root)]
        let made = try Self.check(scripts, root: root, seeds: try Self.folder("elsewhere"))
        let key = try #require(made.result.sdkIndexSeed).key
        let seed = SDKIndexSeed(root: seeds, swiftVersion: key.swiftVersion)
        DispatchQueue.concurrentPerform(iterations: 4) { _ in
            seed.publish(from: made.store, key: key)
        }
        let use = seed.seed(store: try Self.folder("store").appendingPathComponent("index-store"), key: key)
        #expect(use.hit && use.unitsSetAside == 0)
        let versions = try FileManager.default.contentsOfDirectory(
            atPath: seed.directory(of: key).appendingPathComponent("versions").path
        )
        #expect(!versions.contains { $0.hasPrefix(".making-") }, "\(versions)")
    }
}
