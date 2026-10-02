import CohereSwift
import Foundation
import Testing

/*
 The known-dirty controls: a real package, built by the real toolchain, run through the whole pipeline
 with one fault injected per phase. Each phase must catch its own fault at the line it was put on, and a
 type error must stop lint. The clean package comes first and must pass with no findings, or the dirty
 runs below would prove only that the pipeline always complains.

 These are slow on purpose (each run is a `swift build`), and they are the only tests that would notice a
 phase quietly wired to nothing: the per-rule and per-phase tests each check a piece, not that the pieces
 are connected. Every run is `--no-fix`, so the fixture is read and never written.
 */
@Suite(.serialized)
struct PipelineControlTests {
    static let manifest = """
        // swift-tools-version:6.0
        import PackageDescription

        let package = Package(name: "Control", targets: [.target(name: "Control")])

        """

    static let configuration = #"{"version":1,"lineLength":120,"indentation":{"spaces":4}}"#

    static let cleanSource = """
        struct Control {
            let value: Int

            func doubled() -> Int {
                value * 2
            }
        }

        """

    /* What one run said: the stream's records by kind, in order. */
    struct Run {
        var records: [[String: Any]]

        func of(_ kind: String) -> [[String: Any]] {
            records.filter { $0["kind"] as? String == kind }
        }

        func phase(_ name: String) -> String? {
            of("phase").first { $0["name"] as? String == name }?["outcome"] as? String
        }

        /* `rule:line` for every finding, so an assertion reads as what was caught and where. */
        var findings: [String] {
            of("finding").map { "\($0["rule"] as? String ?? ""):\($0["line"] as? Int ?? 0)" }
        }
    }

    /* A fresh package per run, because the engine's build cache is keyed by the package's path. */
    static func run(source: String, manifest: String = manifest) async throws -> Run {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-control-\(UUID().uuidString)", isDirectory: true)
        let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        try configuration.write(to: root.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
        try source.write(to: sources.appendingPathComponent("Control.swift"), atomically: true, encoding: .utf8)
        defer {
            do {
                try FileManager.default.removeItem(at: root)
            } catch {
                /* Ignored on purpose: a leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
            }
        }

        let options = try CommandOptions.parse(["--contract", "1", "--root", root.path, "--no-fix"], workingDirectory: root)
        var lines = Data()
        let writer = ContractWriter { lines.append($0) }
        _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
        let records = try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }
        #expect(String(decoding: try Data(contentsOf: sources.appendingPathComponent("Control.swift")), as: UTF8.self) == source, "a --no-fix run wrote to the package")
        return Run(records: records)
    }

    @Test func theCleanPackagePasses() async throws {
        let run = try await Self.run(source: Self.cleanSource)
        #expect(run.findings.isEmpty)
        #expect(run.phase("types") == "ran")
        #expect(run.phase("lint") == "ran")
        #expect(run.of("summary").first?["exitCode"] as? Int == 0)
    }

    @Test func formatCatchesAMisindentedLine() async throws {
        let source = Self.cleanSource.replacingOccurrences(of: "    let value: Int", with: "let value: Int")
        let run = try await Self.run(source: source)
        #expect(run.findings == ["cohere-swift/format:2"])
        #expect(run.phase("lint") == "ran", "a formatting finding does not stop the phases after it")
    }

    @Test func lintCatchesAForceUnwrap() async throws {
        let source = Self.cleanSource.replacingOccurrences(of: "        value * 2", with: "        Int(\"2\")! * value")
        let run = try await Self.run(source: source)
        #expect(run.findings == ["cohere-swift/no-force-unwrap:5"])
        #expect(run.phase("types") == "ran")
    }

    @Test func typesCatchesATypeErrorAndStopsLint() async throws {
        let source = Self.cleanSource.replacingOccurrences(of: "        value * 2", with: "        let text: Int = \"two\"\n        return value * text")
        let run = try await Self.run(source: source)
        #expect(run.findings == [":5"], "the compiler's error, which carries no rule name")
        #expect(run.phase("lint") == "notReached")
        #expect(run.of("summary").first?["exitCode"] as? Int == 1)
    }

    /*
     `--no-fix` forbids resolving dependencies, so a package whose Package.resolved is missing (ahraos-macos
     gitignores its own) fails before the compiler runs. The run must say that, not blame the build layout.
     */
    @Test func aBuildThatFailsBeforeCompilingSaysSo() async throws {
        let manifest = Self.manifest.replacingOccurrences(
            of: #"Package(name: "Control", targets:"#,
            with: #"Package(name: "Control", dependencies: [.package(url: "https://example.invalid/Missing.git", from: "1.0.0")], targets:"#
        )
        await #expect {
            _ = try await Self.run(source: Self.cleanSource, manifest: manifest)
        } throws: { error in
            String(describing: error).contains("failed before compiling anything")
        }
    }
}
