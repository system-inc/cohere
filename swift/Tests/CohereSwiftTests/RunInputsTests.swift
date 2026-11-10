import Foundation
import Testing

@testable import CohereSwift

/*
 The inputs record is the whole of what the front door's replay proves unchanged (#nkk91tv), so each kind of input a
 verdict is read from has a test here: record the inputs, make one change of that kind, and the replay's check, which
 stats every recorded path again, must see something move. A kind the record leaves out is a replay that repeats a
 stale verdict, the one failure cohere exists to end, so each test is the control for one line of `RunInputs.record`.

 `moved(after:)` is the replay's check restated: every recorded file or directory is there with the same size (a
 file's), modification time, change time and identity, and every recorded absent path is still not there, the same
 comparison `program.RunCacheInput.stillMatches` makes in the front door.
 */
struct RunInputsTests {
    /*
     A package inside a repository at `repository/package`, so a `.gitignore` above the package is the repository's
     and not walked, with a path dependency beside it at `repository/Shared`, outside the package's root, and a toolchain
     laid out as an Xcode lays one out. The global git settings are read from a `home` of the fixture's own.
     */
    struct Project {
        var base: URL
        var repository: URL
        var root: URL
        var shared: URL
        var home: URL
        var developer: URL
        var toolchainExecutable: URL
        var pathDirectory: URL
        var settingsFile: URL

        init() throws {
            base = FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-inputs-\(UUID().uuidString)",
                isDirectory: true,
            )
            repository = base.appendingPathComponent("repository", isDirectory: true)
            root = repository.appendingPathComponent("package", isDirectory: true)
            shared = repository.appendingPathComponent("Shared", isDirectory: true)
            home = base.appendingPathComponent("home", isDirectory: true)
            developer = base.appendingPathComponent("Xcode.app/Contents/Developer", isDirectory: true)
            toolchainExecutable = developer.appendingPathComponent(
                "Toolchains/XcodeDefault.xctoolchain/usr/bin/swift"
            )
            pathDirectory = base.appendingPathComponent("bin", isDirectory: true)
            settingsFile = root.appendingPathComponent("CohereSettings.json")
            try Self.write(
                [
                    ".gitignore": "Ignored.swift\n",
                    ".git/info/exclude": "",
                    "package/Package.swift": "// swift-tools-version:6.2\n",
                    "package/Package.resolved": "{\"pins\":[]}\n",
                    "package/CohereSettings.json": "{\"swift\":{\"rules\":{}}}\n",
                    "package/Sources/App/App.swift": "struct App {}\n",
                    "package/Sources/App/Second.swift": "struct Second {}\n",
                    "package/project.yml": "name: App\n",
                    "package/Resources/Assets.xcassets/Contents.json": "{}\n",
                    "package/Resources/Assets.xcassets/AppIcon.appiconset/Contents.json": "{}\n",
                    "Shared/Package.swift": "// swift-tools-version:6.2\n",
                    "Shared/Sources/Shared/Shared.swift": "struct Shared {}\n",
                ],
                under: repository,
            )
            try FileManager.default.createDirectory(at: home, withIntermediateDirectories: true)
            try Self.write(
                [
                    "Toolchains/XcodeDefault.xctoolchain/usr/bin/swift": "#!/bin/sh\n",
                    "Toolchains/XcodeDefault.xctoolchain/usr/bin/swift-frontend": "frontend 1\n",
                    "Toolchains/XcodeDefault.xctoolchain/ToolchainInfo.plist": "<plist>1</plist>\n",
                    "Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/SDKSettings.json": "{\"Version\":\"27.0\"}\n",
                    "../version.plist": "<plist>27.0</plist>\n",
                ],
                under: developer,
            )
            try Self.write(["swift": "#!/bin/sh\nexec swift \"$@\"\n"], under: pathDirectory)
            try FileManager.default.setAttributes(
                [.posixPermissions: 0o755],
                ofItemAtPath: pathDirectory.appendingPathComponent("swift").path,
            )
        }

        static func write(_ files: [String: String], under directory: URL) throws {
            for (path, text) in files {
                let url = directory.appendingPathComponent(path).standardizedFileURL
                try FileManager.default.createDirectory(
                    at: url.deletingLastPathComponent(),
                    withIntermediateDirectories: true,
                )
                try text.write(to: url, atomically: true, encoding: .utf8)
            }
        }

        /*
         The package as SwiftPM describes it: one target of two files, and the path dependency beside it, loaded unless
         `withShared` is false.
         */
        func package(
            projectKind: PackageModel.ProjectKind = .swiftPackage,
            withShared: Bool = true,
        ) throws -> PackageModel {
            let dump = #"{"toolsVersion":{"_version":"6.2.0"},"swiftLanguageVersions":null,"targets":[]}"#
            var model = try PackageModel(
                root: root,
                describeJson: Data(
                    #"{"name":"App","targets":[{"name":"App","type":"library","path":"Sources/App","sources":["App.swift","Second.swift"]}],"dependencies":[{"identity":"shared","type":"fileSystem","path":"\#(shared.path)"}]}"#
                        .utf8
                ),
                dumpPackageJson: Data(dump.utf8),
            )
            let sharedModel = try PackageModel(
                root: shared,
                describeJson: Data(
                    #"{"name":"Shared","targets":[{"name":"Shared","type":"library","path":"Sources/Shared","sources":["Shared.swift"]}]}"#
                        .utf8
                ),
                dumpPackageJson: Data(dump.utf8),
            )
            model.localPackages = withShared ? [sharedModel] : []
            model.projectKind = projectKind
            return model
        }

        /* The record a run over the package would write now. */
        func record(
            projectKind: PackageModel.ProjectKind = .swiftPackage,
            withShared: Bool = true,
        ) throws -> InputsRecord {
            let package = try package(projectKind: projectKind, withShared: withShared)
            let fileSet = try FileSet.build(package: package, environment: ["HOME": home.path])
            return RunInputs.record(
                package: package,
                fileSet: fileSet,
                boundary: repository,
                settingsFile: settingsFile,
                toolchainExecutable: toolchainExecutable.path,
                environment: ["PATH": pathDirectory.path],
            )
        }

        func remove() {
            do {
                try FileManager.default.removeItem(at: base)
            }
            catch {
                /* Ignored on purpose: a leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
            }
        }
    }

    /* What the replay's check compares for one recorded path, as text, so two readings compare as equal or not. */
    static func signature(of path: String, recordedAbsent: Bool) -> String {
        let url = URL(fileURLWithPath: path).resolvingSymlinksInPath()
        let keys: Set<URLResourceKey> = [
            .isDirectoryKey, .fileSizeKey, .contentModificationDateKey, .attributeModificationDateKey,
            .fileResourceIdentifierKey,
        ]
        guard FileManager.default.fileExists(atPath: url.path), let values = try? url.resourceValues(forKeys: keys)
        else {
            return "absent"
        }
        if recordedAbsent {
            return "there"
        }
        let size = values.isDirectory == true ? "" : "\(values.fileSize ?? -1)"
        let modified = values.contentModificationDate?.timeIntervalSinceReferenceDate ?? -1
        let changed = values.attributeModificationDate?.timeIntervalSinceReferenceDate ?? -1
        return
            "\(values.isDirectory == true)\t\(size)\t\(modified)\t\(changed)\t\(String(describing: values.fileResourceIdentifier))"
    }

    static func signatures(of record: InputsRecord) -> [String: String] {
        var signatures: [String: String] = [:]
        for path in record.files + record.directories {
            signatures[path] = signature(of: path, recordedAbsent: false)
        }
        for path in record.absent {
            signatures[path] = signature(of: path, recordedAbsent: true)
        }
        return signatures
    }

    /*
     The recorded paths whose signature moved across `change`, as the replay would find them: a recorded run, then the
     change, then the check. Empty means the replay would repeat the recorded verdict over the changed tree.
     */
    static func moved(
        in project: Project,
        projectKind: PackageModel.ProjectKind = .swiftPackage,
        after change: () throws -> Void,
    ) throws -> Moved {
        let record = try project.record(projectKind: projectKind)
        let before = signatures(of: record)
        /* The file systems' clocks are fine-grained, and a change in the same instant as the reading would still move a change time; the pause only keeps a reader of a failure from wondering. */
        Thread.sleep(forTimeInterval: 0.01)
        try change()
        let after = signatures(of: record)
        return Moved(paths: before.keys.filter { before[$0] != after[$0] }.sorted())
    }

    /* Recorded paths, the ones a change moved or the ones a record names absent. */
    struct Moved {
        var paths: [String]

        var isEmpty: Bool { paths.isEmpty }

        func contains(_ path: String) -> Bool { paths.contains(path) }

        /* Whether the path at `url` is among them, however either spells the links on the way. */
        func holds(_ url: URL) -> Bool {
            let wanted = url.resolvingSymlinksInPath().path
            return paths.contains { URL(fileURLWithPath: $0).resolvingSymlinksInPath().path == wanted }
        }
    }

    static func rewrite(_ url: URL, _ text: String) throws {
        try text.write(to: url, atomically: false, encoding: .utf8)
    }

    /* The control: with nothing changed nothing moves, and the record is the same twice, or every test below would pass on noise. */
    @Test func nothingMovesWhenNothingChanges() throws {
        let project = try Project()
        defer { project.remove() }
        let first = try project.record()
        #expect(try Self.moved(in: project) {}.isEmpty)
        #expect(try project.record() == first)
        #expect(!first.files.isEmpty && !first.directories.isEmpty)
    }

    @Test func aSourceEditIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let source = project.root.appendingPathComponent("Sources/App/App.swift")
        #expect(try !Self.moved(in: project) { try Self.rewrite(source, "struct App { let edited = 1 }\n") }.isEmpty)
    }

    /* A file no record names yet: only the listing of the directory it lands in can say it arrived. */
    @Test func aNewFileIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let moved = try Self.moved(in: project) {
            try Self.rewrite(project.root.appendingPathComponent("Sources/App/Third.swift"), "struct Third {}\n")
        }
        #expect(moved.holds(project.root.appendingPathComponent("Sources/App")))
    }

    @Test func aDeletedFileIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        #expect(
            try !Self.moved(in: project) {
                try FileManager.default.removeItem(at: project.root.appendingPathComponent("Sources/App/Second.swift"))
            }
            .isEmpty
        )
    }

    @Test func aChangedResolvedFileIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let resolved = project.root.appendingPathComponent("Package.resolved")
        #expect(
            try !Self.moved(in: project) { try Self.rewrite(resolved, "{\"pins\":[{\"identity\":\"x\"}]}\n") }.isEmpty
        )
        /* One appearing where there was none moves the pins as surely as an edit. */
        try FileManager.default.removeItem(at: resolved)
        #expect(try !Self.moved(in: project) { try Self.rewrite(resolved, "{\"pins\":[]}\n") }.isEmpty)
    }

    @Test func aChangedManifestIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let manifest = project.root.appendingPathComponent("Package.swift")
        #expect(
            try !Self.moved(in: project) { try Self.rewrite(manifest, "// swift-tools-version:6.3\n// edited\n") }
                .isEmpty
        )
    }

    /* The settings named by flag can live anywhere, so it is named by itself and not only by a walk that may not reach it. */
    @Test func aChangedSettingsFileIsNoticed() throws {
        var project = try Project()
        defer { project.remove() }
        #expect(
            try !Self.moved(in: project) {
                try Self.rewrite(
                    project.settingsFile,
                    "{\"swift\":{\"rules\":{\"cohere-swift/force-cast\":\"off\"}}}\n",
                )
            }
            .isEmpty
        )
        project.settingsFile = project.base.appendingPathComponent("Elsewhere/CohereSettings.json")
        try Project.write(["Elsewhere/CohereSettings.json": "{}\n"], under: project.base)
        let moved = try Self.moved(in: project) { try Self.rewrite(project.settingsFile, "{\"swift\":{}}\n") }
        #expect(moved.holds(project.settingsFile))
    }

    /* The repository's `.gitignore` sits above the package, where no walk goes, and decides what is ours all the same, as `info/exclude` does. */
    @Test func aChangedIgnoreFileIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let above = project.repository.appendingPathComponent(".gitignore")
        #expect(try Self.moved(in: project) { try Self.rewrite(above, "Ignored.swift\nApp.swift\n") }.holds(above))
        let exclude = project.repository.appendingPathComponent(".git/info/exclude")
        #expect(try Self.moved(in: project) { try Self.rewrite(exclude, "Second.swift\n") }.holds(exclude))
        #expect(
            try !Self.moved(in: project) {
                try Self.rewrite(project.root.appendingPathComponent("Sources/.gitignore"), "App.swift\n")
            }
            .isEmpty
        )
    }

    /* `git add -f` moves only the index, and the file it tracks is ours from then on, whatever the ignore files say. */
    @Test func aChangedIndexIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let index = project.repository.appendingPathComponent(".git/index")
        #expect(
            try Self.moved(in: project) { try GitIndexTests.indexBytes(paths: ["Ignored.swift"]).write(to: index) }
                .holds(index)
        )
    }

    /*
     The index the file set read whole is named under `trackedIgnored` too (#jem2gsa), so the front door keys it on what
     it decides rather than on bytes every `git status` rewrites: with the repository's root, and the directory the
     file set asked about, the package's own below the root, the root itself for a package at the root, and the root
     again when a path dependency beside the package was asked about too, since a narrower scope would replay stale.
     */
    @Test func theIndexIsNamedWithTheScopeItsAnswersWereAskedFor() throws {
        let project = try Project()
        defer { project.remove() }
        let repository = project.repository.resolvingSymlinksInPath()
        try GitIndexTests.indexBytes(paths: ["package/Package.swift"])
            .write(to: project.repository.appendingPathComponent(".git/index"))
        func named(_ repository: URL, scope: String) -> [InputsRecord.TrackedIgnored] {
            let index = RunInputs.path(repository.appendingPathComponent(".git/index"))
            return [InputsRecord.TrackedIgnored(index: index, root: RunInputs.path(repository), scope: scope)]
        }

        let below = try project.record(withShared: false)
        #expect(below.trackedIgnored == named(repository, scope: "package"))
        #expect(below.files.contains(RunInputs.path(repository.appendingPathComponent(".git/index"))))
        #expect(try project.record().trackedIgnored == named(repository, scope: ""))

        /* A repository of the package's own: its index is read, and the path dependency, outside it, asks nothing. */
        try Project.write([".git/info/exclude": ""], under: project.root)
        try GitIndexTests.indexBytes(paths: ["Package.swift"])
            .write(to: project.root.appendingPathComponent(".git/index"))
        #expect(try project.record().trackedIgnored == named(project.root.resolvingSymlinksInPath(), scope: ""))
    }

    /* An index the reader fell back from stays keyed on its bytes, a plain file among the inputs, as it was. */
    @Test func anIndexReadWithAFallbackIsNotNamedForItsDigest() throws {
        let project = try Project()
        defer { project.remove() }
        let index = project.repository.appendingPathComponent(".git/index")
        try Data("not an index".utf8).write(to: index)
        let record = try project.record(withShared: false)
        #expect(record.trackedIgnored.isEmpty)
        #expect(record.files.contains(RunInputs.path(index.resolvingSymlinksInPath())))
    }

    /*
     The front door's digest reads the repository's `.gitignore` files and a `.git` directory's `info/exclude`, case
     sensitively, so a path excluded by anything else, here the global excludes file or `core.ignorecase`'s folding,
     is one whose tracking no digest would see: the index stays keyed on its bytes. Excluded by a line the digest
     reads, it is named, `core.ignorecase` set or not.
     */
    @Test func anExclusionTheDigestCannotSeeKeepsTheIndexOnItsBytes() throws {
        let project = try Project()
        defer { project.remove() }
        try GitIndexTests.indexBytes(paths: ["package/Package.swift"])
            .write(to: project.repository.appendingPathComponent(".git/index"))
        try Project.write(["Sources/App/Scratch.swift": "struct Scratch {}\n"], under: project.root)
        #expect(try project.record(withShared: false).trackedIgnored.count == 1, "the control: nothing is excluded")

        try Project.write([".config/git/ignore": "Scratch.swift\n"], under: project.home)
        #expect(try project.record(withShared: false).trackedIgnored.isEmpty, "the global excludes file excluded it")
        try FileManager.default.removeItem(at: project.home.appendingPathComponent(".config/git/ignore"))

        try Project.write(
            [".git/config": "[core]\n\tignorecase = true\n", ".gitignore": "Ignored.swift\nscratch.swift\n"],
            under: project.repository,
        )
        #expect(try project.record(withShared: false).trackedIgnored.isEmpty, "only a folded match excluded it")
        try Project.write([".gitignore": "Ignored.swift\nScratch.swift\n"], under: project.repository)
        #expect(try project.record(withShared: false).trackedIgnored.count == 1, "the repository's line excluded it")
    }

    /* The path dependency sits outside the package's root, so only its own walk sees a file added to it. */
    @Test func aLocalPackagesSourceIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let source = project.shared.appendingPathComponent("Sources/Shared/Shared.swift")
        #expect(
            try Self.moved(in: project) { try Self.rewrite(source, "struct Shared { let edited = 1 }\n") }.holds(source)
        )
        /* Its manifest is no file of ours and no target's source, so only its package's own walk and manifest name it. */
        let manifest = project.shared.appendingPathComponent("Package.swift")
        #expect(
            try Self.moved(in: project) { try Self.rewrite(manifest, "// swift-tools-version:6.3\n// edited\n") }
                .holds(manifest)
        )
        let listing = RunInputs.path(project.shared.appendingPathComponent("Sources/Shared"))
        #expect(
            try Self.moved(in: project) {
                try Self.rewrite(
                    project.shared.appendingPathComponent("Sources/Shared/Another.swift"),
                    "struct Another {}\n",
                )
            }
            .contains(listing)
        )
    }

    @Test func aChangedSpecificationIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let specification = project.root.appendingPathComponent("project.yml")
        #expect(
            try Self.moved(in: project, projectKind: .xcodeGen) {
                try Self.rewrite(specification, "name: App\ntargets: {}\n")
            }
            .holds(specification)
        )
    }

    /* An asset catalog generates symbols the type-checker reads, so a color added to one is a new input. */
    @Test func anAssetInAnXcodeRootIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let catalog = project.root.appendingPathComponent("Resources/Assets.xcassets")
        let moved = try Self.moved(in: project, projectKind: .xcodeGen) {
            try Project.write(["Accent.colorset/Contents.json": "{\"colors\":[]}\n"], under: catalog)
        }
        #expect(moved.holds(catalog))
        let icon = catalog.appendingPathComponent("AppIcon.appiconset/Contents.json")
        #expect(
            try Self.moved(in: project, projectKind: .xcodeGen) { try Self.rewrite(icon, "{\"images\":[]}\n") }.holds(
                icon
            )
        )
    }

    /*
     An Xcode root's build reads pins that may live only in the engine's scratch (cache's review): a spec's bundle is
     generated afresh, so its build starts from the copy the last build kept, and a committed bundle under `--no-fix`
     builds through the scratch's workspace. The pins named are the ones the build starts from in each case, and a
     pin changed there moves the record.
     */
    @Test func anXcodeRootNamesThePinsItsBuildRead() throws {
        try XcodeTypesPhaseTests.withFolder { folder in
            let app = [XcodeTypesPhaseTests.target("App", "application", sdkRoot: "iphoneos")]
            let generated = XcodeTypesPhaseTests.phase(app, in: folder)
            #expect(RunInputs.pinsRead(by: generated) == [generated.keptResolved])
            try XcodeTypesPhaseTests.write("{\"pins\":[\"kept\"]}", to: generated.keptResolved)
            generated.restoreResolved()
            #expect(
                try Data(contentsOf: generated.resolvedInBuild) == Data(contentsOf: generated.keptResolved),
                "the generated bundle's build did not start from the kept pins, so naming them proves nothing",
            )
            /* A root of its own: the fixture's scratch is not hidden as the engine's is, and a walk of it would name the pins whatever `pins` says. */
            let project = folder.appendingPathComponent("project", isDirectory: true)
            try FileManager.default.createDirectory(at: project, withIntermediateDirectories: true)
            let record = RunInputs.record(
                package: PackageModel(name: "Fixture", root: project, toolsVersion: "", targets: []),
                fileSet: FileSet(filesInPackage: [], owned: [], excluded: [], note: ""),
                pins: RunInputs.pinsRead(by: generated),
                boundary: folder,
                settingsFile: folder.appendingPathComponent("CohereSettings.json"),
                toolchainExecutable: nil,
                environment: [:],
            )
            let before = Self.signatures(of: record)
            Thread.sleep(forTimeInterval: 0.01)
            try XcodeTypesPhaseTests.write("{\"pins\":[\"moved\"]}", to: generated.keptResolved)
            let after = Self.signatures(of: record)
            #expect(Moved(paths: before.keys.filter { before[$0] != after[$0] }).holds(generated.keptResolved))

            let throughWorkspace = XcodeTypesPhaseTests.phase(
                app,
                in: folder,
                resolutionAllowed: false,
                kind: .xcodeProject,
            )
            #expect(RunInputs.pinsRead(by: throughWorkspace) == [throughWorkspace.resolvedInBuild])
            #expect(
                throughWorkspace.resolvedInBuild != throughWorkspace.resolvedInBundle,
                "the build reads the workspace's",
            )
            let inPlace = XcodeTypesPhaseTests.phase(app, in: folder, kind: .xcodeProject)
            #expect(RunInputs.pinsRead(by: inPlace) == [inPlace.resolvedInBundle])

            /* Pins the repository commits replace whatever the build holds, so they are the ones named. */
            try XcodeTypesPhaseTests.write("{}", to: throughWorkspace.resolvedInBundle)
            let committed = try #require(throughWorkspace.committedResolved)
            #expect(RunInputs.pinsRead(by: throughWorkspace) == [committed])
            throughWorkspace.restoreResolved()
            #expect(try Data(contentsOf: throughWorkspace.resolvedInBuild) == Data(contentsOf: committed))
        }
    }

    /*
     A toolchain is named by its files, so a new one in the same place moves them: the compiler beside the `swift` xcrun
     names, the toolchain's own description, the Xcode around it, an SDK, and the `swift` that `PATH` finds.
     */
    @Test func aNewToolchainIsNoticed() throws {
        let project = try Project()
        defer { project.remove() }
        let changed = [
            project.toolchainExecutable.deletingLastPathComponent().appendingPathComponent("swift-frontend"),
            project.developer.appendingPathComponent("Toolchains/XcodeDefault.xctoolchain/ToolchainInfo.plist"),
            project.developer.deletingLastPathComponent().appendingPathComponent("version.plist"),
            project.developer.appendingPathComponent(
                "Platforms/MacOSX.platform/Developer/SDKs/MacOSX.sdk/SDKSettings.json"
            ),
            project.pathDirectory.appendingPathComponent("swift"),
        ]
        for file in changed {
            #expect(
                try Self.moved(in: project) { try Self.rewrite(file, "a new toolchain's \(file.lastPathComponent)\n") }
                    .holds(file),
                "rewriting \(file.path) moved no input",
            )
        }
    }

    /*
     What a walk must not name: a git directory, which moves with every git command (only its `info/exclude`, config and
     index are named, as files the ignore rules read), cohere's own cache, which the run writes after its record is
     taken, and Finder's notes.
     */
    @Test func theWalkLeavesOutWhatMovesOnItsOwn() throws {
        let project = try Project()
        defer { project.remove() }
        try Project.write([".git/HEAD": "ref: refs/heads/main\n", ".git/index": "index"], under: project.repository)
        try Project.write([".cache/cohere/swift/build-inputs.json": "{}\n", ".DS_Store": "x"], under: project.root)
        let record = try project.record()
        let gitDirectory = RunInputs.path(project.repository.appendingPathComponent(".git")) + "/"
        let cache = RunInputs.path(project.root.appendingPathComponent(".cache/cohere"))
        for path in record.files + record.directories + record.absent {
            #expect(!path.hasPrefix(cache), "the record named \(path), in the cache the run writes")
            #expect(!path.hasSuffix("/.DS_Store"), "the record named \(path)")
            if path.hasPrefix(gitDirectory) {
                #expect(
                    ["info/exclude", "config", "index"].contains(String(path.dropFirst(gitDirectory.count))),
                    "the record named \(path), in the git directory",
                )
            }
        }
        #expect(Moved(paths: record.absent).holds(project.root.appendingPathComponent(".swift-format")))
        #expect(Moved(paths: record.absent).holds(project.repository.appendingPathComponent(".swift-format")))
    }

    /* A run of the whole pipeline ends on its inputs record, then its summary, and the record names what it checked. */
    @Test func aCheckRunNamesItsInputsJustBeforeItsSummary() async throws {
        let run = try await PipelineControlTests.run(source: PipelineControlTests.cleanSource)
        let kinds = run.records.compactMap { $0["kind"] as? String }
        #expect(Array(kinds.suffix(2)) == ["inputs", "summary"])
        let files = try #require(run.of("inputs").first?["files"] as? [String])
        #expect(files.contains { $0.hasSuffix("/Sources/Control/Control.swift") })
        #expect(files.contains { $0.hasSuffix("/Package.swift") })
    }
}
