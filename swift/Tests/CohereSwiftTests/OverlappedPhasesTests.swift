import Foundation
import Testing

@testable import CohereSwift

/*
 The run computes side by side and writes in one order (`Pipeline.overlapsPhases`): the types phase's in-process check
 of a body-only edit runs beside the fix phase, the scripts' check and lint's reads of the index, and the scripts'
 check beside the package's. What it may never do is ask about a text the fix phase is about to rewrite, or write
 anything other than what the serial run writes.

 Each test writes its own UUID-named package with a standalone script beside the target, so the scripts' check runs
 beside the package's too.
 */
@Suite
struct OverlappedPhasesTests {
    final class Package {
        let root: URL
        let source: URL

        init() throws {
            root = FileManager.default.temporaryDirectory.appendingPathComponent(
                "cohere-swift-overlap-\(UUID().uuidString)",
                isDirectory: true,
            )
            let sources = root.appendingPathComponent("Sources/Control", isDirectory: true)
            source = sources.appendingPathComponent("Control.swift")
            try FileManager.default.createDirectory(at: sources, withIntermediateDirectories: true)
            let scripts = root.appendingPathComponent("Scripts", isDirectory: true)
            try FileManager.default.createDirectory(at: scripts, withIntermediateDirectories: true)
            try PipelineControlTests.manifest.write(
                to: root.appendingPathComponent("Package.swift"),
                atomically: true,
                encoding: .utf8,
            )
            try "print(\"greeted\")\n".write(
                to: scripts.appendingPathComponent("Greet.swift"),
                atomically: true,
                encoding: .utf8,
            )
            try write(PipelineControlTests.cleanSource)
        }

        func write(_ text: String) throws {
            try text.write(to: source, atomically: true, encoding: .utf8)
        }

        var text: String {
            get throws { String(decoding: try Data(contentsOf: source), as: UTF8.self) }
        }

        /* One run, its records in the order written, as JSON lines. */
        func run(mutating: Bool, overlapsPhases: Bool = true) async throws -> [String] {
            let options = try CommandOptions.parse(
                ["--contract", "\(EngineVersion.contract)", "--root", root.path] + (mutating ? [] : ["--no-fix"]),
                workingDirectory: root,
            )
            var lines = Data()
            let writer = ContractWriter { lines.append($0) }
            var pipeline = Pipeline(options: options, writer: writer, workingDirectory: root)
            pipeline.overlapsPhases = overlapsPhases
            _ = try await pipeline.run()
            return lines.split(separator: UInt8(ascii: "\n")).map { String(decoding: $0, as: UTF8.self) }
        }

        deinit {
            do {
                try FileManager.default.removeItem(at: root)
            }
            catch {
                /* A leftover fixture in the temporary directory costs nothing. */
            }
        }
    }

    static func records(_ lines: [String]) throws -> [[String: Any]] {
        try lines.map { line in
            try #require(try JSONSerialization.jsonObject(with: Data(line.utf8)) as? [String: Any])
        }
    }

    /*
     A line with what varies between two runs that did the same thing left out: every elapsed time, and the package's
     own folder, which differs between two packages.
     */
    static func comparable(_ line: String, root: URL) throws -> String {
        func strip(_ value: Any) -> Any {
            if let object = value as? [String: Any] {
                return object.filter { $0.key != "elapsedMilliseconds" }.mapValues(strip)
            }
            if let array = value as? [Any] {
                return array.map(strip)
            }
            if let text = value as? String {
                return text.replacingOccurrences(of: root.resolvingSymlinksInPath().path, with: "<root>")
                    .replacingOccurrences(of: root.path, with: "<root>")
            }
            return value
        }
        let object = strip(try JSONSerialization.jsonObject(with: Data(line.utf8)))
        let encoded = try JSONSerialization.data(withJSONObject: object, options: [.sortedKeys])
        return String(decoding: encoded, as: UTF8.self)
    }

    /* A body edit that leaves an unused constant badly indented: the formatter moves it, and the compiler's warning with it. */
    static let unindentedUnused = PipelineControlTests.cleanSource.replacingOccurrences(
        of: "        value * 2",
        with: "let unused = value\n        return value * 2",
    )

    /*
     With fixes on, the in-process check asks about the text the fix phase leaves, never the text it read: the formatter
     indents the edited body, and the compiler's warning about it is placed in the indented text. A check begun before
     the fix phase wrote would place it at the column the unindented text had.
     */
    @Test func aFixedBodyEditIsAskedAboutAsTheFixLeftIt() async throws {
        let package = try Package()
        _ = try await package.run(mutating: true)
        try package.write(Self.unindentedUnused)
        let records = try Self.records(try await package.run(mutating: true))
        let fixed = try package.text
        #expect(fixed != Self.unindentedUnused, "the fix phase left the edit as it was, so this test proves nothing")
        let build = records.first { $0["kind"] as? String == "types" }?["build"] as? String ?? ""
        #expect(build.contains(TypesPhase.checkedInProcessWords), "the edit was not checked in process: \(build)")
        let warning = try #require(
            records.first { record in
                record["kind"] as? String == "finding" && record["source"] as? String == "compiler"
                    && (record["message"] as? String ?? "").contains("'unused'")
            },
            "the compiler said nothing of the unused constant, so this test proves nothing: \(records)",
        )
        let lines = fixed.split(separator: "\n", omittingEmptySubsequences: false)
        let line = try #require(warning["line"] as? Int)
        let column = try #require(warning["column"] as? Int)
        let declared = try #require(lines.firstIndex { $0.contains("unused") })
        let named = try #require(lines[declared].range(of: "unused"))
        #expect(line == declared + 1, "the warning is not on the line the fixed text declares it: \(warning)")
        #expect(
            column == lines[declared].utf8.distance(from: lines[declared].startIndex, to: named.lowerBound) + 1,
            "the warning is placed in the text before the fix phase rewrote it: \(warning), fixed text \(fixed)",
        )
    }

    /* A body edit with a compiler warning and a lint finding, so types and lint both have something to write. */
    static let warnedAndUnwrapped = PipelineControlTests.cleanSource.replacingOccurrences(
        of: "        value * 2",
        with: "        var twice = value * 2\n        return twice + Int(\"0\")!",
    )

    /*
     A body edit's records are the serial run's, written in the serial run's order: two packages with the same history,
     one run side by side and one in series, write the same stream but for elapsed times and their own folders.
     */
    @Test func aBodyEditWritesTheSerialRunsRecordsInItsOrder() async throws {
        let overlapped = try Package()
        let serial = try Package()
        _ = try await overlapped.run(mutating: false)
        _ = try await serial.run(mutating: false)
        try overlapped.write(Self.warnedAndUnwrapped)
        try serial.write(Self.warnedAndUnwrapped)
        let side = try await overlapped.run(mutating: false)
        let series = try await serial.run(mutating: false, overlapsPhases: false)
        let sideRecords = try Self.records(side)
        let build = sideRecords.first { $0["kind"] as? String == "types" }?["build"] as? String ?? ""
        #expect(build.contains(TypesPhase.checkedInProcessWords), "the edit was not checked in process: \(build)")
        #expect(build.contains("1 script checked alone"), "the script was not checked: \(build)")
        #expect(
            sideRecords.contains { $0["kind"] as? String == "finding" && $0["source"] as? String == "compiler" },
            "no compiler finding, so the types records prove little: \(side)",
        )
        #expect(
            sideRecords.contains { $0["kind"] as? String == "finding" && $0["source"] as? String == "rule" },
            "no lint finding, so the lint records prove little: \(side)",
        )
        let sideLines = try side.map { try Self.comparable($0, root: overlapped.root) }
        let seriesLines = try series.map { try Self.comparable($0, root: serial.root) }
        #expect(
            sideLines == seriesLines,
            "side by side and in series differ: \(sideLines.filter { !seriesLines.contains($0) }) against \(seriesLines.filter { !sideLines.contains($0) })",
        )
    }
}
