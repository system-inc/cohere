import Foundation
import Testing

@testable import CohereSwift

/*
 Where the typed rules' symbols come from, on real stores: two tiny packages, each built into its own scratch path
 the way a package and its local package are, so each has its own index store. Every layout's store is consulted,
 a file is described by its own record and not a neighbor's, and a file edited since its unit was written is not
 vouched for by that unit, though the same store answered for it a moment before.
 */
@Suite(.serialized)
struct SymbolProviderTests {
    static let leftSource = "public func leftReading() -> Int { 1 }\n"
    static let leftSpareSource = "public func leftSpare() -> Int { 2 }\n"
    static let rightSource = "public func rightReading() -> Int { 3 }\n"

    /* Both packages built, each into its own scratch, and their three files as a run hands them over. */
    static func builtPackages() async throws -> (layouts: [BuildLayout], files: [ParsedFile]) {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-symbol-provider-\(UUID().uuidString)",
            isDirectory: true,
        )
        var layouts: [BuildLayout] = []
        var owned: [FileSet.OwnedFile] = []
        for (name, sources) in [
            ("Left", ["Left.swift": leftSource, "LeftSpare.swift": leftSpareSource]),
            ("Right", ["Right.swift": rightSource]),
        ] {
            let root = directory.appendingPathComponent(name, isDirectory: true)
            let target = root.appendingPathComponent("Sources/\(name)", isDirectory: true)
            try FileManager.default.createDirectory(at: target, withIntermediateDirectories: true)
            let manifest =
                "// swift-tools-version:6.0\nimport PackageDescription\nlet package = Package(name: \"\(name)\", targets: [.target(name: \"\(name)\")])\n"
            try manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
            for (file, text) in sources.sorted(by: { $0.key < $1.key }) {
                let url = target.appendingPathComponent(file)
                try text.write(to: url, atomically: true, encoding: .utf8)
                owned.append(FileSet.OwnedFile(url: url, targetName: name, targetKind: "regular", packageRoot: root))
            }
            let scratch = root.appendingPathComponent(".cache/cohere/swift", isDirectory: true)
            let built = try ProcessRunner().run(
                "swift",
                ["build", "--build-system", "swiftbuild", "--scratch-path", scratch.path],
                in: root,
            )
            try #require(built.succeeded, "the fixture package \(name) must build: \(built.standardError)")
            layouts.append(.swiftPackage(scratchPath: scratch))
        }
        return (layouts, await SourceParser().parse(owned).files)
    }

    /* The names a file's symbols hold at the start of its first line, where each fixture declares its function. */
    static func firstLineNames(_ symbols: FileSymbols?) -> [String] {
        symbols?.occurrences(line: 1, column: 13).map(\.name) ?? []
    }

    /* What one store says of one file, asked through a fresh URL: a URL keeps the modification date it was first asked for. */
    static func described(_ file: ParsedFile, by store: IndexStore) -> FileSymbols? {
        IndexStore.symbols(of: [URL(fileURLWithPath: file.url.path)], in: [store]).first ?? nil
    }

    /*
     Every file from the index, each layout's store for its own package's files, and each file with its own
     symbols: the two files of one store are told apart by path, though the store's lookup is built once for both.
     */
    @Test func everyLayoutsStoreDescribesItsOwnFiles() async throws {
        let (layouts, files) = try await Self.builtPackages()
        try #require(files.count == 3)

        let result = SymbolProvider(layouts: layouts, runner: ProcessRunner()).symbols(for: files)

        #expect(result.fromIndex == 3, "every file is in one layout's store: \(result.unavailable)")
        #expect(result.fromSourcekitd == 0)
        #expect(result.unavailable.isEmpty)
        let names = files.map { Self.firstLineNames(result.symbols[$0.url.path]) }
        #expect(names == [["leftReading()"], ["leftSpare()"], ["rightReading()"]], "\(names)")
    }

    /*
     A unit vouches only for the file as it was when the unit was written. One store asked twice: it describes the
     file, the file is touched later than its unit, and the same store no longer describes it, while the file
     beside it, untouched, is still described. Through the provider, the touched file leaves the index's count.
     */
    @Test func aFileTouchedSinceItsUnitIsNotDescribedByIt() async throws {
        let (layouts, files) = try await Self.builtPackages()
        let left = try #require(files.first { $0.url.lastPathComponent == "Left.swift" })
        let spare = try #require(files.first { $0.url.lastPathComponent == "LeftSpare.swift" })
        let firstLayout = try #require(layouts.first)
        let store = try IndexStore(
            libraryPath: IndexStore.toolchainLibraryPath(),
            storePath: IndexStore.storePath(layout: firstLayout),
        )
        #expect(Self.firstLineNames(Self.described(left, by: store)) == ["leftReading()"])

        try FileManager.default.setAttributes(
            [.modificationDate: Date().addingTimeInterval(60)],
            ofItemAtPath: left.url.path,
        )

        #expect(Self.described(left, by: store) == nil, "the unit is older than the file")
        #expect(Self.firstLineNames(Self.described(spare, by: store)) == ["leftSpare()"])
        let reparsed = await SourceParser().parse(
            files.map {
                FileSet.OwnedFile(
                    url: URL(fileURLWithPath: $0.url.path),
                    targetName: $0.targetName,
                    targetKind: $0.targetKind,
                )
            }
        )
        let result = SymbolProvider(layouts: layouts, runner: ProcessRunner()).symbols(for: reparsed.files)
        #expect(result.fromIndex == 2, "the touched file is not the index's to describe: \(result.unavailable)")
        #expect(result.fromSourcekitd + result.unavailable.count == 1)
    }
}
