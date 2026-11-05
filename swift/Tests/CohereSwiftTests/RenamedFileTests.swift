import Foundation
import Testing

@testable import CohereSwift

/*
 A lint-only run after a file is renamed with plain mv. `--lint` does not build, so a typed rule that needs
 sourcekitd for a changed file asks it with the last build's compile arguments, and those name the file's old
 path. Presence, 2026-11-04 (#zggj9xy): "error opening input file" for the old names, and answers from a module
 that no longer exists. Such a command vouches for nothing, so the file is reported unchecked, with why.
 */
struct RenamedFileTests {
    static func run(_ root: URL, _ arguments: [String]) async throws -> [[String: Any]] {
        let options = try CommandOptions.parse(
            ["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix"] + arguments,
            workingDirectory: root,
        )
        var lines = Data()
        _ = try await Pipeline(options: options, writer: ContractWriter { lines.append($0) }, workingDirectory: root)
            .run()
        return try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }
    }

    @Test func aModuleWhoseFileWasRenamedSinceTheBuildVouchesForNothing() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-renamed-\(UUID().uuidString)",
            isDirectory: true,
        )
        let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try PipelineControlTests.manifest.write(
            to: root.appendingPathComponent("Package.swift"),
            atomically: true,
            encoding: .utf8,
        )
        let box = sources.appendingPathComponent("Box.swift")
        let source =
            "struct Box {\n    let items: [Int]\n\n    func any() -> Bool {\n        items.count > 0\n    }\n}\n"
        try source.write(to: box, atomically: true, encoding: .utf8)
        try "struct Old {\n    let names: [String]\n}\n".write(
            to: sources.appendingPathComponent("Old.swift"),
            atomically: true,
            encoding: .utf8,
        )

        /* The control: built and current, the typed rule answers from the build. */
        let built = try await Self.run(root, [])
        #expect(built.contains { $0["rule"] as? String == "cohere-swift/empty-count" })

        try FileManager.default.moveItem(
            at: sources.appendingPathComponent("Old.swift"),
            to: sources.appendingPathComponent("New.swift"),
        )
        try (source + "\n").write(to: box, atomically: true, encoding: .utf8)
        let linted = try await Self.run(root, ["--lint"])
        let crashes = linted.first { $0["kind"] as? String == "lint" }?["crashes"] as? [[String: Any]] ?? []
        #expect(
            crashes.map { ($0["file"] as? String).map { URL(fileURLWithPath: $0).lastPathComponent } } == ["Box.swift"]
        )
        #expect((crashes.first?["error"] as? String)?.contains("Old.swift, which no longer exists") == true)
        #expect(
            !linted.contains { $0["rule"] as? String == "cohere-swift/empty-count" },
            "no answer from the old module",
        )
        #expect(linted.last?["complete"] as? Bool == false)
    }
}
