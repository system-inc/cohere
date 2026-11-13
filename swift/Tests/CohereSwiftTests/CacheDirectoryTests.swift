import Foundation
import Testing

@testable import CohereSwift

/*
 The engine's cache directory ignores itself (#6vhp0vd): the call that creates `.cache/cohere` writes its ignore file,
 the same bytes the front door's `cache_directory` writes, and a directory that was there already, as an older cohere
 made it, is left without one.
 */
struct CacheDirectoryTests {
    static func temporaryRoot() throws -> URL {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-cache-directory-\(UUID().uuidString)",
            isDirectory: true,
        )
        try FileManager.default.createDirectory(at: root, withIntermediateDirectories: true)
        return root
    }

    static func remove(_ root: URL) {
        do {
            try FileManager.default.removeItem(at: root)
        }
        catch {
            /* Left in the temporary directory, which the system clears; the test's answer stands either way. */
        }
    }

    @Test func theCallThatCreatesTheDirectoryWritesItsIgnoreFile() throws {
        let root = try Self.temporaryRoot()
        defer { Self.remove(root) }
        let directory = CacheDirectory.of(root: root)
        try CacheDirectory.make(directory)
        let contents = try String(contentsOf: directory.appendingPathComponent(".gitignore"), encoding: .utf8)
        #expect(contents == CacheDirectory.ignoreFileContents)
        #expect(contents.hasSuffix("\n*\n"))
        #expect(Pipeline.scratchPath(for: root).deletingLastPathComponent().path == directory.path)
    }

    @Test func aDirectoryThatWasThereGetsNoIgnoreFile() throws {
        let root = try Self.temporaryRoot()
        defer { Self.remove(root) }
        let directory = CacheDirectory.of(root: root)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try CacheDirectory.make(directory)
        #expect(!FileManager.default.fileExists(atPath: directory.appendingPathComponent(".gitignore").path))
    }
}
