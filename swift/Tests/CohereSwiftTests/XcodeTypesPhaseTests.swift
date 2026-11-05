import Foundation
import Testing

@testable import CohereSwift

/*
 The parts of the Xcode types phase that need no xcodebuild: which targets it builds and how, what it says when a
 build fails, which package pins it builds from and keeps, and the local packages it reads from a build's package
 state. The builds themselves are `XcodeProjectPipelineTests`'.
 */
@Suite
struct XcodeTypesPhaseTests {
    /* A fresh folder for one test, removed after it. */
    static func withFolder(_ body: (URL) throws -> Void) throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-xcode-types-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer {
            do {
                try FileManager.default.removeItem(at: folder)
            }
            catch {
                /* Ignored on purpose: a leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
            }
        }
        try body(folder.resolvingSymlinksInPath())
    }

    static func target(_ name: String, _ productType: String, sdkRoot: String?) -> XcodeProjectFile.Target {
        XcodeProjectFile.Target(
            name: name,
            productType: "com.apple.product-type.\(productType)",
            sources: [],
            swiftVersion: "6.0",
            sdkRoot: sdkRoot,
        )
    }

    /*
     A phase over these targets, each owning one file, with its scratch under `folder`. A generated project's bundle
     is in the scratch, as `XcodeGeneration` makes it; a committed one stands in `folder`.
     */
    static func phase(
        _ targets: [XcodeProjectFile.Target],
        in folder: URL,
        resolutionAllowed: Bool = true,
        kind: PackageModel.ProjectKind = .xcodeGen,
    ) -> XcodeTypesPhase {
        let bundle = (kind == .xcodeGen ? folder.appendingPathComponent("scratch/xcode") : folder)
            .appendingPathComponent("Fixture.xcodeproj", isDirectory: true)
        let projectFile = XcodeProjectFile(sourceRoot: folder, targets: targets, localPackages: [])
        var model = PackageModel(name: "Fixture", root: folder, toolsVersion: "", targets: [])
        model.projectKind = kind
        return XcodeTypesPhase(
            package: model,
            project: XcodeProject.Loaded(
                model: model,
                bundle: bundle,
                projectFile: projectFile,
                definition: folder.appendingPathComponent("project.yml"),
            ),
            files: targets.map {
                FileSet.OwnedFile(
                    url: folder.appendingPathComponent("\($0.name)/File.swift"),
                    targetName: $0.name,
                    targetKind: XcodeProject.kind(of: $0.productType),
                    packageRoot: folder,
                )
            },
            scratchPath: folder.appendingPathComponent("scratch", isDirectory: true),
            resolutionAllowed: resolutionAllowed,
            toolchain: "swiftlang-test",
            runner: ProcessRunner(),
        )
    }

    /*
     Applications and tools build before the test bundles that host in them, each for its SDK's platform, and a test
     bundle builds for testing. A target whose SDKROOT names no platform, or that sets none, is not built and says why.
     */
    @Test func plansOneBuildPerTargetForItsPlatform() throws {
        try Self.withFolder { folder in
            let phase = Self.phase(
                [
                    Self.target("AppTests", "bundle.unit-test", sdkRoot: "iphoneos"),
                    Self.target("App", "application", sdkRoot: "iphoneos"),
                    Self.target("Odd", "framework", sdkRoot: "auto"),
                    Self.target("Bare", "framework", sdkRoot: nil),
                    Self.target("Tool", "tool", sdkRoot: "macosx"),
                    Self.target("Vision", "application", sdkRoot: "xros"),
                ],
                in: folder,
            )
            let plan = phase.buildPlan
            #expect(
                plan.builds.map { "\($0.target):\($0.platform):\($0.action)" } == [
                    "App:iOS:build", "Tool:macOS:build", "Vision:visionOS:build", "AppTests:iOS:build-for-testing",
                ]
            )
            #expect(
                plan.unbuilt.map { "\($0.target): \($0.reason)" } == [
                    "Odd: its SDKROOT auto names no platform this engine builds for",
                    "Bare: it sets no SDKROOT, so there is no platform to build it for",
                ]
            )
        }
    }

    /* Every write in the scratch, nothing signed, and run scripts held to their declared files. */
    @Test func buildsIntoTheScratchWithoutSigning() throws {
        try Self.withFolder { folder in
            let phase = Self.phase([Self.target("App", "application", sdkRoot: "iphoneos")], in: folder)
            let build = try #require(phase.buildPlan.builds.first)
            let scratch = folder.appendingPathComponent("scratch").path
            #expect(
                phase.arguments(for: build) == [
                    "-project", scratch + "/xcode/Fixture.xcodeproj", "-scheme", "App", "-destination",
                    "generic/platform=iOS", "-derivedDataPath", scratch + "/xcode-build",
                    "-clonedSourcePackagesDirPath",
                    scratch + "/xcode-packages", "-configuration", "Debug", "CODE_SIGNING_ALLOWED=NO",
                    "ENABLE_USER_SCRIPT_SANDBOXING=YES", "build",
                ]
            )
        }
    }

    /*
     `--no-fix` resolves into the scratch (#s8p20fe): a generated bundle is built as it is, since it is in the
     scratch, and a committed one through the scratch's workspace, so its pins land there. Only a resolution the
     repository commits holds the build to it. A plain run builds a committed bundle where it stands.
     */
    @Test func noFixResolvesIntoTheScratchUnlessTheRepositoryCommitsPins() throws {
        try Self.withFolder { folder in
            let app = [Self.target("App", "application", sdkRoot: "iphoneos")]
            let generated = Self.phase(app, in: folder, resolutionAllowed: false)
            let build = try #require(generated.buildPlan.builds.first)
            #expect(generated.arguments(for: build).prefix(2) == ["-project", generated.project.bundle.path])
            #expect(!generated.arguments(for: build).contains("-disableAutomaticPackageResolution"))

            let committed = Self.phase(app, in: folder, resolutionAllowed: false, kind: .xcodeProject)
            let workspace = folder.appendingPathComponent("scratch/xcode-workspace/Fixture.xcworkspace").path
            #expect(committed.arguments(for: build).prefix(2) == ["-workspace", workspace])
            #expect(!committed.arguments(for: build).contains("-disableAutomaticPackageResolution"))
            #expect(committed.resolvedInBuild.path == workspace + "/xcshareddata/swiftpm/Package.resolved")

            let fixing = Self.phase(app, in: folder, kind: .xcodeProject)
            #expect(fixing.arguments(for: build).prefix(2) == ["-project", folder.path + "/Fixture.xcodeproj"])
            #expect(fixing.resolvedInBuild == fixing.resolvedInBundle)

            try Self.write("{}", to: committed.resolvedInBundle)
            #expect(
                committed.committedResolved == committed.resolvedInBundle,
                "outside a repository a bundle's pins are its own",
            )
            #expect(committed.arguments(for: build).contains("-disableAutomaticPackageResolution"))
            #expect(!fixing.arguments(for: build).contains("-disableAutomaticPackageResolution"))
        }
    }

    static func write(_ text: String, to file: URL) throws {
        try FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try text.write(to: file, atomically: true, encoding: .utf8)
    }

    /*
     Committed means git would list it. The pins in the bundle beside a spec are read only from a repository that
     does not ignore them, never from a folder with no repository, where that bundle is the last generation's output.
     A committed bundle's pins an ignore rule covers are a developer's own, and are not read either.
     */
    @Test func readsOnlyTheResolutionARepositoryCommits() throws {
        try Self.withFolder { folder in
            let generated = Self.phase([], in: folder, resolutionAllowed: false)
            let besideSpec = folder.appendingPathComponent(
                "Fixture.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
            )
            try Self.write("{}", to: besideSpec)
            #expect(generated.committedResolved == nil, "no repository")

            try FileManager.default.createDirectory(
                at: folder.appendingPathComponent(".git"),
                withIntermediateDirectories: true,
            )
            try Self.write("*.xcodeproj/\n", to: folder.appendingPathComponent(".gitignore"))
            #expect(generated.committedResolved == nil, "ignored")

            try Self.write(
                "*.xcodeproj/*\n!*.xcodeproj/project.xcworkspace/\n",
                to: folder.appendingPathComponent(".gitignore"),
            )
            #expect(generated.committedResolved?.path == besideSpec.path)

            let committed = Self.phase([], in: folder, resolutionAllowed: false, kind: .xcodeProject)
            #expect(committed.committedResolved?.path == besideSpec.path)
            try Self.write("Package.resolved\n", to: folder.appendingPathComponent(".gitignore"))
            #expect(committed.committedResolved == nil, "a developer's own pins")
        }
    }

    /* The workspace names the committed bundle by its absolute path, escaped for XML. */
    @Test func writesAWorkspaceNamingTheCommittedBundle() throws {
        try Self.withFolder { folder in
            let odd = folder.appendingPathComponent("R&D \"one\"", isDirectory: true)
            try FileManager.default.createDirectory(at: odd, withIntermediateDirectories: true)
            let phase = Self.phase([], in: odd, resolutionAllowed: false, kind: .xcodeProject)
            try phase.writeWorkspace()
            let contents = try String(
                contentsOf: phase.workspace.appendingPathComponent("contents.xcworkspacedata"),
                encoding: .utf8,
            )
            #expect(
                contents.contains("location = \"absolute:\(folder.path)/R&amp;D &quot;one&quot;/Fixture.xcodeproj\"")
            )
        }
    }

    /* The build directory of each built target, by its SDK: `Debug-iphoneos`, and plain `Debug` for macOS. */
    @Test func readsEachTargetsRecordsFromItsSdksConfiguration() throws {
        try Self.withFolder { folder in
            let phase = Self.phase(
                [
                    Self.target("App", "application", sdkRoot: "iphoneos"),
                    Self.target("Tool", "tool", sdkRoot: "macosx"),
                ],
                in: folder,
            )
            let intermediates = "scratch/xcode-build/Build/Intermediates.noindex/Fixture.build"
            for directory in ["Debug-iphoneos/App.build", "Debug/Tool.build", "Debug-iphoneos/Tool.build"] {
                try FileManager.default.createDirectory(
                    at: folder.appendingPathComponent("\(intermediates)/\(directory)"),
                    withIntermediateDirectories: true,
                )
            }
            let directories = phase.targetDirectories(builds: phase.buildPlan.builds)
            #expect(
                directories.map { "\($0.0):\($0.1.path.dropFirst(folder.path.count + intermediates.count + 2))" } == [
                    "App:Debug-iphoneos/App.build", "Tool:Debug/Tool.build",
                ]
            )
        }
    }

    static func result(_ output: String, exitCode: Int32 = 65) -> ProcessRunner.Result {
        ProcessRunner.Result(exitCode: exitCode, standardOutput: Data(output.utf8), standardError: "")
    }

    /* The failed build commands xcodebuild lists, or its own error with the lines under it, never only "it failed". */
    @Test func namesTheStepThatFailed() throws {
        try Self.withFolder { folder in
            let phase = Self.phase([], in: folder)
            let linked = Self.result(
                """
                ** BUILD FAILED **

                The following build commands failed:
                	Ld /dd/Build/Products/Debug-iphoneos/App.app/App normal (in target 'App' from project 'Fixture')
                	Building project Fixture with scheme App
                (2 failures)

                """
            )
            #expect(
                phase.failedStep(of: linked)
                    == "Ld /dd/Build/Products/Debug-iphoneos/App.app/App normal (in target 'App' from project 'Fixture') / Building project Fixture with scheme App"
            )
            let scheme = Self.result(
                "xcodebuild: error: The project named \"Fixture\" does not contain a scheme named \"App\".\n"
            )
            #expect(
                phase.failedStep(of: scheme)
                    == "xcodebuild: error: The project named \"Fixture\" does not contain a scheme named \"App\"."
            )
        }
    }

    /*
     A resolve that fails is said in xcodebuild's words. Under `--no-fix`, held to a resolution the repository
     commits, the words name that file, since the build was held to it.
     */
    @Test func aResolutionThatFailsIsNamed() throws {
        try Self.withFolder { folder in
            let stale = Self.result(
                """
                xcodebuild: error: Could not resolve package dependencies:
                  an out-of-date resolved file was detected at /scratch/Package.resolved, which is not allowed when automatic dependency resolution is disabled

                """,
                exitCode: 74,
            )
            let checking = Self.phase([], in: folder, resolutionAllowed: false, kind: .xcodeProject)
            let said =
                "xcodebuild: error: Could not resolve package dependencies: an out-of-date resolved file was detected at /scratch/Package.resolved, which is not allowed when automatic dependency resolution is disabled"
            #expect(checking.failedStep(of: stale) == said)
            try Self.write("{}", to: checking.resolvedInBundle)
            #expect(
                checking.failedStep(of: stale)
                    == "\(said) (under --no-fix the build is held to the resolution the repository commits, \(checking.resolvedInBundle.path), and a check never moves it)"
            )
            #expect(Self.phase([], in: folder, kind: .xcodeProject).failedStep(of: stale) == said)
        }
    }

    /*
     A generated bundle is made afresh each run, so the pins a build wrote are kept in the scratch and put back into
     the next bundle. A bundle that has pins of its own keeps them, unless the repository commits pins, which replace
     whatever the bundle holds.
     */
    @Test func keepsAGeneratedBundlesPinsAcrossGeneration() throws {
        try Self.withFolder { folder in
            let phase = Self.phase([], in: folder)
            try FileManager.default.createDirectory(at: phase.scratchPath, withIntermediateDirectories: true)
            try FileManager.default.createDirectory(
                at: phase.resolvedInBundle.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try "pins one".write(to: phase.resolvedInBundle, atomically: true, encoding: .utf8)
            phase.keepResolved()

            try FileManager.default.removeItem(at: phase.project.bundle)
            phase.restoreResolved()
            #expect(try String(contentsOf: phase.resolvedInBundle, encoding: .utf8) == "pins one")

            try "pins two".write(to: phase.resolvedInBundle, atomically: true, encoding: .utf8)
            phase.restoreResolved()
            #expect(try String(contentsOf: phase.resolvedInBundle, encoding: .utf8) == "pins two")

            try FileManager.default.createDirectory(
                at: folder.appendingPathComponent(".git"),
                withIntermediateDirectories: true,
            )
            try Self.write(
                "committed pins",
                to: folder.appendingPathComponent(
                    "Fixture.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved"
                ),
            )
            phase.restoreResolved()
            #expect(try String(contentsOf: phase.resolvedInBundle, encoding: .utf8) == "committed pins")
        }
    }

    /* The local packages a build's package state names, which reach beyond what the project file links; remote ones are not. */
    @Test func readsTheLocalPackagesABuildResolved() throws {
        try Self.withFolder { folder in
            let state = """
                {"object": {"dependencies": [
                  {"packageRef": {"identity": "numbertext", "kind": "fileSystem", "location": "/work/Libraries/NumberText"}},
                  {"packageRef": {"identity": "mediapipe", "kind": "remoteSourceControl", "location": "https://github.com/vcamapp/mediapipe"}}
                ]}, "version": 7}
                """
            try state.write(
                to: folder.appendingPathComponent("workspace-state.json"),
                atomically: true,
                encoding: .utf8,
            )
            #expect(XcodeTypesPhase.localPackages(stateIn: folder).map(\.path) == ["/work/Libraries/NumberText"])
            #expect(XcodeTypesPhase.localPackages(stateIn: folder.appendingPathComponent("absent")).isEmpty)
        }
    }
}
