import Foundation
import Testing

@testable import CohereSwift

/*
 The compile-command reader against task stores written here in MessagePack's own encoding, each shape the
 real store was measured to use (fixarray and array16 headers, fixstr and str8 strings), plus the decoys a
 byte scan can trip on. The real store's agreement with the build is the oracle's job (`TypesOracle`); this
 is the reader's arithmetic.
 */
struct CompileCommandsTests {
    static func string(_ value: String) -> [UInt8] {
        let bytes = Array(value.utf8)
        switch bytes.count {
            case 0..<32: return [UInt8(0xA0 | bytes.count)] + bytes
            case 32..<256: return [0xD9, UInt8(bytes.count)] + bytes
            default: return [0xDA, UInt8(bytes.count >> 8), UInt8(bytes.count & 0xFF)] + bytes
        }
    }

    static func array(_ values: [String]) -> [UInt8] {
        let header: [UInt8] =
            values.count < 16
            ? [UInt8(0x90 | values.count)] : [0xDC, UInt8(values.count >> 8), UInt8(values.count & 0xFF)]
        return header + values.flatMap(string)
    }

    static let swiftc = "/Toolchain/usr/bin/swiftc"

    @Test func findsEveryDriverArrayWhateverItsHeader() throws {
        let short = ["builtin-SwiftDriver", "--", Self.swiftc, "-module-name", "Small"]
        let long = ["builtin-SwiftDriver", "--", Self.swiftc, "-module-name", "Large"] + (0..<20).map { "-DFLAG\($0)" }
        let store = [0x82, 0xC3] + Self.array(short) + [0xC0] + Self.array(long)
        let commands = try CompileCommands.driverCommands(in: store)
        #expect(commands.count == 2)
        #expect(commands.first == ["-module-name", "Small"])
        #expect(commands.last?.count == 22)
    }

    /* The words alone, or at the head of an array that is not a driver command, are not a command. */
    @Test func skipsTheMarkerOutsideACommandArray() throws {
        let bare = Self.string("builtin-SwiftDriver") + Self.string("--")
        let wrongShape = Self.array(["builtin-SwiftDriver", "not the separator", Self.swiftc, "-module-name", "Decoy"])
        let real = Self.array(["builtin-SwiftDriver", "--", Self.swiftc, "-module-name", "Real"])
        let commands = try CompileCommands.driverCommands(in: [0xC0] + bare + wrongShape + real)
        #expect(commands == [["-module-name", "Real"]])
    }

    @Test func dropsTheDriversFlagsAndReadsTheFileList() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-commands-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let source = directory.appendingPathComponent("Shape.swift").resolvingSymlinksInPath()
        try "".write(to: source, atomically: true, encoding: .utf8)
        let list = directory.appendingPathComponent("Subject.SwiftFileList")
        try "\(source.path)\n".write(to: list, atomically: true, encoding: .utf8)

        let command = try CompileCommands.command(from: [
            "-module-name", "Subject", "@\(list.path)", "-DDEBUG",
            "-module-cache-path", "/implicit", "-c", "-j16", "-incremental", "-enable-batch-mode",
            "-Xcc", "-ivfsstatcache", "-Xcc", "/stat.cache",
            "-output-file-map", "/map.json", "-explicit-module-build", "-module-cache-path", "/explicit",
            "-emit-module", "-emit-module-path", "/Subject.swiftmodule", "-index-store-path", "/index",
            "-Xcc", "-DKEPT=1", "-enable-upcoming-feature", "ExistentialAny",
        ])
        #expect(command.moduleName == "Subject")
        #expect(command.files == [source.path])
        #expect(
            command.arguments == [
                "-module-name", "Subject", source.path, "-DDEBUG", "-module-cache-path", "/implicit",
                "-Xcc", "-DKEPT=1", "-enable-upcoming-feature", "ExistentialAny",
            ]
        )
        #expect(!command.testable)
    }

    @Test func aCommandNamingNoModuleIsAFailure() {
        #expect(throws: CompileCommands.ReadFailure.self) {
            try CompileCommands.command(from: ["-DDEBUG"])
        }
    }

    /* An executable compiles twice; the testable twin must not answer for the file the product builds. */
    @Test func aFileTakesTheNormalCompileOverItsTestableTwin() {
        let testable = CompileCommands.Command(
            moduleName: "App",
            arguments: ["testable"],
            files: ["/App/main.swift"],
            testable: true,
        )
        let normal = CompileCommands.Command(
            moduleName: "App",
            arguments: ["normal"],
            files: ["/App/main.swift"],
            testable: false,
        )
        let commands = CompileCommands(commands: [testable, normal])
        #expect(commands.command(for: URL(fileURLWithPath: "/App/main.swift"))?.arguments == ["normal"])
        #expect(commands.command(for: URL(fileURLWithPath: "/App/Other.swift")) == nil)
    }

    /* An Xcode target's module is named for its product (CapturePhone's is `Capture`), so the modules our files are in are the ones their compiles name. */
    @Test func theModulesOfFilesAreTheOnesTheirCompilesName() {
        let commands = CompileCommands(commands: [
            CompileCommands.Command(
                moduleName: "Capture",
                arguments: [],
                files: ["/App/Phone.swift"],
                testable: false,
            ),
            CompileCommands.Command(
                moduleName: "CaptureWire",
                arguments: [],
                files: ["/Wire/Wire.swift"],
                testable: false,
            ),
        ])
        let files = [URL(fileURLWithPath: "/App/Phone.swift"), URL(fileURLWithPath: "/App/Uncompiled.swift")]
        #expect(commands.modules(compiling: files) == ["Capture"])
    }

    @Test func aScratchWithNoBuildDescriptionIsAFailure() {
        let empty = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-no-build-\(UUID().uuidString)",
            isDirectory: true,
        )
        #expect(throws: CompileCommands.ReadFailure.self) {
            try CompileCommands.read(layout: .swiftPackage(scratchPath: empty))
        }
    }

    /* A build description `name` holding `store`, written where the layout's build system writes one. */
    static func writeBuildDescription(_ store: [UInt8], named name: String, in layout: BuildLayout) throws {
        let buildData = CompileCommands.buildData(in: layout)
        try FileManager.default.createDirectory(
            at: buildData.appendingPathComponent("\(name).xcbuilddata"),
            withIntermediateDirectories: true,
        )
        try "older\n\(name)\n".write(
            to: buildData.appendingPathComponent("prior-build-descriptions.txt"),
            atomically: true,
            encoding: .utf8,
        )
        try Data(store).write(to: buildData.appendingPathComponent("\(name).xcbuilddata/task-store.msgpack"))
    }

    /*
     The same store read from both roots it was measured at: SwiftPM's `<scratch>/out/`, and an xcodebuild's
     `<derivedData>/Build/`, whose index store is `Index.noindex/DataStore` rather than SwiftPM's `out`.
     */
    @Test func bothLayoutsResolveTheirBuildDescriptionAndIndexStore() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-layouts-\(UUID().uuidString)",
            isDirectory: true,
        ).resolvingSymlinksInPath()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let source = directory.appendingPathComponent("Shape.swift")
        try "".write(to: source, atomically: true, encoding: .utf8)
        let list = directory.appendingPathComponent("Shape.SwiftFileList")
        try "\(source.path)\n".write(to: list, atomically: true, encoding: .utf8)
        let store = Self.array(["builtin-SwiftDriver", "--", Self.swiftc, "-module-name", "Shape", "@\(list.path)"])

        let scratch = directory.appendingPathComponent("scratch", isDirectory: true)
        let derivedData = scratch.appendingPathComponent("xcode-build", isDirectory: true)
        try Self.writeBuildDescription(store, named: "package", in: .swiftPackage(scratchPath: scratch))
        try Self.writeBuildDescription(store, named: "project", in: .xcode(derivedData: derivedData))

        #expect(
            FileManager.default.fileExists(
                atPath: scratch.appendingPathComponent("out/Intermediates.noindex/XCBuildData/package.xcbuilddata").path
            )
        )
        #expect(
            FileManager.default.fileExists(
                atPath: derivedData.appendingPathComponent(
                    "Build/Intermediates.noindex/XCBuildData/project.xcbuilddata"
                ).path
            )
        )
        for layout in [BuildLayout.swiftPackage(scratchPath: scratch), .xcode(derivedData: derivedData)] {
            let commands = try CompileCommands.read(layout: layout)
            #expect(commands.command(for: source)?.moduleName == "Shape")
        }
        #expect(
            IndexStore.storePath(layout: .swiftPackage(scratchPath: scratch)).path
                == scratch.appendingPathComponent("out").path
        )
        #expect(
            IndexStore.storePath(layout: .xcode(derivedData: derivedData)).path
                == derivedData.appendingPathComponent("Index.noindex/DataStore").path
        )
    }

    /*
     A compile directory as an explicit-module build leaves it: an SDK whose settings name its version, a module the
     map names, and the map beside the output file map. `missing` names a module that is gone instead.
     */
    static func explicitBuild(missing: Bool = false) throws -> (driver: [String], objects: URL, sdk: URL) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-explicit-\(UUID().uuidString)",
            isDirectory: true,
        )
        let objects = directory.appendingPathComponent("Objects-normal/arm64", isDirectory: true)
        let sdk = directory.appendingPathComponent("MacOSX27.2.sdk", isDirectory: true)
        try FileManager.default.createDirectory(at: objects, withIntermediateDirectories: true)
        try FileManager.default.createDirectory(at: sdk, withIntermediateDirectories: true)
        try #"{"Version": "27.2", "DefaultDeploymentTarget": "27.2"}"#.write(
            to: sdk.appendingPathComponent("SDKSettings.json"),
            atomically: true,
            encoding: .utf8,
        )
        let module = directory.appendingPathComponent("Swift.swiftmodule")
        try "".write(to: module, atomically: true, encoding: .utf8)
        let named = missing ? directory.appendingPathComponent("Gone.pcm").path : module.path
        try #"[{"moduleName": "Swift", "modulePath": "\#(named)", "isFramework": false}]"#.write(
            to: objects.appendingPathComponent("Subject-dependencies-1.json"),
            atomically: true,
            encoding: .utf8,
        )
        let driver = [
            "-module-name", "Subject", "-sdk", sdk.path, "-target", "arm64-apple-macos15.0", "-explicit-module-build",
            "-output-file-map", objects.appendingPathComponent("Subject-OutputFileMap.json").path,
        ]
        return (driver, objects, sdk)
    }

    static func explicitArguments(map: URL, clangTarget: String) -> [String] {
        [
            "-Xfrontend", "-disable-implicit-swift-modules", "-Xfrontend", "-explicit-swift-module-map-file",
            "-Xfrontend", map.path, "-Xcc", "-fno-implicit-modules", "-Xcc", "-fno-implicit-module-maps",
            "-Xfrontend", "-clang-target", "-Xfrontend", clangTarget,
        ]
    }

    /* The build's own modules, handed over: its map, implicit modules off, and the Clang target its PCMs were compiled for. */
    @Test func anExplicitModuleBuildIsGivenTheModulesItCompiled() throws {
        let build = try Self.explicitBuild()
        let command = try CompileCommands.command(from: build.driver)
        #expect(
            command.arguments
                == ["-module-name", "Subject", "-sdk", build.sdk.path, "-target", "arm64-apple-macos15.0"]
                + Self.explicitArguments(
                    map: build.objects.appendingPathComponent("Subject-dependencies-1.json"),
                    clangTarget: "arm64-apple-macos27.2",
                )
        )
    }

    /* A compile the build did not plan explicitly, or one whose map names a module that is gone, keeps building its modules implicitly. */
    @Test func aMapThatCannotBeTrustedKeepsImplicitModules() throws {
        let implicit = try Self.explicitBuild()
        let notExplicit = try CompileCommands.command(from: implicit.driver.filter { $0 != "-explicit-module-build" })
        #expect(!notExplicit.arguments.contains("-explicit-swift-module-map-file"))
        let gone = try Self.explicitBuild(missing: true)
        #expect(!(try CompileCommands.command(from: gone.driver).arguments.contains("-explicit-swift-module-map-file")))
    }

    /* The numbered maps are not in build order, so the one the last build wrote, the newest, is the one taken. */
    @Test func theNewestMapIsTheOneTaken() throws {
        let build = try Self.explicitBuild()
        let older = build.objects.appendingPathComponent("Subject-dependencies-1.json")
        let newer = build.objects.appendingPathComponent("Subject-dependencies-2.json")
        try FileManager.default.copyItem(at: older, to: newer)
        try FileManager.default.setAttributes(
            [.modificationDate: Date(timeIntervalSinceNow: -3600)],
            ofItemAtPath: newer.path,
        )
        let command = try CompileCommands.command(from: build.driver)
        #expect(command.arguments.contains(older.path))
        #expect(!command.arguments.contains(newer.path))
    }

    /* The SDK's version replaces macOS's deployment target, `DefaultDeploymentTarget` first; any other platform is left implicit. */
    @Test func onlyAMacOSTargetIsAlignedToItsSDK() throws {
        let build = try Self.explicitBuild()
        func aligned(_ target: String) -> String? {
            CompileCommands.sdkAlignedClangTarget(arguments: ["-target", target, "-sdk", build.sdk.path])
        }
        #expect(aligned("arm64-apple-macos15.0") == "arm64-apple-macos27.2")
        #expect(aligned("x86_64-apple-macosx14.0") == "x86_64-apple-macosx27.2")
        #expect(aligned("arm64-apple-ios17.0") == nil)
        #expect(aligned("arm64-apple-ios17.0-macabi") == nil)
        #expect(CompileCommands.sdkAlignedClangTarget(arguments: ["-target", "arm64-apple-macos15.0"]) == nil)
        try #"{"Version": "27.1"}"#.write(
            to: build.sdk.appendingPathComponent("SDKSettings.json"),
            atomically: true,
            encoding: .utf8,
        )
        #expect(aligned("arm64-apple-macos15.0") == "arm64-apple-macos27.1")
    }

    /* A task store with no Swift compile in it means the format moved, and must say so rather than read as no files. */
    @Test func aStoreWithNoCompileIsAFailure() throws {
        let scratch = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-empty-store-\(UUID().uuidString)",
            isDirectory: true,
        )
        try Self.writeBuildDescription(
            Self.array(["something", "else"]),
            named: "abc",
            in: .swiftPackage(scratchPath: scratch),
        )
        #expect(throws: CompileCommands.ReadFailure.self) {
            try CompileCommands.read(layout: .swiftPackage(scratchPath: scratch))
        }
    }
}
