import Foundation
import Testing

@testable import CohereSwift

/*
 A package shaped like the ones a check meets, built in a fresh temporary folder: a C target, a Swift target that
 imports it, a second that depends on the first, and a test target that reaches the second through `@testable
 import`. Shared by the tests of `BuildServerClient`, `FrontendCheck` and `CSyntaxCheck`, and by the proofs that the
 check builds nothing and reports what a build reports.
 */
enum CheckFixture {
    /* One planted diagnostic: the file it lives in, its text, and words the compiler's message holds. */
    struct Plant: Sendable {
        var path: String
        var text: String
        var message: String
    }

    static let manifest = """
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "Fixture",
            platforms: [.macOS(.v15), .iOS(.v18)],
            targets: [
                .target(name: "CCore"),
                .target(name: "Core", dependencies: ["CCore"]),
                .target(name: "Feature", dependencies: ["Core"]),
                .testTarget(name: "FeatureTests", dependencies: ["Feature"]),
            ]
        )

        """

    static let files: [String: String] = [
        "Package.swift": manifest,
        "Sources/CCore/include/CCore.h": "int ccore_answer(void);\n",
        "Sources/CCore/answer.c": """
        #include "CCore.h"

        int ccore_answer(void) {
            return 42;
        }

        """,
        "Sources/Core/Greeting.swift": """
        import CCore

        public struct Greeting {
            public var name: String

            public init(name: String) {
                self.name = name
            }

            public func text() -> String {
                "Hello, \\(name), \\(ccore_answer())"
            }
        }

        """,
        /* Line 5 calls `text()` inside a body: the reference the index must hold. */
        "Sources/Feature/Welcome.swift": """
        import Core

        func welcome(_ name: String) -> String {
            let greeting = Greeting(name: name)
            return greeting.text()
        }

        """,
        "Sources/Feature/Count.swift": """
        func count(_ values: [Int]) -> Int {
            values.reduce(0) { $0 + $1 }
        }

        """,
        "Tests/FeatureTests/WelcomeTests.swift": """
        import Testing

        @testable import Feature

        @Test func welcomes() {
            #expect(welcome("Ada").hasPrefix("Hello"))
        }

        """,
    ]

    /*
     Every kind of diagnostic a build reports, each in a file of its own in the test target, which nothing depends
     on. The first is the type checker's; the next six come only from the `SIL` passes plain `-typecheck` skips.
     */
    static let swiftPlants: [Plant] = [
        Plant(
            path: "Tests/FeatureTests/TypeError.swift",
            text: "func typeError() -> Int {\n    let wrong: Int = \"nope\"\n    return wrong\n}\n",
            message: "cannot convert value of type 'String' to specified type 'Int'",
        ),
        Plant(
            path: "Tests/FeatureTests/MissingReturn.swift",
            text: "func missingReturn(_ value: Int) -> Int {\n    if value > 0 {\n        return value\n    }\n}\n",
            message: "missing return in global function expected to return 'Int'",
        ),
        Plant(
            path: "Tests/FeatureTests/Overflow.swift",
            text: "func overflow() -> Int8 {\n    let maximum: Int8 = 127\n    return maximum + 1\n}\n",
            message: "results in an overflow",
        ),
        Plant(
            path: "Tests/FeatureTests/InfiniteRecursion.swift",
            text: "func forever(_ value: Int) -> Int {\n    forever(value)\n}\n",
            message: "function call causes an infinite recursion",
        ),
        Plant(
            path: "Tests/FeatureTests/UseBeforeInitialization.swift",
            text: "func uninitialized() -> Int {\n    let value: Int\n    return value\n}\n",
            message: "constant 'value' used before being initialized",
        ),
        Plant(
            path: "Tests/FeatureTests/Exclusivity.swift",
            text: "func exclusivity() {\n    var numbers = [1, 2]\n    swap(&numbers[0], &numbers[1])\n}\n",
            message: "overlapping accesses to 'numbers'",
        ),
        Plant(
            path: "Tests/FeatureTests/DataRace.swift",
            text: """
                final class Counter {
                    var value = 0
                }

                func race() async {
                    let counter = Counter()
                    Task {
                        counter.value += 1
                    }
                    counter.value += 1
                }

                """,
            message: "risks causing data races",
        ),
    ]

    /* A C error, in the C target the Swift targets import. */
    static let cPlant = Plant(
        path: "Sources/CCore/broken.c",
        text: "int ccore_broken(void) {\n    return undeclared_value;\n}\n",
        message: "use of undeclared identifier 'undeclared_value'",
    )

    /* The package in a fresh temporary folder, with these plants, its root resolved so paths compare as the compiler writes them. */
    static func package(plants: [Plant] = []) throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-check-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        let resolved = root.resolvingSymlinksInPath()
        for (path, text) in files {
            try write(text, to: path, in: resolved)
        }
        for plant in plants {
            try write(plant.text, to: plant.path, in: resolved)
        }
        return resolved
    }

    static func write(_ text: String, to path: String, in root: URL) throws {
        let file = root.appendingPathComponent(path)
        try FileManager.default.createDirectory(
            at: file.deletingLastPathComponent(),
            withIntermediateDirectories: true,
        )
        try text.write(to: file, atomically: true, encoding: .utf8)
    }

    /* A scratch beside nothing the package owns. */
    static func scratch() -> URL {
        FileManager.default.temporaryDirectory
            .appendingPathComponent("cohere-swift-check-scratch-\(UUID().uuidString)", isDirectory: true)
            .resolvingSymlinksInPath()
    }

    /* Everything one check of the package produced. */
    struct Checked {
        var description: BuildServerClient.Description
        var swift: FrontendCheck.Outcome
        var cFamily: CSyntaxCheck.Outcome

        var targetDirectories: [(String, URL)] { swift.targetDirectories + cFamily.targetDirectories }
    }

    /* The whole check as wave 2 will run it: the server describes and prepares, then the Swift and C checks run. */
    static func check(_ root: URL, scratch: URL, batchSize: Int = 25) throws -> Checked {
        let description = try BuildServerClient.describe(
            configuration: .init(packageRoot: root, scratchPath: scratch, resolutionAllowed: false)
        )
        let diagnosticsRoot = scratch.appendingPathComponent("check", isDirectory: true)
        let frontend = FrontendCheck(
            swiftc: try FrontendCheck.toolchainExecutable(named: "swiftc"),
            diagnosticsRoot: diagnosticsRoot,
            indexStorePath: description.indexStorePath,
            workingDirectory: root,
            batchSize: batchSize,
        )
        let swift = try frontend.check(FrontendCheck.modules(from: description.options))
        let cFamily = try CSyntaxCheck(
            clang: try FrontendCheck.toolchainExecutable(named: "clang"),
            diagnosticsRoot: diagnosticsRoot,
            workingDirectory: root,
        )
        .check(CSyntaxCheck.sources(of: description.targets, options: description.options))
        return Checked(description: description, swift: swift, cFamily: cFamily)
    }

    /* `swift build --build-tests` of the package into its own scratch, as the types phase builds today. */
    static func build(_ root: URL, scratch: URL) throws -> ProcessRunner.Result {
        try ProcessRunner().run(
            "swift",
            ["build", "--build-tests", "--scratch-path", scratch.path] + TypesPhase.swiftBuildArguments,
            in: root,
        )
    }

    /* A build's record directory for each target, named as `TypesPhase` names them. */
    static func buildDirectories(scratch: URL) throws -> [(String, URL)] {
        let debug = scratch.appendingPathComponent("out/Intermediates.noindex/Fixture.build/Debug", isDirectory: true)
        return try FileManager.default.contentsOfDirectory(at: debug, includingPropertiesForKeys: nil)
            .filter { $0.lastPathComponent.hasSuffix(".build") }
            .map { (TypesPhase.directoryStem($0.lastPathComponent), $0) }
    }

    /* Every Swift and C file the package owns, by the target folder it sits in. */
    static func ownedFiles(_ root: URL) -> [FileSet.OwnedFile] {
        var owned: [FileSet.OwnedFile] = []
        for (folder, kind) in [("Sources", "library"), ("Tests", "test")] {
            let base = root.appendingPathComponent(folder, isDirectory: true)
            guard let walker = FileManager.default.enumerator(at: base, includingPropertiesForKeys: nil) else {
                continue
            }
            for case let file as URL in walker where ["swift", "c"].contains(file.pathExtension) {
                guard let target = relative(file.path, to: base).split(separator: "/").first else { continue }
                owned.append(FileSet.OwnedFile(url: file, targetName: String(target), targetKind: kind))
            }
        }
        return owned
    }

    /*
     What the records under these directories say about each owned file, read through `CompilerRecords` as the types
     phase reads a build's: `line:column: severity: message`, by path relative to the root, every owned file listed.
     */
    static func diagnostics(
        _ root: URL,
        directories: [(String, URL)],
    ) throws -> (byFile: [String: [String]], withoutRecord: [String]) {
        let owned = ownedFiles(root)
        let reading = try CompilerRecords(files: owned, runner: ProcessRunner()).read(targetDirectories: directories)
        var byFile: [String: [String]] = [:]
        for file in owned {
            byFile[relative(file.url.path, to: root)] = []
        }
        for finding in reading.findings {
            byFile[relative(finding.file, to: root), default: []].append(
                "\(finding.line):\(finding.column): \(finding.severity): \(finding.message)"
            )
        }
        return (byFile.mapValues { $0.sorted() }, reading.filesWithoutRecord.map { relative($0, to: root) }.sorted())
    }

    /*
     A path relative to the root. The temporary folder is reached two ways, `/var/...` and its target `/private/var/...`:
     Foundation resolves to the first, the compilers write the second, and a folder walk returns either, so both sides
     are compared without `/private`.
     */
    static func relative(_ path: String, to root: URL) -> String {
        func withoutPrivate(_ path: String) -> String {
            path.hasPrefix("/private/") ? String(path.dropFirst("/private".count)) : path
        }
        let base = withoutPrivate(root.path) + "/"
        let file = withoutPrivate(path)
        return file.hasPrefix(base) ? String(file.dropFirst(base.count)) : path
    }

    /* What a walk of the scratch and the package found. */
    struct Walk {
        /* Files a compiler or linker wrote as code: objects, executables, libraries, archives, bitcode. */
        var compiled: [String]
        /* Clang module containers: an AST in a Mach-O object, with no code in it. */
        var moduleContainers: [String]
    }

    /*
     Every file under the roots, judged by its bytes, never its name. Mach-O of either byte order, thin or universal, an
     `ar` archive, and LLVM bitcode, bare or wrapped, are compiled. One exception, read from the load commands: a 64-bit
     object holding a `__clangast` section whose every `__text` section is empty is a Clang module container, an AST and
     its debug info. Measured 2026-11-06: the frontend writes 18 of them for the SDK's Clang modules (`Darwin`,
     `ObjectiveC`) when it rebuilds the SDK's Swift modules from their interfaces, as any build does; the fixture's own
     `CCore` module is a raw `CPCH`, which is no Mach-O at all.
     */
    static func walk(_ roots: [URL]) -> Walk {
        let magics: [[UInt8]] = [
            [0xFE, 0xED, 0xFA, 0xCE], [0xFE, 0xED, 0xFA, 0xCF], [0xCE, 0xFA, 0xED, 0xFE], [0xCF, 0xFA, 0xED, 0xFE],
            [0xCA, 0xFE, 0xBA, 0xBE], [0xBE, 0xBA, 0xFE, 0xCA], Array("!<arch>\n".utf8), [0x42, 0x43, 0xC0, 0xDE],
            [0xDE, 0xC0, 0x17, 0x0B],
        ]
        var walk = Walk(compiled: [], moduleContainers: [])
        for root in roots {
            guard
                let walker = FileManager.default.enumerator(at: root, includingPropertiesForKeys: [.isRegularFileKey])
            else { continue }
            for case let file as URL in walker {
                guard (try? file.resourceValues(forKeys: [.isRegularFileKey]).isRegularFile) == true,
                    let bytes = try? [UInt8](Data(contentsOf: file)), magics.contains(where: { bytes.starts(with: $0) })
                else { continue }
                if isModuleContainer(bytes) {
                    walk.moduleContainers.append(file.path)
                }
                else {
                    walk.compiled.append(file.path)
                }
            }
        }
        walk.compiled.sort()
        walk.moduleContainers.sort()
        return walk
    }

    /*
     Whether these bytes are a little-endian 64-bit Mach-O object (`MH_OBJECT`) whose segments hold a `__clangast`
     section and no `__text` section with any size: read from the header's command count and each `LC_SEGMENT_64`'s
     sections, 80 bytes each, the name in the first 16 and the size at 40.
     */
    static func isModuleContainer(_ bytes: [UInt8]) -> Bool {
        func word(_ offset: Int) -> Int? {
            guard offset >= 0, offset + 4 <= bytes.count else { return nil }
            return (0..<4).reduce(0) { $0 | Int(bytes[offset + $1]) << (8 * $1) }
        }
        func name(_ offset: Int) -> String {
            String(decoding: bytes[offset..<offset + 16].prefix { $0 != 0 }, as: UTF8.self)
        }
        guard bytes.starts(with: [0xCF, 0xFA, 0xED, 0xFE]), word(12) == 1, let commands = word(16) else { return false }
        var hasAst = false
        var offset = 32
        for _ in 0..<commands {
            guard let command = word(offset), let size = word(offset + 4), size > 0 else { return false }
            if command == 0x19, let sections = word(offset + 64) {
                for index in 0..<sections {
                    let section = offset + 72 + index * 80
                    guard section + 80 <= bytes.count, let low = word(section + 40), let high = word(section + 44)
                    else {
                        return false
                    }
                    switch name(section) {
                        case "__clangast": hasAst = true
                        case "__text" where low != 0 || high != 0: return false
                        default: break
                    }
                }
            }
            offset += size
        }
        return hasAst
    }

    /* The occurrences one file's newest own unit records in a store: `line:column roles kind symbol name`, sorted. */
    static func occurrences(of file: URL, storePath: URL) throws -> [String] {
        let store = try IndexStore(libraryPath: IndexStore.toolchainLibraryPath(), storePath: storePath)
        let path = file.resolvingSymlinksInPath().path
        let unit = try #require(
            store.units().filter { !$0.isSystem && $0.mainFile == path }.max { $0.written < $1.written },
            "the store at \(storePath.path) holds no unit for \(path)",
        )
        let record = try #require(unit.ownRecords.first, "the unit for \(path) names no record of it")
        let occurrences = try #require(store.occurrences(inRecord: record), "the record for \(path) cannot be read")
        return occurrences.map { "\($0.line):\($0.column) \($0.roles) \($0.kind) \($0.symbol) \($0.name)" }.sorted()
    }
}
