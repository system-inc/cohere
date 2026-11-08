import Foundation
import Synchronization
import Testing

@testable import CohereSwift

/*
 The two promises of a check that never builds (#11fs5xp): it writes no object, archive or binary, and it reports
 what a build reports, file by file. Serialized, because each runs a build server, frontends and builds.
 */
@Suite(.serialized)
struct CheckProofTests {
    /*
     The control for the proof below: the walk finds what a compiler writes by its bytes, whatever its name. A real
     object named like a record, an object whose `__text` is empty, and an archive named like text are compiled; a
     file named like an object is not; and a real Clang module container, an AST in a Mach-O object, is told apart.
     */
    @Test func theWalkFindsCompiledFilesByTheirBytes() throws {
        let folder = CheckFixture.scratch()
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        func clang(_ arguments: [String]) throws {
            let result = try ProcessRunner().run("xcrun", ["clang"] + arguments, in: folder)
            try #require(result.succeeded, "clang could not write the control: \(result.standardError)")
        }
        let source = folder.appendingPathComponent("tiny.c")
        try "int tiny(void) { return 1; }\n".write(to: source, atomically: true, encoding: .utf8)
        try clang(["-c", source.path, "-o", folder.appendingPathComponent("tiny.dia").path])
        let empty = folder.appendingPathComponent("empty.c")
        try "".write(to: empty, atomically: true, encoding: .utf8)
        try clang(["-c", empty.path, "-o", folder.appendingPathComponent("empty.swiftmodule").path])
        try "int tiny(void);\n".write(to: folder.appendingPathComponent("tiny.h"), atomically: true, encoding: .utf8)
        let map = folder.appendingPathComponent("module.modulemap")
        try "module Tiny {\n  header \"tiny.h\"\n}\n".write(to: map, atomically: true, encoding: .utf8)
        try clang([
            "-cc1", "-triple", "arm64-apple-macosx15.0", "-emit-module", "-fmodules", "-fmodule-name=Tiny",
            "-fmodule-format=obj", "-x", "c", map.path, "-o", folder.appendingPathComponent("Tiny.o").path,
        ])
        try Data("!<arch>\nmember".utf8).write(to: folder.appendingPathComponent("library.txt"))
        try Data("not compiled".utf8).write(to: folder.appendingPathComponent("plain.o"))

        let walk = CheckFixture.walk([folder])
        #expect(
            walk.compiled.map { URL(fileURLWithPath: $0).lastPathComponent } == [
                "empty.swiftmodule", "library.txt", "tiny.dia",
            ]
        )
        #expect(walk.moduleContainers.map { URL(fileURLWithPath: $0).lastPathComponent } == ["Tiny.o"])
    }

    /*
     The no-build proof: after prepare and the whole check of the fixture, nothing under the scratch or the package is
     an object, an archive, bitcode or a Mach-O binary. The only Mach-O files are the SDK's Clang module containers,
     named here so they are seen, and none is the fixture's. The check did run: every owned file has a record, and the
     index holds a unit for a file in the dependent target.
     */
    @Test func aCheckBuildsNothing() throws {
        let root = try CheckFixture.package()
        let scratch = CheckFixture.scratch()
        let checked = try CheckFixture.check(root, scratch: scratch)

        let walk = CheckFixture.walk([scratch, root])
        #expect(walk.compiled == [])
        #expect(!walk.moduleContainers.contains { $0.contains("CCore") || $0.hasPrefix(root.path) })

        let diagnostics = try CheckFixture.diagnostics(root, directories: checked.targetDirectories)
        #expect(diagnostics.withoutRecord == [])
        let reported = diagnostics.byFile.filter { !$0.value.isEmpty }
        #expect(reported.isEmpty, "the clean fixture reported \(reported)")
        #expect(
            try !CheckFixture.occurrences(
                of: root.appendingPathComponent("Sources/Feature/Welcome.swift"),
                storePath: checked.description.indexStorePath,
            )
            .isEmpty
        )
    }

    /*
     The no-build proof through the types phase itself, as a run makes it: after a `--no-fix --unused` run of the
     fixture, nothing under the package, whose scratch is inside it, is compiled; every file has a record; the words
     say it checked without building and count the plugin host tools it built, none; and the store `--unused` reads
     holds the C file's unit beside the Swift files', since a Swift `import CCore` resolves through it.
     */
    @Test func aRunsTypesPhaseBuildsNothing() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--unused"])

        #expect(run.typesBuild.hasPrefix("checked without building"), "\(run.typesBuild)")
        #expect(run.typesBuild.contains("prepare built no plugin host tool"), "\(run.typesBuild)")
        #expect(run.filesWithoutRecord.isEmpty)
        #expect(run.phase("unused")?.outcome == "ran")
        #expect(CheckFixture.walk([root]).compiled == [])
        let store = try IndexStore(
            libraryPath: IndexStore.toolchainLibraryPath(),
            storePath: IndexStore.storePath(layout: .check(scratchPath: Pipeline.scratchPath(for: root))),
        )
        let mainFiles = store.units().map { URL(fileURLWithPath: $0.mainFile).lastPathComponent }
        #expect(mainFiles.contains("answer.c"), "\(mainFiles)")
        #expect(mainFiles.contains("Welcome.swift"), "\(mainFiles)")
    }

    /*
     A check after an interface edit looks again at the edited module and every module that imports it, and reads the
     rest from the last check: the can't-miss direction first. Core's `text()` renamed breaks Feature's Welcome.swift,
     which never changed, so that error must come back, from Feature checked again because it imports Core. Then an
     interface edit to Feature alone leaves Core's records as they were, and the words say so.
     */
    @Test func aCheckAfterAnInterfaceEditLooksAgainAtEveryModuleItCouldChange() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")

        let greeting = try #require(CheckFixture.files["Sources/Core/Greeting.swift"])
        try CheckFixture.write(
            greeting.replacingOccurrences(of: "public func text()", with: "public func spoken()"),
            to: "Sources/Core/Greeting.swift",
            in: root,
        )
        let renamed = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(
            renamed.compilerFindings == ["Welcome.swift:5:error"],
            "\(renamed.typesBuild) \(renamed.of("finding"))",
        )

        try CheckFixture.write(greeting, to: "Sources/Core/Greeting.swift", in: root)
        _ = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        let count = try #require(CheckFixture.files["Sources/Feature/Count.swift"])
        try CheckFixture.write(count + "\nfunc counted() -> Int { 1 }\n", to: "Sources/Feature/Count.swift", in: root)
        let widened = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(widened.compilerFindings.isEmpty, "\(widened.of("finding"))")
        #expect(
            widened.typesBuild.contains("1 module no edit could change read from the last check's records"),
            "\(widened.typesBuild)",
        )
        #expect(widened.filesWithoutRecord.isEmpty)
    }

    /*
     A check that found an error still recorded every file, so the check after it looks again only at what changed:
     a file added with a type error is checked with its target alone, and the file removed again likewise, Core and
     Feature read from records both times. Before, any failure, or any file added or removed, checked everything.
     */
    @Test func aCheckAfterAnErrorOrAFileAddedLooksOnlyAtWhatChanged() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        _ = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        let plant = try #require(CheckFixture.swiftPlants.first)
        try CheckFixture.write(plant.text, to: plant.path, in: root)
        let planted = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(planted.compilerFindings == ["TypeError.swift:2:error"], "\(planted.of("finding"))")
        #expect(planted.typesBuild.contains("2 modules no edit could change"), "\(planted.typesBuild)")

        try FileManager.default.removeItem(at: root.appendingPathComponent(plant.path))
        let removed = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(removed.compilerFindings.isEmpty, "\(removed.of("finding"))")
        #expect(removed.typesBuild.contains("2 modules no edit could change"), "\(removed.typesBuild)")
        #expect(removed.filesWithoutRecord.isEmpty)
    }

    /*
     A header edit reaches every Swift module that imports its C target: CCore.h renaming `ccore_answer` breaks Core's
     Greeting.swift, which never changed, so Core is checked again because the build server lists the header as
     CCore's and Core imports CCore, and the error comes back. A scope that only reran the C checks missed it.
     */
    @Test func aHeaderEditLooksAgainAtEverySwiftModuleImportingItsTarget() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")

        try CheckFixture.write("int ccore_reply(void);\n", to: "Sources/CCore/include/CCore.h", in: root)
        let edited = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(
            edited.compilerFindings.contains("Greeting.swift:11:error"),
            "\(edited.typesBuild) \(edited.of("finding"))",
        )
        #expect(edited.filesWithoutRecord.isEmpty)
    }

    /* When prepare last wrote a module's interface into `Products`, the newest of its files' times there. */
    static func prepared(_ module: String, root: URL) -> Date? {
        let folder = Pipeline.scratchPath(for: root).appendingPathComponent(
            "out/Products/Debug/\(module).swiftmodule",
            isDirectory: true,
        )
        let files = (try? FileManager.default.contentsOfDirectory(at: folder, includingPropertiesForKeys: nil)) ?? []
        return files.filter { $0.pathExtension == "swiftmodule" }.compactMap {
            try? $0.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate
        }
        .max()
    }

    /*
     An importer sees the interface the check emitted, not the one prepare left. After an interface edit to Core,
     prepare covers only Core's own dependencies and leaves `Products`' Core as it was, so Feature's Welcome.swift,
     never edited, reports the renamed `text()` only by reading Core's interface from the check's own emit. The
     revert is seen the same way. Red under the mutant that keeps the emitted interfaces off the importers' search
     paths (`emittedAll` left empty): Feature reads the old Core and reports nothing.
     */
    @Test func anImporterSeesTheInterfaceTheCheckEmitted() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")
        let preparedCore = try #require(Self.prepared("Core", root: root))

        let greeting = try #require(CheckFixture.files["Sources/Core/Greeting.swift"])
        try CheckFixture.write(
            greeting.replacingOccurrences(of: "public func text()", with: "public func spoken()"),
            to: "Sources/Core/Greeting.swift",
            in: root,
        )
        let renamed = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(
            renamed.compilerFindings == ["Welcome.swift:5:error"],
            "\(renamed.typesBuild) \(renamed.of("finding"))",
        )
        #expect(
            renamed.typesBuild.contains("only the edited modules' own, and the check emitted the interfaces of Core"),
            "\(renamed.typesBuild)",
        )
        #expect(Self.prepared("Core", root: root) == preparedCore, "prepare emitted Core's interface again")

        try CheckFixture.write(greeting, to: "Sources/Core/Greeting.swift", in: root)
        let reverted = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(reverted.compilerFindings.isEmpty, "\(reverted.typesBuild) \(reverted.of("finding"))")
        #expect(reverted.filesWithoutRecord.isEmpty)
    }

    /*
     A header edit prepares everything again, so prepare emits the interfaces a narrowed prepare left older than their
     sources. After Core's interface edit was checked with prepare covering only Core's own dependencies, `Products`'
     Core is the one from before the edit; CCore.h renaming `ccore_answer` must have prepare emit Core again, and
     Greeting.swift report the call. Red under the mutant that narrows prepare for a C-family edit too (the
     `cFamilyEdits.isEmpty` condition dropped): `Products`' Core stays as it was, and the words say prepare covered
     only the edited modules' own dependencies.
     */
    @Test func aHeaderEditPreparesWhatANarrowedPrepareLeft() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        _ = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        let greeting = try #require(CheckFixture.files["Sources/Core/Greeting.swift"])
        try CheckFixture.write(
            greeting + "\npublic func greeted() -> Bool { true }\n",
            to: "Sources/Core/Greeting.swift",
            in: root,
        )
        let widened = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(widened.typesBuild.contains("only the edited modules' own"), "\(widened.typesBuild)")
        let leftByNarrowedPrepare = try #require(Self.prepared("Core", root: root))

        try CheckFixture.write("int ccore_reply(void);\n", to: "Sources/CCore/include/CCore.h", in: root)
        let edited = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(
            edited.compilerFindings.contains("Greeting.swift:11:error"),
            "\(edited.typesBuild) \(edited.of("finding"))",
        )
        #expect(!edited.typesBuild.contains("only the edited modules' own"), "\(edited.typesBuild)")
        let preparedAgain = try #require(Self.prepared("Core", root: root))
        #expect(preparedAgain > leftByNarrowedPrepare, "prepare left Core's interface from before the edit")
        #expect(InterfaceEmit.Ledger.stored(scratchPath: Pipeline.scratchPath(for: root)) == .init(targets: []))
        #expect(edited.filesWithoutRecord.isEmpty)
    }

    /* A package with a library nothing imports yet, Extra, beside Core, and Feature, which may import both. */
    static let staleFiles: [String: String] = [
        "Package.swift": """
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "Stale",
            platforms: [.macOS(.v15)],
            targets: [
                .target(name: "Core"),
                .target(name: "Extra"),
                .target(name: "Feature", dependencies: ["Core", "Extra"]),
            ]
        )

        """,
        "Sources/Core/Core.swift": "public func core() -> Int { 1 }\n",
        "Sources/Extra/Extra.swift": "public func extra() -> Int { 2 }\n",
        "Sources/Feature/Feature.swift": "import Core\n\nfunc feature() -> Int {\n    core()\n}\n",
    ]

    /* A package of these files in a fresh temporary folder, its root resolved. */
    static func package(_ files: [String: String], named name: String) throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-\(name)-\(UUID().uuidString)",
            isDirectory: true,
        )
        .resolvingSymlinksInPath()
        for (path, text) in files {
            try CheckFixture.write(text, to: path, in: root)
        }
        return root
    }

    /* One `--no-fix --types` run of the root. */
    static func types(_ root: URL) async throws -> XcodeProjectPipelineTests.Run {
        try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
    }

    /*
     A scoped module nothing imported, which a narrowed prepare left older in `Products` than its sources and the check
     did not emit, is never read stale (cache's review of 04d698c1f, hole 1). Extra gains `added()` while nothing
     imports it, so it is neither prepared nor emitted. Then Feature alone imports it and calls `added()`: Feature is
     independent, so on 04d698c1f it was checked alongside a prepare rebuilding Extra and read the old one.
     */
    @Test func aModuleNothingImportedIsNotReadStaleOnTheEarlyPath() async throws {
        let root = try Self.package(Self.staleFiles, named: "stale-early")
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await Self.types(root)
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")
        try CheckFixture.write(
            "public func extra() -> Int { 2 }\n\npublic func added() -> Int { 3 }\n",
            to: "Sources/Extra/Extra.swift",
            in: root,
        )
        let added = try await Self.types(root)
        #expect(added.compilerFindings.isEmpty, "\(added.typesBuild) \(added.of("finding"))")

        try CheckFixture.write(
            "import Core\nimport Extra\n\nfunc feature() -> Int {\n    core() + added()\n}\n",
            to: "Sources/Feature/Feature.swift",
            in: root,
        )
        let imported = try await Self.types(root)
        #expect(imported.compilerFindings.isEmpty, "\(imported.typesBuild) \(imported.of("finding"))")
        #expect(!imported.typesBuild.contains("only the edited modules' own"), "\(imported.typesBuild)")
        #expect(imported.filesWithoutRecord.isEmpty)
    }

    /*
     The same, with Core edited too, so Feature is not checked early and Extra is not prepared at all: on 04d698c1f the
     check read the old Extra every time, not only when it outran prepare.
     */
    @Test func aModuleNothingImportedIsNotReadStaleWhenSomethingDoes() async throws {
        let root = try Self.package(Self.staleFiles, named: "stale")
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await Self.types(root)
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")
        try CheckFixture.write(
            "public func extra() -> Int { 2 }\n\npublic func added() -> Int { 3 }\n",
            to: "Sources/Extra/Extra.swift",
            in: root,
        )
        let added = try await Self.types(root)
        #expect(added.compilerFindings.isEmpty, "\(added.typesBuild) \(added.of("finding"))")

        try CheckFixture.write(
            "public func core() -> Int { 1 }\n\npublic func core2() -> Int { 4 }\n",
            to: "Sources/Core/Core.swift",
            in: root,
        )
        try CheckFixture.write(
            "import Core\nimport Extra\n\nfunc feature() -> Int {\n    core() + added()\n}\n",
            to: "Sources/Feature/Feature.swift",
            in: root,
        )
        let imported = try await Self.types(root)
        #expect(imported.compilerFindings.isEmpty, "\(imported.typesBuild) \(imported.of("finding"))")
        #expect(imported.filesWithoutRecord.isEmpty)
    }

    /*
     Middle infers `made`'s type from Core, and nothing imports Middle: Core's `make()` turned String leaves Middle in
     the scope, unedited and unemitted, its `Products` copy still saying Int. Feature then imports Middle and Zed, Zed
     edited, so Middle is not prepared: the check must read `made` as String.
     */
    @Test func anUnimportedImportersInferredTypeIsNotReadStale() async throws {
        let root = try Self.package(
            [
                "Package.swift": """
                // swift-tools-version:6.2
                import PackageDescription

                let package = Package(
                    name: "Inferred",
                    platforms: [.macOS(.v15)],
                    targets: [
                        .target(name: "Core"),
                        .target(name: "Middle", dependencies: ["Core"]),
                        .target(name: "Zed"),
                        .target(name: "Feature", dependencies: ["Middle", "Zed"]),
                    ]
                )

                """,
                "Sources/Core/Core.swift": "public func make() -> Int { 1 }\n",
                "Sources/Middle/Middle.swift": "import Core\n\npublic let made = make()\n",
                "Sources/Zed/Zed.swift": "public func zed() -> Int { 1 }\n",
                "Sources/Feature/Feature.swift": "func feature() -> Int { 0 }\n",
            ],
            named: "inferred",
        )
        defer { XcodeProjectPipelineTests.remove(root) }
        let first = try await Self.types(root)
        #expect(first.compilerFindings.isEmpty, "\(first.of("finding"))")
        try CheckFixture.write("public func make() -> String { \"one\" }\n", to: "Sources/Core/Core.swift", in: root)
        let turned = try await Self.types(root)
        #expect(turned.compilerFindings.isEmpty, "\(turned.typesBuild) \(turned.of("finding"))")

        try CheckFixture.write(
            "public func zed() -> Int { 1 }\n\npublic func zed2() -> Int { 2 }\n",
            to: "Sources/Zed/Zed.swift",
            in: root,
        )
        try CheckFixture.write(
            "import Middle\nimport Zed\n\nfunc feature() -> String {\n    made\n}\n",
            to: "Sources/Feature/Feature.swift",
            in: root,
        )
        let imported = try await Self.types(root)
        #expect(imported.compilerFindings.isEmpty, "\(imported.typesBuild) \(imported.of("finding"))")
        #expect(imported.filesWithoutRecord.isEmpty)
    }

    /* Whether the seam has paused a run, and the signal that lets it go. */
    final class Pause: Sendable {
        let paused = Mutex(false)
        let release = DispatchSemaphore(value: 0)
    }

    /*
     Two runs on one scratch at once: the second waits for the first (cache's review of 04d698c1f, hole 2). Core's
     `newer()` was checked, and the ledger holds Core, since Uses imports it. Run A, after another interface edit to
     Core, is paused by the test seam right after it clears Core's folder and before it emits. Meanwhile Core goes back
     to the text the last check saw, and Uses starts calling `newer()` and Zed's new `zed2()`: run B's scope is Zed and
     Uses, so it prepares only Zed's dependencies, emits nothing of Core, and reads Core through the folder the ledger
     lists. Without the lock B finds the folder empty, falls through to `Products`' Core from before `newer()`, and
     reports a call that is there.
     */
    @Test func aSecondRunOnTheScratchWaitsForTheFirstsEmit() async throws {
        let root = try Self.package(
            [
                "Package.swift": """
                // swift-tools-version:6.2
                import PackageDescription

                let package = Package(
                    name: "Two",
                    platforms: [.macOS(.v15)],
                    targets: [
                        .target(name: "Core"),
                        .target(name: "Zed"),
                        .target(name: "Uses", dependencies: ["Core", "Zed"]),
                    ]
                )

                """,
                "Sources/Core/Core.swift": "public func core() -> Int { 1 }\n",
                "Sources/Zed/Zed.swift": "public func zed() -> Int { 1 }\n",
                "Sources/Uses/Uses.swift": "import Core\n\nfunc uses() -> Int {\n    core()\n}\n",
            ],
            named: "two-runs",
        )
        defer { XcodeProjectPipelineTests.remove(root) }
        _ = try await Self.types(root)
        let newer = "public func core() -> Int { 1 }\n\npublic func newer() -> Int { 2 }\n"
        try CheckFixture.write(newer, to: "Sources/Core/Core.swift", in: root)
        let listed = try await Self.types(root)
        #expect(listed.compilerFindings.isEmpty, "\(listed.of("finding"))")
        let scratch = Pipeline.scratchPath(for: root)
        #expect(InterfaceEmit.Ledger.stored(scratchPath: scratch)?.targets == ["Core"])

        let pause = Pause()
        let core = InterfaceEmit.Ledger.moduleDirectory(scratchPath: scratch, target: "Core").standardizedFileURL.path
        InterfaceEmit.cleared.withLock { seam in
            seam = { @Sendable directory in
                guard directory.standardizedFileURL.path == core,
                    pause.paused.withLock({ paused in
                        defer { paused = true }
                        return !paused
                    })
                else { return }
                pause.release.wait()
            }
        }
        defer { InterfaceEmit.cleared.withLock { $0 = nil } }
        try CheckFixture.write(
            newer + "\npublic func newest() -> Int { 3 }\n",
            to: "Sources/Core/Core.swift",
            in: root,
        )
        let first = Task { try await Self.types(root).compilerFindings }
        var waited = 0
        while !pause.paused.withLock({ $0 }), waited < 600 {
            try await Task.sleep(for: .milliseconds(100))
            waited += 1
        }
        try #require(pause.paused.withLock { $0 }, "run A never reached the seam")

        try CheckFixture.write(newer, to: "Sources/Core/Core.swift", in: root)
        try CheckFixture.write(
            "public func zed() -> Int { 1 }\n\npublic func zed2() -> Int { 4 }\n",
            to: "Sources/Zed/Zed.swift",
            in: root,
        )
        try CheckFixture.write(
            "import Core\nimport Zed\n\nfunc uses() -> Int {\n    newer() + zed2()\n}\n",
            to: "Sources/Uses/Uses.swift",
            in: root,
        )
        let second = Task { try await Self.types(root).compilerFindings }
        try await Task.sleep(for: .seconds(10))
        pause.release.signal()
        _ = try await first.value
        let findings = try await second.value
        #expect(findings.isEmpty, "run B read Core's emptied folder: \(findings)")
        #expect(InterfaceEmit.Ledger.stored(scratchPath: scratch)?.targets.contains("Core") == true)
    }

    /*
     An emit that fails leaves no ledger, so the next check prepares everything rather than trust what it listed. A
     file where Core's folder goes makes the emit fail; with it gone, the next check's words say prepare was not
     narrowed. Red under the mutant that keeps the ledger after a failed emit.
     */
    @Test func aFailedEmitMakesTheNextCheckPrepareEverything() async throws {
        let root = try CheckFixture.package()
        defer { XcodeProjectPipelineTests.remove(root) }
        _ = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        let greeting = try #require(CheckFixture.files["Sources/Core/Greeting.swift"])
        try CheckFixture.write(
            greeting.replacingOccurrences(of: "public func text()", with: "public func spoken()"),
            to: "Sources/Core/Greeting.swift",
            in: root,
        )
        let scratch = Pipeline.scratchPath(for: root)
        let blocker = InterfaceEmit.Ledger.moduleDirectory(scratchPath: scratch, target: "Core")
        try FileManager.default.createDirectory(
            at: blocker.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try Data("not a folder".utf8).write(to: blocker)
        await #expect {
            _ = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        } throws: { error in
            "\(error)".contains("its interface's folder could not be made ready")
        }
        #expect(InterfaceEmit.Ledger.stored(scratchPath: scratch) == nil)

        try FileManager.default.removeItem(at: blocker)
        let after = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(!after.typesBuild.contains("only the edited modules' own"), "\(after.typesBuild)")
        #expect(after.compilerFindings == ["Welcome.swift:5:error"], "\(after.of("finding"))")
    }

    /*
     A root that uses a local package whose tests are ours: App imports Shapes, and ShapesTests, which only Shapes'
     own session checks, holds a warning on line 6.
     */
    static let localPackageFiles: [String: String] = [
        "Package.swift": """
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "App",
            platforms: [.macOS(.v15)],
            dependencies: [.package(path: "Libraries/Shapes")],
            targets: [.target(name: "App", dependencies: [.product(name: "Shapes", package: "Shapes")])]
        )

        """,
        "Sources/App/Area.swift": """
        import Shapes

        func area() -> Double {
            Square(side: 2).area()
        }

        """,
        "Libraries/Shapes/Package.swift": """
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "Shapes",
            platforms: [.macOS(.v15)],
            products: [.library(name: "Shapes", targets: ["Shapes"])],
            targets: [.target(name: "Shapes"), .testTarget(name: "ShapesTests", dependencies: ["Shapes"])]
        )

        """,
        "Libraries/Shapes/Sources/Shapes/Square.swift": """
        public struct Square {
            public var side: Double

            public init(side: Double) {
                self.side = side
            }

            public func area() -> Double {
                side * side
            }
        }

        """,
        "Libraries/Shapes/Tests/ShapesTests/SquareTests.swift": """
        import Testing

        @testable import Shapes

        @Test func measures() {
            let unused = Square(side: 3)
            #expect(Square(side: 2).area() == 4)
        }

        """,
    ]

    /*
     A local package's session is skipped only when no edit could change what it checks, and skipping it loses
     nothing. An interface edit to App leaves Shapes' session out, and ShapesTests' old warning is still reported from
     its records. An interface edit inside the package skipped just before, a type error added to SquareTests, starts
     its session again and reports the error. And Square's `area()` renamed reaches across packages: App's Area.swift
     and ShapesTests' SquareTests.swift, neither edited since the last check, each report it.
     */
    @Test func aLocalPackageSessionIsSkippedOnlyWhenNoEditCouldChangeIt() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-local-\(UUID().uuidString)",
            isDirectory: true,
        )
        .resolvingSymlinksInPath()
        defer { XcodeProjectPipelineTests.remove(root) }
        for (path, text) in Self.localPackageFiles {
            try CheckFixture.write(text, to: path, in: root)
        }
        let skippedWords = "no build server started for the local package Shapes"
        let first = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(first.compilerFindings == ["SquareTests.swift:6:warning"], "\(first.of("finding"))")

        let area = try #require(Self.localPackageFiles["Sources/App/Area.swift"])
        try CheckFixture.write(
            area + "\nfunc perimeter() -> Double {\n    8\n}\n",
            to: "Sources/App/Area.swift",
            in: root,
        )
        let elsewhere = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(elsewhere.typesBuild.contains(skippedWords), "\(elsewhere.typesBuild)")
        #expect(elsewhere.compilerFindings == ["SquareTests.swift:6:warning"], "\(elsewhere.of("finding"))")
        #expect(elsewhere.filesWithoutRecord.isEmpty)

        let squareTests = "Libraries/Shapes/Tests/ShapesTests/SquareTests.swift"
        let tests = try #require(Self.localPackageFiles[squareTests])
        try CheckFixture.write(tests + "\nfunc broken() -> Int {\n    \"no\"\n}\n", to: squareTests, in: root)
        let inside = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(!inside.typesBuild.contains(skippedWords), "\(inside.typesBuild)")
        #expect(
            inside.compilerFindings == ["SquareTests.swift:6:warning", "SquareTests.swift:11:error"],
            "\(inside.typesBuild) \(inside.of("finding"))",
        )
        #expect(inside.filesWithoutRecord.isEmpty)

        let square = try #require(Self.localPackageFiles["Libraries/Shapes/Sources/Shapes/Square.swift"])
        try CheckFixture.write(
            square.replacingOccurrences(of: "public func area()", with: "public func surface()"),
            to: "Libraries/Shapes/Sources/Shapes/Square.swift",
            in: root,
        )
        let across = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--types"])
        #expect(!across.typesBuild.contains(skippedWords), "\(across.typesBuild)")
        /* The compiler says nothing of `unused` once the function holding it has an error, so line 6's warning goes. */
        #expect(
            across.compilerFindings.sorted() == [
                "Area.swift:4:error", "SquareTests.swift:11:error", "SquareTests.swift:7:error",
            ],
            "\(across.typesBuild) \(across.of("finding"))",
        )
        #expect(across.filesWithoutRecord.isEmpty)
    }

    /* The plugins SwiftPM compiled this run are named by the executables it left in the plugin cache since the run began. */
    @Test func thePluginsCompiledAreReadFromThePluginCache() throws {
        let scratch = CheckFixture.scratch()
        let cache = scratch.appendingPathComponent("plugins/cache", isDirectory: true)
        try FileManager.default.createDirectory(
            at: cache.appendingPathComponent("CudaBuild.dSYM"),
            withIntermediateDirectories: true,
        )
        let before = Date().addingTimeInterval(-60)
        FileManager.default.createFile(
            atPath: cache.appendingPathComponent("CudaBuild").path,
            contents: Data("#!/bin/sh\n".utf8),
            attributes: [.posixPermissions: 0o755],
        )
        FileManager.default.createFile(
            atPath: cache.appendingPathComponent("CudaBuild-state.json").path,
            contents: Data(),
        )
        #expect(TypesPhase.pluginsCompiled(scratchPath: scratch, since: before) == ["CudaBuild"])
        #expect(TypesPhase.pluginsCompiled(scratchPath: scratch, since: Date().addingTimeInterval(60)).isEmpty)
    }

    /*
     Every kind of diagnostic a build reports, the check reports the same, file by file, read through `CompilerRecords`
     from both. The check runs once with every kind planted. A build stops at its first failed task (measured: a C
     error left no Swift file compiled, and two failing Swift files left four unreached), so the build's record of
     each kind comes from its own incremental build with only that kind planted. Every other file is compared with a
     clean build's records, so the check also adds nothing a build does not say.
     */
    @Test func everyDiagnosticKindMatchesTheBuild() throws {
        let plants = CheckFixture.swiftPlants + [CheckFixture.cPlant]
        let checkedRoot = try CheckFixture.package(plants: plants)
        let checked = try CheckFixture.check(checkedRoot, scratch: CheckFixture.scratch())
        #expect(checked.swift.crashes.isEmpty)
        let fromCheck = try CheckFixture.diagnostics(checkedRoot, directories: checked.targetDirectories).byFile

        let builtRoot = try CheckFixture.package()
        let buildScratch = CheckFixture.scratch()
        let clean = try CheckFixture.build(builtRoot, scratch: buildScratch)
        try #require(clean.succeeded, "the clean fixture does not build: \(clean.standardError)")
        var fromBuild = try CheckFixture.diagnostics(
            builtRoot,
            directories: CheckFixture.buildDirectories(scratch: buildScratch),
        )
        .byFile
        for plant in plants {
            try CheckFixture.write(plant.text, to: plant.path, in: builtRoot)
            _ = try CheckFixture.build(builtRoot, scratch: buildScratch)
            let planted = try CheckFixture.diagnostics(
                builtRoot,
                directories: CheckFixture.buildDirectories(scratch: buildScratch),
            )
            fromBuild[plant.path] = planted.byFile[plant.path] ?? []
            try FileManager.default.removeItem(at: builtRoot.appendingPathComponent(plant.path))
        }

        for plant in plants {
            #expect(
                fromBuild[plant.path, default: []].contains { $0.contains(plant.message) },
                "the build's record of \(plant.path) lacks \"\(plant.message)\": \(fromBuild[plant.path, default: []])",
            )
        }
        #expect(fromCheck.keys.sorted() == fromBuild.keys.sorted())
        for file in fromCheck.keys.sorted() {
            #expect(
                fromCheck[file] == fromBuild[file],
                "\(file): the check said \(fromCheck[file] ?? []), the build \(fromBuild[file] ?? [])",
            )
        }
    }
}
