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
        // swift-tools-version:6.2
        import PackageDescription

        let package = Package(
            name: "Control",
            targets: [
                .target(
                    name: "Control",
                    swiftSettings: [.enableUpcomingFeature("ExistentialAny"), .enableUpcomingFeature("MemberImportVisibility"), .strictMemorySafety()]
                )
            ]
        )

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
    static func run(source: String, manifest: String = manifest, otherFiles: [String: Data] = [:], arguments: [String] = [], mutating: Bool = false, sink: ((Data) -> Void)? = nil) async throws -> Run {
        let root = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-control-\(UUID().uuidString)", isDirectory: true)
        let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
        try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
        try manifest.write(to: root.appendingPathComponent("Package.swift"), atomically: true, encoding: .utf8)
        try configuration.write(to: root.appendingPathComponent(".swift-format"), atomically: true, encoding: .utf8)
        try source.write(to: sources.appendingPathComponent("Control.swift"), atomically: true, encoding: .utf8)
        for (name, contents) in otherFiles {
            try contents.write(to: sources.appendingPathComponent(name))
        }
        defer {
            do {
                try FileManager.default.removeItem(at: root)
            } catch {
                /* Ignored on purpose: a leftover fixture in the temporary directory costs nothing, and failing a test that already answered would hide its answer. */
            }
        }

        let options = try CommandOptions.parse(["--contract", "\(EngineVersion.contract)", "--root", root.path] + (mutating ? [] : ["--no-fix"]) + arguments, workingDirectory: root)
        var lines = Data()
        let writer = ContractWriter { data in
            lines.append(data)
            sink?(data)
        }
        _ = try await Pipeline(options: options, writer: writer, workingDirectory: root).run()
        let records = try lines.split(separator: UInt8(ascii: "\n")).map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])
        }
        if !mutating {
            #expect(String(decoding: try Data(contentsOf: sources.appendingPathComponent("Control.swift")), as: UTF8.self) == source, "a --no-fix run wrote to the package")
        }
        return Run(records: records)
    }

    @Test func theCleanPackagePasses() async throws {
        let run = try await Self.run(source: Self.cleanSource)
        #expect(run.findings.isEmpty)
        #expect(run.phase("types") == "ran")
        #expect(run.phase("lint") == "ran")
        #expect(run.of("summary").first?["exitCode"] as? Int == 0)
    }

    /*
     The front door reads a skip whose detail is exactly `not requested` (for unused, that phrase, the dash and how to
     ask) as no gap, and any other skip as one (f9ec90c, Contract.md). The words are written out here rather than read
     from the engine's constants, so rewording them fails this test before it breaks every narrowed run.
     */
    @Test func aPhaseNobodyAskedForIsSkippedInTheContractsWords() async throws {
        let run = try await Self.run(source: Self.cleanSource, arguments: ["--lint"])
        let details = Dictionary(run.of("phase").compactMap { phase in
            (phase["name"] as? String).map { ($0, "\(phase["outcome"] ?? "")|\(phase["detail"] ?? "")") }
        }, uniquingKeysWith: { first, _ in first })
        #expect(details["fix"] == "skipped|not requested")
        #expect(details["types"] == "skipped|not requested")
        #expect(details["unused"] == "skipped|not requested — this is a report, ask for it with --unused")
        #expect(run.phase("lint") == "ran")
        #expect(run.of("summary").first?["complete"] as? Bool == true)
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

    /*
     When the fix phase rewrote nothing, lint reuses its last walk instead of walking again, and must find exactly
     what a fresh walk finds. The --no-fix run has no fixer, so its lint walks; the two are compared.
     */
    @Test func lintReusesTheFixWalkAndFindsTheSame() async throws {
        let source = Self.cleanSource.replacingOccurrences(of: "        value * 2", with: "        Int(\"2\")! * value")
        let reused = try await Self.run(source: source, mutating: true)
        let walked = try await Self.run(source: source)
        #expect(reused.phase("lint") == "reused")
        #expect(reused.of("lint").first?["reusedFrom"] as? String == "fix")
        #expect(walked.phase("lint") == "ran")
        #expect(reused.findings == walked.findings)
        #expect(reused.findings == ["cohere-swift/no-force-unwrap:5"])
    }

    @Test func typesCatchesATypeErrorAndStopsLint() async throws {
        let source = Self.cleanSource.replacingOccurrences(of: "        value * 2", with: "        let text: Int = \"two\"\n        return value * text")
        let run = try await Self.run(source: source)
        #expect(run.findings == [":5"], "the compiler's error, which carries no rule name")
        #expect(run.phase("lint") == "notReached")
        #expect(run.of("summary").first?["exitCode"] as? Int == 1)
    }

    /*
     The naming rules' vocabulary, both ways. Missing or unreadable, the run is refused before the package is
     described, naming the path, so a naming rule with no words can never read as a clean tree. Present at the
     path `--abbreviations` names, it is the one the rule judges with.
     */
    @Test(arguments: ["missing", "malformed"])
    func aVocabularyThatCannotBeLoadedRefusesTheRun(problem: String) async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-vocabulary-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let path = directory.appendingPathComponent("abbreviations.json")
        if problem == "malformed" {
            try Data(#"{"abbreviations": [{"abbreviation": "val", "expansion": "value", "suffx": {}}]}"#.utf8).write(to: path)
        }
        var written = Data()
        do {
            _ = try await Self.run(source: Self.cleanSource, arguments: ["--abbreviations", path.path]) { written.append($0) }
            Issue.record("a \(problem) vocabulary did not refuse the run")
        } catch let failure as Pipeline.RunFailure {
            #expect(failure.description.contains(path.path), "the refusal must name the path it looked at: \(failure)")
            #expect(failure.description.contains("nothing was checked"))
        }
        let kinds = written.split(separator: UInt8(ascii: "\n")).compactMap { line in
            (try? JSONSerialization.jsonObject(with: Data(line)) as? [String: Any])?["kind"] as? String
        }
        #expect(kinds == ["provenance"], "nothing past provenance may be written before the refusal: \(kinds)")
    }

    @Test func theVocabularyAtTheGivenPathIsTheOneJudgedWith() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent("cohere-swift-vocabulary-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let path = directory.appendingPathComponent("words.json")
        try Data(#"{"abbreviations": [{"abbreviation": "qty", "expansion": "quantity", "whole": {"messageId": "noQty", "style": "plain"}}]}"#.utf8).write(to: path)
        let source = Self.cleanSource.replacingOccurrences(of: "    let value: Int", with: "    let qty: Int")
        let run = try await Self.run(source: source.replacingOccurrences(of: "value * 2", with: "qty * 2"), arguments: ["--abbreviations", path.path])
        #expect(run.findings == ["cohere-swift/no-abbreviated-identifier:2"], "a word only the given file holds is the proof it was read")
    }

    /* Contract 2: a file the engine cannot read is a record, between `project` and the first `phase`, and the run is incomplete. */
    @Test func anUnreadableFileIsNamedInARecord() async throws {
        let latin1 = Data("// caf".utf8) + Data([0xE9, 0x0A])
        let run = try await Self.run(source: Self.cleanSource, otherFiles: ["Latin1.swift": latin1])
        let kinds = run.records.compactMap { $0["kind"] as? String }
        let unreadable = try #require(run.of("unreadable").first)
        #expect((unreadable["file"] as? String)?.hasSuffix("/Sources/Control/Latin1.swift") == true)
        #expect((unreadable["error"] as? String)?.isEmpty == false)
        let at = try #require(kinds.firstIndex(of: "unreadable"))
        #expect(kinds.firstIndex(of: "project").map { $0 < at } == true)
        #expect(kinds.firstIndex(of: "phase").map { at < $0 } == true)
        #expect(run.of("summary").first?["complete"] as? Bool == false)
    }

    /*
     `--no-fix` forbids resolving dependencies, so a package whose Package.resolved is missing (ahraos-macos
     gitignores its own) fails before the compiler runs. The run must say that, not blame the build layout.
     */
    @Test func aBuildThatFailsBeforeCompilingSaysSo() async throws {
        let manifest = Self.manifest.replacingOccurrences(
            of: "name: \"Control\",\n    targets:",
            with: "name: \"Control\",\n    dependencies: [.package(url: \"https://example.invalid/Missing.git\", from: \"1.0.0\")],\n    targets:"
        )
        await #expect {
            _ = try await Self.run(source: Self.cleanSource, manifest: manifest)
        } throws: { error in
            String(describing: error).contains("failed before compiling anything")
        }
    }
}
