import Foundation
import Testing

@testable import CohereSwift

/*
 The build server client speaks to SwiftPM's real `swift package experimental-build-server`: it learns every target's
 arguments, prepares the dependency modules into the run's own scratch and nowhere else, and fails by step.
 Serialized, because each starts a server, and several at once only slow each other.
 */
@Suite(.serialized)
struct BuildServerClientTests {
    /* A message split anywhere across reads comes out whole, and two in one read come out one at a time. */
    @Test func theFramerFollowsMessagesAcrossReads() throws {
        let first = #"{"jsonrpc":"2.0","id":1,"result":{}}"#
        let second = #"{"jsonrpc":"2.0","method":"build/logMessage","params":{"message":"hi"}}"#
        let stream = Data(
            "Content-Length: \(first.utf8.count)\r\n\r\n\(first)Content-Length: \(second.utf8.count)\r\n\r\n\(second)"
                .utf8
        )
        var framer = BuildServerClient.MessageFramer()
        var bodies: [String] = []
        for start in stride(from: 0, to: stream.count, by: 7) {
            framer.append(stream[start..<min(start + 7, stream.count)])
            while let body = try framer.next() {
                bodies.append(String(decoding: body, as: UTF8.self))
            }
        }
        #expect(bodies == [first, second])

        var headless = BuildServerClient.MessageFramer()
        headless.append(Data("X-Other: 1\r\n\r\n{}".utf8))
        #expect(throws: BuildServerClient.MessageFramer.FramingError.self) { try headless.next() }
    }

    /*
     Prepare's log names each host tool it linked, timed from the first message that names it. A partial link of a
     library's objects (`Link ArgumentParser.o`) is part of a tool, not one, and a tool linked twice is counted once.
     The words are the ones measured on ahraos-presence, mlx-swift's `encuda`.
     */
    @Test func prepareNamesTheToolsItLinked() {
        let start = Date()
        let log: [(arrived: Date, message: String)] = [
            (start, "Write encuda-OutputFileMap.json (arm64)"),
            (start.addingTimeInterval(1), "Link ArgumentParser.o (arm64)"),
            (start.addingTimeInterval(2), "Compiling encuda-compile.swift, encuda.swift"),
            (start.addingTimeInterval(9.75), "Link encuda (arm64)"),
            (start.addingTimeInterval(10), "Link encuda (arm64)"),
        ]
        #expect(BuildServerClient.toolsBuilt(in: log) == [.init(name: "encuda", seconds: 9.75)])
        #expect(BuildServerClient.toolsBuilt(in: [(start, "Compiling Count.swift, Welcome.swift")]).isEmpty)
    }

    /* A location is inside the scratch whether or not it exists yet, and whichever spelling of a linked folder each uses. */
    @Test func aLocationIsJudgedInsideTheScratchThroughLinks() {
        let scratch = URL(fileURLWithPath: "/var/folders/scratch")
        #expect(BuildServerClient.isInside(URL(fileURLWithPath: "/var/folders/scratch/index-store"), scratch))
        #expect(BuildServerClient.isInside(URL(fileURLWithPath: "/private/var/folders/scratch/out"), scratch))
        #expect(!BuildServerClient.isInside(URL(fileURLWithPath: "/var/folders/elsewhere/out"), scratch))
        #expect(!BuildServerClient.isInside(URL(fileURLWithPath: "/var/folders/scratch"), scratch))
    }

    /*
     The whole session on the fixture: every buildable target with its files, one set of arguments per target and
     language, the dependency modules prepared under the run's scratch, and nothing written into the package.
     */
    @Test func aSessionDescribesAndPreparesIntoTheScratchOnly() throws {
        let root = try CheckFixture.package()
        let scratch = CheckFixture.scratch()
        let description = try BuildServerClient.describe(
            configuration: .init(packageRoot: root, scratchPath: scratch, resolutionAllowed: false)
        )

        #expect(description.outputPath.path == scratch.appendingPathComponent("out").path)
        #expect(BuildServerClient.isInside(description.indexStorePath, scratch))
        #expect(Set(description.targets.map(\.displayName)) == ["CCore", "Core", "Feature", "FeatureTests-product"])

        let swiftModules = FrontendCheck.modules(from: description.options).map(\.name).sorted()
        #expect(swiftModules == ["Core", "Feature", "FeatureTests"])
        let feature = try #require(FrontendCheck.modules(from: description.options).first { $0.name == "Feature" })
        #expect(
            feature.sources.map { URL(fileURLWithPath: $0).lastPathComponent }.sorted() == [
                "Count.swift", "Welcome.swift",
            ]
        )

        let cOptions = try #require(description.options.first { $0.language == "c" })
        #expect(cOptions.target.displayName == "CCore")
        #expect(cOptions.arguments.contains(cOptions.askedFile.path))

        let products = description.outputPath.appendingPathComponent("Products/Debug", isDirectory: true)
        for module in ["Core", "Feature"] {
            #expect(
                FileManager.default.fileExists(atPath: products.appendingPathComponent("\(module).swiftmodule").path),
                "prepare left no \(module).swiftmodule under \(products.path)",
            )
        }
        let packageEntries = try FileManager.default.contentsOfDirectory(atPath: root.path).sorted()
        #expect(packageEntries == ["Package.swift", "Sources", "Tests"])
    }

    /* A request the server answers with an error fails as its step, with the server's code and words. */
    @Test func aServerErrorNamesItsStep() throws {
        let root = try CheckFixture.package()
        let client = try BuildServerClient(
            configuration: .init(packageRoot: root, scratchPath: CheckFixture.scratch(), resolutionAllowed: false)
        )
        _ = try client.initialize()
        try client.waitForUpdates()
        let bogus = BuildServerClient.Target(
            identifier: "swift-build://configured-target?configuredTargetGUID=bogus&targetGUID=bogus",
            displayName: "Bogus",
            tags: [],
            dependencies: [],
        )
        let failure = try #require(throws: BuildServerClient.Failure.self) { try client.prepare([bogus]) }
        #expect(failure.step == .prepare)
        guard case .serverError(let code, let message) = failure.reason else {
            Issue.record("a server error came back as \(failure.reason)")
            return
        }
        #expect(code == -32001)
        #expect(message.contains("bogus"))
        #expect(failure.description.hasPrefix("the build server's buildTarget/prepare answered with error -32001"))
        try client.shutdown()
    }

    /* A server that says nothing in time fails as the step it was asked. */
    @Test func aTimeoutNamesItsStep() throws {
        let root = try CheckFixture.package()
        var configuration = BuildServerClient.Configuration(
            packageRoot: root,
            scratchPath: CheckFixture.scratch(),
            resolutionAllowed: false,
        )
        configuration.timeouts.request = 0.001
        let client = try BuildServerClient(configuration: configuration)
        let failure = try #require(throws: BuildServerClient.Failure.self) { try client.initialize() }
        #expect(failure.step == .initialize)
        #expect(failure.reason == .timedOut(seconds: 0.001))
    }

    /* A server that exits before answering fails as the step it was asked, with what it said on its way out. */
    @Test func aServerThatExitsNamesItsStep() throws {
        let missing = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-no-package-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: missing, withIntermediateDirectories: true)
        let client = try BuildServerClient(
            configuration: .init(packageRoot: missing, scratchPath: CheckFixture.scratch(), resolutionAllowed: false)
        )
        let failure = try #require(throws: BuildServerClient.Failure.self) { try client.initialize() }
        #expect(failure.step == .initialize)
        guard case .serverExited(let standardError) = failure.reason else {
            Issue.record("a server that exited came back as \(failure.reason)")
            return
        }
        #expect(standardError.contains("Package.swift"))
    }

    /* `--triple` and `--sdk` reach the server, so an iOS package's arguments target iOS against the iOS SDK. Needs Xcode's iOS SDK. */
    @Test func aTripleAndSdkReachTheArguments() throws {
        try PlatformPackagePipelineTests.requireIosSdk()
        let sdk = String(
            decoding: try ProcessRunner().run(
                "xcrun",
                ["--sdk", "iphoneos", "--show-sdk-path"],
                in: FileManager.default.temporaryDirectory,
            )
            .standardOutput,
            as: UTF8.self,
        )
        .trimmingCharacters(in: .whitespacesAndNewlines)
        let root = try CheckFixture.package()
        let client = try BuildServerClient(
            configuration: .init(
                packageRoot: root,
                scratchPath: CheckFixture.scratch(),
                triple: "arm64-apple-ios18.0",
                sdk: sdk,
                resolutionAllowed: false,
            )
        )
        _ = try client.initialize()
        try client.waitForUpdates()
        let options = try client.sourceKitOptions(for: client.sources(of: client.buildTargets()))
        try client.shutdown()
        let core = try #require(FrontendCheck.modules(from: options).first { $0.name == "Core" })
        let target = try #require(core.arguments.firstIndex(of: "-target"))
        #expect(core.arguments[target + 1] == "arm64-apple-ios18.0")
        let sdkFlag = try #require(core.arguments.firstIndex(of: "-sdk"))
        #expect(core.arguments[sdkFlag + 1].contains("iPhoneOS"))
    }
}
