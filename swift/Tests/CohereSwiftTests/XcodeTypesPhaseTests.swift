import Foundation
import Testing

@testable import CohereSwift

/*
 The parts of the Xcode types phase that need no xcodebuild: which targets it builds and how, what it says when a
 build fails, the package pins it keeps for a generated bundle, and the local packages it reads from a build's
 package state. The builds themselves are `XcodeProjectPipelineTests`'.
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

    /* A phase over these targets, each owning one file, with its bundle and scratch under `folder`. */
    static func phase(
        _ targets: [XcodeProjectFile.Target],
        in folder: URL,
        resolutionAllowed: Bool = true,
    ) -> XcodeTypesPhase {
        let bundle = folder.appendingPathComponent("Fixture.xcodeproj", isDirectory: true)
        let projectFile = XcodeProjectFile(sourceRoot: folder, targets: targets, localPackages: [])
        let model = PackageModel(name: "Fixture", root: folder, toolsVersion: "", targets: [])
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

    /* Every write in the scratch, nothing signed, run scripts held to their declared files, and no resolution under `--no-fix`. */
    @Test func buildsIntoTheScratchWithoutSigningOrResolving() throws {
        try Self.withFolder { folder in
            let phase = Self.phase([Self.target("App", "application", sdkRoot: "iphoneos")], in: folder)
            let build = try #require(phase.buildPlan.builds.first)
            let arguments = phase.arguments(for: build)
            let scratch = folder.appendingPathComponent("scratch").path
            #expect(
                arguments == [
                    "-project", folder.appendingPathComponent("Fixture.xcodeproj").path, "-scheme", "App",
                    "-destination", "generic/platform=iOS", "-derivedDataPath", scratch + "/xcode-build",
                    "-clonedSourcePackagesDirPath", scratch + "/xcode-packages", "-configuration", "Debug",
                    "CODE_SIGNING_ALLOWED=NO", "ENABLE_USER_SCRIPT_SANDBOXING=YES", "build",
                ]
            )
            let checking = Self.phase(
                [Self.target("App", "application", sdkRoot: "iphoneos")],
                in: folder,
                resolutionAllowed: false,
            )
            #expect(checking.arguments(for: build).contains("-disableAutomaticPackageResolution"))
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

    /* Under `--no-fix` a resolution the scratch cannot answer is named as the one a plain run repairs. */
    @Test func aResolutionNoFixRefusesIsNamed() throws {
        try Self.withFolder { folder in
            let unresolved = Self.result(
                """
                xcodebuild: error: Could not resolve package dependencies:
                  a resolved file is required when automatic dependency resolution is disabled

                """,
                exitCode: 74,
            )
            let checking = Self.phase([], in: folder, resolutionAllowed: false)
            #expect(
                checking.failedStep(of: unresolved)
                    == "xcodebuild: error: Could not resolve package dependencies: a resolved file is required when automatic dependency resolution is disabled (under --no-fix packages are never resolved, and this scratch holds no resolution to build from; a run without --no-fix resolves them once)"
            )
            #expect(!Self.phase([], in: folder).failedStep(of: unresolved).contains("--no-fix"))
        }
    }

    /*
     A generated bundle is made afresh each run, so the pins a build wrote are kept in the scratch and put back into
     the next bundle. A bundle that has pins of its own keeps them.
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
