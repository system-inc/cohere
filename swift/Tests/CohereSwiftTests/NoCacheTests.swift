import Foundation
import Testing

@testable import CohereSwift

/*
 `--no-cache`: every phase from source, as TypeScript's means it. The run builds in a scratch of its own and
 removes it, so nothing a run before left is read and nothing is written to the project's `.cache`. Presence,
 2026-11-04 (#zggj9xy): with stale state able to mislead, a run that trusts nothing has to exist.
 */
struct NoCacheTests {
    @Test func aRunWithNoCacheBuildsInItsOwnScratchAndLeavesNothing() async throws {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent(
            "cohere-swift-nocache-package-\(UUID().uuidString)",
            isDirectory: true,
        )
        let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try PipelineControlTests.manifest.write(
            to: root.appendingPathComponent("Package.swift"),
            atomically: true,
            encoding: .utf8,
        )
        try PipelineControlTests.cleanSource.write(
            to: sources.appendingPathComponent("Control.swift"),
            atomically: true,
            encoding: .utf8,
        )
        let options = try CommandOptions.parse(
            ["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix", "--no-cache"],
            workingDirectory: root,
        )
        var lines = Data()
        let pipeline = Pipeline(options: options, writer: ContractWriter { lines.append($0) }, workingDirectory: root)
        /*
         This run's own scratch, by its path: other tests run beside this one with `--no-cache`, so a count of the
         temporary directory's scratches would see theirs mid-run and blame this one.
         */
        let scratch = try #require(pipeline.freshScratch, "a --no-cache run named no scratch of its own")
        _ = try await pipeline.run()
        let records = try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }

        let types = try #require(records.first { $0["kind"] as? String == "types" })
        #expect(types["files"] as? Int == 1, "the package was type-checked")
        #expect(records.last?["complete"] as? Bool == true)
        #expect(
            !FileManager.default.fileExists(atPath: root.appendingPathComponent(".cache").path),
            "a --no-cache run wrote to the project's .cache",
        )
        #expect(!FileManager.default.fileExists(atPath: scratch.path), "the run's own scratch outlived it")
    }
}
