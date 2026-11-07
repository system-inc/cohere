import Foundation
import Testing

@testable import CohereSwift

/*
 A SwiftPM package whose platforms leave out macOS is type-checked for a platform it names, never against the macOS
 host (#ee9ct1r). The fixture is the shape the false error was found in: a package naming only iOS whose code uses
 `Duration`, which macOS has only from 13.0 on, while `swift build` builds for the host at its default target and
 said "'Duration' is only available in macOS 13.0 or newer" of a package that claims no macOS at all.

 These need Xcode with the iOS SDK. Without it they fail and say so, since a skipped test reads like a passing one.
 Serialized, because each runs a check, and several at once only slow each other.
 */
@Suite(.serialized)
struct PlatformPackagePipelineTests {
    /* A manifest naming `platforms`, its target with every toolchain setting the rules require on, so it is clean of them. */
    static func manifest(name: String = "Control", platforms: String, tests: Bool = false) -> String {
        let testTarget = tests ? ",\n        .testTarget(name: \"ControlTests\", dependencies: [\"Control\"])" : ""
        return """
            // swift-tools-version:6.2
            import PackageDescription

            let package = Package(
                name: "\(name)",
                platforms: [\(platforms)],
                targets: [
                    .target(
                        name: "Control",
                        swiftSettings: [.enableUpcomingFeature("ExistentialAny"), .enableUpcomingFeature("MemberImportVisibility"), .strictMemorySafety()]
                    )\(testTarget)
                ]
            )

            """
    }

    /* Line 3 takes a `Duration`, which macOS has only from 13.0 on and iOS from 16.0 on. */
    static let durationSource = """
        enum Pause {
            /* Sleeps for `duration`, returning early if the task is cancelled. */
            static func sleep(for duration: Duration) async {
                do {
                    try await Task.sleep(for: duration)
                }
                catch {
                    /* Cancelled, the only error Task.sleep throws, and the pause is simply over. */
                }
            }
        }

        """

    /* Line 6 holds a type error, in a test file, so it is caught only if the build compiled the tests too. */
    static let typeErrorTestSource = """
        import Testing

        @testable import Control

        @Test func pauses() async {
            let wrong: Int = "not an int"
            await Pause.sleep(for: .milliseconds(wrong))
        }

        """

    /* A package in a fresh temporary folder, its files by path. */
    static func fixture(files: [String: String]) throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-platform-\(UUID().uuidString)",
            isDirectory: true,
        )
        for (path, text) in files {
            let file = root.appendingPathComponent(path)
            try FileManager.default.createDirectory(
                at: file.deletingLastPathComponent(),
                withIntermediateDirectories: true,
            )
            try text.write(to: file, atomically: true, encoding: .utf8)
        }
        return root
    }

    static func requireIosSdk() throws {
        let result = try ProcessRunner().run(
            "xcrun",
            ["--sdk", "iphoneos", "--show-sdk-path"],
            in: FileManager.default.temporaryDirectory,
        )
        try #require(
            result.succeeded,
            "Xcode's iOS SDK is not installed, so an iOS package cannot be built and these tests check nothing",
        )
    }

    /* The platforms SwiftPM's `describe` names, in the manifest's order, and every product's name. */
    @Test func thePackageModelReadsItsPlatformsAndProducts() throws {
        let describe = Data(
            #"{"name":"Example","platforms":[{"name":"ios","version":"18.0"},{"name":"macos","version":"15.0"}],"products":[{"name":"Kit","targets":["A","B"],"type":{"library":["automatic"]}}],"targets":[{"name":"A","type":"library","path":"Sources/A","sources":["A.swift"]}]}"#
                .utf8
        )
        let dump = Data(#"{"toolsVersion":{"_version":"6.2.0"},"swiftLanguageVersions":null,"targets":[]}"#.utf8)
        let model = try PackageModel(
            root: URL(fileURLWithPath: "/package"),
            describeJson: describe,
            dumpPackageJson: dump,
        )
        #expect(model.platforms == ["ios", "macos"])

        let silent = Data(#"{"name":"Example","targets":[]}"#.utf8)
        #expect(
            try PackageModel(root: URL(fileURLWithPath: "/package"), describeJson: silent, dumpPackageJson: dump)
                .platforms.isEmpty
        )
    }

    /* Only a package that names platforms and leaves macOS out is taken off the host, and it is checked for the first it names that this engine checks for. */
    @Test func onlyAPackageLeavingOutMacOSLeavesTheHost() {
        func model(_ platforms: [String]) -> PackageModel {
            var model = PackageModel(
                name: "Kit",
                root: URL(fileURLWithPath: "/package"),
                toolsVersion: "6.2",
                targets: [],
            )
            model.platforms = platforms
            return model
        }
        func phase(_ package: PackageModel) -> PlatformPackageTypesPhase {
            PlatformPackageTypesPhase(
                package: package,
                files: [],
                scratchPath: URL(fileURLWithPath: "/scratch"),
                resolutionAllowed: false,
                toolchain: "unknown",
                runner: ProcessRunner(),
            )
        }
        #expect(
            !PlatformPackageTypesPhase.leavesOutHost(model([])),
            "a package naming no platform supports macOS at its default",
        )
        #expect(!PlatformPackageTypesPhase.leavesOutHost(model(["macos", "ios"])))
        #expect(!PlatformPackageTypesPhase.leavesOutHost(model(["ios", "macos"])))
        #expect(PlatformPackageTypesPhase.leavesOutHost(model(["ios"])))
        #expect(phase(model(["ios"])).destination == .init(sdkRoot: "iphoneos", platform: "iOS"))
        #expect(
            phase(model(["maccatalyst", "visionos", "ios"])).destination
                == .init(sdkRoot: "xros", platform: "visionOS")
        )
        #expect(phase(model(["maccatalyst"])).destination == nil)
        #expect(phase(model(["ios"])).destination?.triple == "arm64-apple-ios")
        #expect(phase(model(["visionos"])).destination?.triple == "arm64-apple-xros")
    }

    /* The fixture: an iOS-only package using `Duration` has no type error, is checked for iOS, and is complete and green. */
    @Test func anIosOnlyPackageUsingDurationIsCheckedForIos() async throws {
        try Self.requireIosSdk()
        let root = try Self.fixture(files: [
            "Package.swift": Self.manifest(platforms: ".iOS(.v18)"),
            "Sources/Control/Pause.swift": Self.durationSource,
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings.isEmpty, "\(run.of("finding"))")
        #expect(run.findings.isEmpty, "\(run.findings)")
        #expect(
            run.typesBuild.hasPrefix(
                "for iOS, the first platform the package names, since it names no macOS: checked without building"
            ),
            "\(run.typesBuild)",
        )
        #expect(run.filesWithoutRecord.isEmpty)
        #expect(run.phase("lint")?.outcome == "ran")
        #expect(run.complete == true, "\(run.records)")
        #expect(run.exitCode == 0)
    }

    /* The iOS check is a real one: a type error planted in a test file is the compiler's finding at its line, and lint is not reached. */
    @Test func aTypeErrorInAnIosOnlyPackageIsReported() async throws {
        try Self.requireIosSdk()
        let root = try Self.fixture(files: [
            "Package.swift": Self.manifest(platforms: ".iOS(.v18)", tests: true),
            "Sources/Control/Pause.swift": Self.durationSource,
            "Tests/ControlTests/ControlTests.swift": Self.typeErrorTestSource,
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings == ["ControlTests.swift:6:error"], "\(run.typesBuild)")
        #expect(run.of("finding").count == 1, "the failed check is the type error's, not a second finding")
        #expect(run.phase("lint")?.outcome == "notReached")
        #expect(run.exitCode == 1)
    }

    /*
     A package naming macOS beside iOS is checked for the host as before, and the host still judges it: at macOS 12
     the same `Duration` is a true error, since that package does claim macOS 12.
     */
    @Test func aPackageNamingMacOSAndIosIsBuiltForTheHost() async throws {
        let root = try Self.fixture(files: [
            "Package.swift": Self.manifest(platforms: ".macOS(.v12), .iOS(.v18)"),
            "Sources/Control/Pause.swift": Self.durationSource,
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.typesBuild.hasPrefix("checked without building"), "\(run.typesBuild)")
        #expect(run.compilerFindings.contains("Pause.swift:3:error"), "\(run.of("finding"))")
        let messages = run.of("finding").compactMap { $0["message"] as? String }
        #expect(messages.contains { $0.contains("is only available in macOS 13.0 or newer") }, "\(messages)")
    }

    /* A package naming no platform this engine checks for is a named gap: not checked, every file without a record, never green. */
    @Test func aPackageNamingNoBuildablePlatformIsANamedGap() async throws {
        let root = try Self.fixture(files: [
            "Package.swift": Self.manifest(platforms: ".macCatalyst(.v18)"),
            "Sources/Control/Pause.swift": Self.durationSource,
        ])
        defer { XcodeProjectPipelineTests.remove(root) }

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.compilerFindings.isEmpty, "the host build's availability errors are never the package's")
        #expect(run.phase("types")?.outcome == "ran")
        #expect(
            run.typesBuild.hasPrefix(
                "not checked: the package's platforms (maccatalyst) leave out macOS and name none this engine checks for"
            ),
            "\(run.typesBuild)",
        )
        #expect(run.filesWithoutRecord == ["Pause.swift"])
        #expect(run.complete == false)
    }

    /* Under `--no-fix` the package's folder is byte for byte what it was, and no DerivedData folder is made for it. */
    @Test func aCheckOfAnIosOnlyPackageWritesNothingIntoIt() async throws {
        try Self.requireIosSdk()
        let name = "Control\(UUID().uuidString.prefix(8))"
        let root = try Self.fixture(files: [
            "Package.swift": Self.manifest(name: name, platforms: ".iOS(.v18)", tests: true),
            "Sources/Control/Pause.swift": Self.durationSource,
            "Tests/ControlTests/ControlTests.swift": Self.typeErrorTestSource.replacingOccurrences(
                of: "let wrong: Int = \"not an int\"",
                with: "let wrong = 1",
            ),
        ])
        defer { XcodeProjectPipelineTests.remove(root) }
        let before = try XcodeProjectPipelineTests.snapshot(of: root)
        let derivedDataBefore = try XcodeProjectPipelineTests.derivedDataFolders(named: name)

        let run = try await XcodeProjectPipelineTests.run(root: root, arguments: ["--no-fix", "--no-cache"])

        #expect(run.typesBuild.hasPrefix("for iOS"), "\(run.typesBuild)")
        #expect(run.filesWithoutRecord.isEmpty, "the check ran, so the comparison below covers what it writes")
        let after = try XcodeProjectPipelineTests.snapshot(of: root)
        #expect(after == before, "written: \(Set(after.keys).symmetricDifference(before.keys).sorted())")
        #expect(
            try XcodeProjectPipelineTests.derivedDataFolders(named: name) == derivedDataBefore,
            "the check wrote DerivedData",
        )
    }
}
