import Foundation
import Testing

@testable import CohereSwift

/*
 Reusing the last build's compiler records, on one real package across several runs, so the build cache is
 warm the way a developer's is.

 A reuse is proven by the types record's own words, and every change a build would see must end it: a planted
 type error after a warm run must be found (the failure a cache that never invalidates would hide), and a
 record removed from the scratch must mean building, never a run that comes back incomplete.
 */
@Suite(.serialized)
struct BuildInputSnapshotTests {
    static let reusedWords = "no input changed since the last successful build"

    final class Package {
        let root: URL
        let sources: URL

        init() throws {
            root = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-build-\(UUID().uuidString)", isDirectory: true)
            sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
            try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
            try PipelineControlTests.manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
            try PipelineControlTests.configuration.write(to: root.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
            try write(PipelineControlTests.cleanSource)
        }

        func write(_ source: String) throws {
            try source.write(to: sources.appendingPathComponent("Control.swift"), atomically: true, encoding: .utf8)
        }

        /* One `--no-fix` run: the types record's `build` words, and the compiler findings. */
        func run() async throws -> (build: String, compilerFindings: [String]) {
            let options = try CommandOptions.parse(["--contract", "\(EngineVersion.contract)", "--root", root.path, "--no-fix"], workingDirectory: root)
            var lines = Data()
            let writer = ContractWriter { lines.append($0) }
            _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
            var build = ""
            var compiler: [String] = []
            for line in lines.split(separator: UInt8(ascii: "\n")) {
                let record = try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
                if record["kind"] as? String == "types" {
                    build = record["build"] as? String ?? ""
                }
                if record["kind"] as? String == "finding", record["source"] as? String == "compiler" {
                    compiler.append("\(record["line"] as? Int ?? 0): \(record["message"] as? String ?? "")")
                }
            }
            return (build, compiler)
        }
    }

    @Test func aRunWithNothingChangedReadsTheLastBuildWithoutBuilding() async throws {
        let package = try Package()
        let first = try await package.run()
        #expect(!first.build.contains(Self.reusedWords), "a first run has nothing to reuse")
        let second = try await package.run()
        #expect(second.build.contains(Self.reusedWords), "the second run built again: \(second.build)")
        #expect(second.compilerFindings == first.compilerFindings)
    }

    /* The test that matters: after a warm run, a planted type error must be found. */
    @Test func aChangeAfterAWarmRunIsBuiltAndFound() async throws {
        let package = try Package()
        _ = try await package.run()
        _ = try await package.run()
        try package.write(PipelineControlTests.cleanSource.replacingOccurrences(of: "        value * 2", with: "        let text: Int = \"two\"\n        return value * text"))
        let changed = try await package.run()
        #expect(!changed.build.contains(Self.reusedWords))
        #expect(changed.compilerFindings.contains { $0.hasPrefix("5: cannot convert") }, "the planted error was not found: \(changed.compilerFindings)")
    }

    /* A failed build is never the one reused: fixing the error after it must build again and come back clean. */
    @Test func aFailedBuildIsNeverReused() async throws {
        let package = try Package()
        try package.write(PipelineControlTests.cleanSource.replacingOccurrences(of: "        value * 2", with: "        let text: Int = \"two\"\n        return value * text"))
        _ = try await package.run()
        let again = try await package.run()
        #expect(!again.build.contains(Self.reusedWords), "a failed build's records were reused")
        #expect(!again.compilerFindings.isEmpty)
    }

    @Test func aManifestEditBuilds() async throws {
        let package = try Package()
        _ = try await package.run()
        try (PipelineControlTests.manifest + "// edited\n").write(to: package.root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        #expect(!(try await package.run().build.contains(Self.reusedWords)))
    }

    /* A record gone from the scratch means building, not a run that reports a file without a record. */
    @Test func aMissingRecordBuildsInsteadOfComingBackIncomplete() async throws {
        let package = try Package()
        _ = try await package.run()
        let scratch = Pipeline.scratchPath(for: package.root)
        let records = FileManager.default.enumerator(at: scratch, includingPropertiesForKeys: nil)?
            .compactMap { $0 as? URL }
            .filter { $0.pathExtension == "dia" && $0.lastPathComponent.hasPrefix("Control") } ?? []
        #expect(!records.isEmpty, "found no record to remove, so this test would prove nothing")
        for record in records {
            try FileManager.default.removeItem(at: record)
        }
        let after = try await package.run()
        #expect(!after.build.contains(Self.reusedWords))
    }

    @Test func theSnapshotSeesSizeAndTime() throws {
        let package = try Package()
        let take = { BuildInputSnapshot.take(roots: [package.root], resolved: package.root.appendingPathComponent("Package.resolved"), toolchain: "t", arguments: ["a"]) }
        let first = take()
        #expect(take() == first, "two snapshots of an unchanged tree differ")
        try package.write(PipelineControlTests.cleanSource + "// longer\n")
        #expect(take() != first)
        #expect(BuildInputSnapshot.take(roots: [package.root], resolved: package.root.appendingPathComponent("Package.resolved"), toolchain: "u", arguments: ["a"]) != take())
    }
}
