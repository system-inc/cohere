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
