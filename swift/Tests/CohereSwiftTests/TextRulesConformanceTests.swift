import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The Swift adapter of the text rules' conformance corpus (#4b337jk): one set of cases under the repository's
 `conformance/textrules/testdata`, one expected verdict each, which every engine's own suite must meet. The Go
 loader validates the corpus's shape for both Go adapters; this reads the same files by walking up from its own
 path, as the policy tests do, and keeps its own copy of the ledger's checks, since only Swift can run Swift:

 - a failing case with no row fails, and so does a row of the other kind than the failure;
 - a row whose case passes fails as stale;
 - a row naming a case that does not exist fails.

 A failure is `verdict` (reported where clean, silent where reported, or a different number of findings) or
 `span` (the right findings, one at the wrong bytes or with no end). A control's count and start lines are
 checked whatever the ledger says, and only a span row may sit on one.
 */
struct TextRulesConformanceTests {
    static let corpusRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("conformance/textrules")

    static let engine = "swift"

    /* An engine's entry for a family: its rule's registered name, or why the language has no such idea. */
    enum EngineRule: Decodable {
        case rule(String)
        case none(String)

        struct NoneEntry: Decodable {
            let none: String
        }

        init(from decoder: any Decoder) throws {
            let container = try decoder.singleValueContainer()
            if let name = try? container.decode(String.self) {
                self = .rule(name)
            }
            else {
                self = .none(try container.decode(NoneEntry.self).none)
            }
        }
    }

    /* One expected finding: the text it covers and which occurrence, the file's start, or the lines from one on. */
    struct Span: Decodable {
        var covers: String?
        var occurrence: Int?
        var fileStart: Bool?
        var linesFrom: Int?
    }

    /* Any JSON value, read for whether it is there and never for what it says. */
    struct PresentValue: Decodable {
        init(from decoder: any Decoder) throws {}
    }

    struct CaseFile: Decodable {
        var why: String
        var control: Bool?
        var expect: [Span]
        var options: [String: PresentValue]?
        var path: String?
        var languages: [String]
    }

    struct Case {
        var family: String
        var name: String
        var file: CaseFile
        var source: String

        var id: String { "\(family)/\(name)" }
        var isControl: Bool { file.control ?? false }
    }

    struct LedgerRow: Decodable {
        var engine: String
        var family: String
        var `case`: String
        var kind: String
        var task: String
        var reason: String
    }

    struct Corpus {
        var families: [String: [String: EngineRule]]
        var cases: [Case]
        var everyCaseId: Set<String>
        var ledger: [LedgerRow]

        func row(for testCase: Case) -> LedgerRow? {
            ledger.first {
                $0.engine == TextRulesConformanceTests.engine && $0.family == testCase.family
                    && $0.case == testCase.name
            }
        }
    }

    /* The corpus's Swift cases, every case's id in any language, the families and the ledger. */
    static func loadCorpus() throws -> Corpus {
        let decoder = JSONDecoder()
        let families = try decoder.decode(
            [String: [String: EngineRule]].self,
            from: Data(contentsOf: corpusRoot.appendingPathComponent("families.json")),
        )
        let ledger = try decoder.decode(
            [LedgerRow].self,
            from: Data(contentsOf: corpusRoot.appendingPathComponent("ledger.json")),
        )
        var cases: [Case] = []
        var everyCaseId: Set<String> = []
        for family in families.keys.sorted() {
            let familyDirectory = corpusRoot.appendingPathComponent("testdata").appendingPathComponent(family)
            let names = try FileManager.default.contentsOfDirectory(atPath: familyDirectory.path).sorted()
            for name in names {
                let caseDirectory = familyDirectory.appendingPathComponent(name)
                let file = try decoder.decode(
                    CaseFile.self,
                    from: Data(contentsOf: caseDirectory.appendingPathComponent("case.json")),
                )
                everyCaseId.insert("\(family)/\(name)")
                guard file.languages.contains(engine) else { continue }
                let source = try String(
                    contentsOf: caseDirectory.appendingPathComponent("case.swift"),
                    encoding: .utf8,
                )
                cases.append(Case(family: family, name: name, file: file, source: source))
            }
        }
        return Corpus(families: families, cases: cases, everyCaseId: everyCaseId, ledger: ledger)
    }

    /* A byte range of a case's source, 0-based UTF-8 offsets with an exclusive end, or a start with no end. */
    struct ByteRange: Equatable {
        var start: Int
        var end: Int?
    }

    /* What the comparison found: the kind of failure or nil for a pass, whether a control's checks held, and what differed. */
    struct Comparison {
        var failure: String?
        var controlHeld: Bool
        var detail: String
    }

    /* The byte offset each line starts at, as Swift ends a line: `\r\n`, `\n`, or a lone `\r`. */
    static func lineStarts(_ bytes: [UInt8]) -> [Int] {
        var starts = [0]
        var index = 0
        while index < bytes.count {
            if bytes[index] == UInt8(ascii: "\n") {
                starts.append(index + 1)
            }
            else if bytes[index] == UInt8(ascii: "\r") {
                if index + 1 < bytes.count, bytes[index + 1] == UInt8(ascii: "\n") {
                    index += 1
                }
                starts.append(index + 1)
            }
            index += 1
        }
        return starts
    }

    /* The 1-based line an offset sits on. */
    static func line(of offset: Int, in starts: [Int]) -> Int {
        starts.lastIndex { $0 <= offset }.map { $0 + 1 } ?? 1
    }

    /* A span's byte range in a source, or nil where it names text the source does not hold. */
    static func resolve(_ span: Span, in bytes: [UInt8]) -> ByteRange? {
        if let covers = span.covers, let occurrence = span.occurrence {
            let needle = Array(covers.utf8)
            guard !needle.isEmpty, needle.count <= bytes.count else { return nil }
            var seen = 0
            for start in 0...(bytes.count - needle.count) where Array(bytes[start..<(start + needle.count)]) == needle {
                seen += 1
                if seen == occurrence {
                    return ByteRange(start: start, end: start + needle.count)
                }
            }
            return nil
        }
        if span.fileStart == true {
            return ByteRange(start: 0, end: 0)
        }
        if let linesFrom = span.linesFrom {
            let starts = lineStarts(bytes)
            guard linesFrom >= 1, linesFrom <= starts.count else { return nil }
            return ByteRange(start: starts[linesFrom - 1], end: bytes.count)
        }
        return nil
    }

    /* A finding's 1-based line and UTF-8 byte column, and its exclusive end when it has one, as a byte range. */
    static func byteRange(of finding: FindingRecord, starts: [Int]) -> ByteRange {
        let start = offset(line: finding.line, column: finding.column, starts: starts)
        guard let endLine = finding.endLine, let endColumn = finding.endColumn else {
            return ByteRange(start: start, end: nil)
        }
        return ByteRange(start: start, end: offset(line: endLine, column: endColumn, starts: starts))
    }

    /* The byte offset of a 1-based line and UTF-8 byte column. A line past the source's last reads as its last. */
    static func offset(line: Int, column: Int, starts: [Int]) -> Int {
        starts[min(max(line - 1, 0), starts.count - 1)] + column - 1
    }

    static func describe(_ ranges: [ByteRange], _ bytes: [UInt8], _ starts: [Int]) -> String {
        if ranges.isEmpty { return "nothing" }
        return ranges.map { range in
            let line = line(of: range.start, in: starts)
            guard let end = range.end else { return "line \(line) from byte \(range.start) with no end" }
            guard range.start >= 0, end <= bytes.count, range.start <= end else {
                return "[\(range.start),\(end)), outside the source"
            }
            let text = String(decoding: bytes[range.start..<end], as: UTF8.self)
            return "line \(line) [\(range.start),\(end)) \"\(text)\""
        }.joined(separator: ", ")
    }

    /* The comparator: an engine's findings, as byte ranges, held to a case's expectation in source order. */
    static func compare(expect: [Span], source: String, found: [ByteRange]) -> Comparison {
        let bytes = Array(source.utf8)
        let starts = lineStarts(bytes)
        var expected: [ByteRange] = []
        for (index, span) in expect.enumerated() {
            guard let resolved = resolve(span, in: bytes) else {
                return Comparison(
                    failure: "verdict",
                    controlHeld: false,
                    detail: "expect \(index) does not resolve in case.swift",
                )
            }
            expected.append(resolved)
        }
        expected.sort { $0.start < $1.start }
        let sorted = found.sorted { ($0.start, $0.end ?? -1) < ($1.start, $1.end ?? -1) }
        let detail = "expected \(describe(expected, bytes, starts)), found \(describe(sorted, bytes, starts))"
        guard sorted.count == expected.count else {
            return Comparison(failure: "verdict", controlHeld: false, detail: detail)
        }
        var comparison = Comparison(failure: nil, controlHeld: true, detail: detail)
        for (finding, expectation) in zip(sorted, expected) {
            if line(of: finding.start, in: starts) != line(of: expectation.start, in: starts) {
                comparison.controlHeld = false
            }
            if finding != expectation {
                comparison.failure = "span"
            }
        }
        return comparison
    }

    /* The rule a family names for Swift, found among the registered rules by its name. */
    static func rule(for family: String, in corpus: Corpus) throws -> any FileRule {
        guard case let .rule(name) = corpus.families[family]?[engine] else {
            throw ConformanceError("family \(family) has no Swift rule, and has a Swift case")
        }
        let rules = RuleRegistry.fileRules(vocabulary: try AbbreviationVocabulary.compiledIn())
        guard let found = rules.first(where: { $0.name == name }) else {
            throw ConformanceError(
                "families.json names \(name) for Swift in \(family), and no rule is registered by that name"
            )
        }
        return found
    }

    struct ConformanceError: Error, CustomStringConvertible {
        var description: String
        init(_ description: String) { self.description = description }
    }

    /* A case's findings from its family's rule, as byte ranges of its source. */
    static func run(_ testCase: Case, in corpus: Corpus) throws -> [ByteRange] {
        let rule = try rule(for: testCase.family, in: corpus)
        let directory = testCase.file.path.map { "/project/\($0)" } ?? "/project"
        let file = ParsedFile(
            url: URL(fileURLWithPath: "\(directory)/case.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: testCase.source,
            tree: Parser.parse(source: testCase.source),
            nodeCount: 0,
        )
        let starts = lineStarts(Array(testCase.source.utf8))
        let findings = rule.applies(to: file) ? rule.findings(in: file) : []
        return findings.map { byteRange(of: $0, starts: starts) }
    }

    /* The comparison for a case, or a verdict failure where the case gives options, which no Swift rule here takes. */
    static func judge(_ testCase: Case, in corpus: Corpus) throws -> Comparison {
        if let options = testCase.file.options, !options.isEmpty {
            return Comparison(
                failure: "verdict",
                controlHeld: false,
                detail:
                    "the case gives options \(options.keys.sorted()), and the Swift adapter has no translation for them",
            )
        }
        return compare(expect: testCase.file.expect, source: testCase.source, found: try run(testCase, in: corpus))
    }

    @Test func everySwiftCaseMeetsItsExpectationOrItsLedgerRow() throws {
        let corpus = try Self.loadCorpus()
        #expect(!corpus.cases.isEmpty, "the corpus has no Swift cases, so this adapter would be green over nothing")
        var compared = 0
        for testCase in corpus.cases {
            let comparison = try Self.judge(testCase, in: corpus)
            compared += 1
            if testCase.isControl, !comparison.controlHeld {
                Issue.record(
                    "\(testCase.id) is its family's control, and Swift does not report it as a control must, which no ledger row can excuse: \(comparison.detail)"
                )
                continue
            }
            let row = corpus.row(for: testCase)
            switch (comparison.failure, row) {
                case (nil, nil):
                    break
                case let (nil, row?):
                    Issue.record(
                        "\(testCase.id) now passes in Swift, so its \(row.kind) row (task \(row.task)) is stale: delete it"
                    )
                case let (failure?, nil):
                    Issue.record(
                        "\(testCase.id) fails in Swift with a \(failure) failure and no ledger row: \(comparison.detail)"
                    )
                case let (failure?, row?) where row.kind != failure:
                    Issue.record(
                        "\(testCase.id) fails in Swift with a \(failure) failure, and its ledger row (task \(row.task)) says \(row.kind): \(comparison.detail)"
                    )
                case (.some, .some):
                    break
            }
        }
        /* The count the loader gives, against the cases compared, so a case skipped or lost on the way is missed by count. */
        let fromTheCorpus = corpus.cases.count
        #expect(fromTheCorpus > 0)
        #expect(compared == fromTheCorpus, "compared \(compared) Swift cases, and the corpus has \(fromTheCorpus)")
    }

    @Test func everySwiftLedgerRowNamesACaseWithASwiftForm() throws {
        let corpus = try Self.loadCorpus()
        for row in corpus.ledger where row.engine == Self.engine {
            let id = "\(row.family)/\(row.case)"
            guard corpus.everyCaseId.contains(id) else {
                Issue.record("ledger.json has a Swift row for \(id), a case that does not exist")
                continue
            }
            guard let testCase = corpus.cases.first(where: { $0.id == id }) else {
                Issue.record("ledger.json has a Swift row for \(id), which has no Swift form")
                continue
            }
            #expect(
                row.kind == "verdict" || row.kind == "span",
                "\(id): a row's kind is verdict or span, not \(row.kind)",
            )
            #expect(
                !testCase.isControl || row.kind == "span",
                "\(id) is a control, and only a span row may sit on one",
            )
        }
    }

    /*
     The comparison can fail: the control's own findings, held to deliberately wrong expectations, fail with the kind
     each is wrong in, and held to exactly what the rule reported (with an end added where it gave none), pass. So a
     green run above is the comparator agreeing, not a comparator unable to disagree.
     */
    @Test func theComparatorCanFail() throws {
        let corpus = try Self.loadCorpus()
        let control = try #require(corpus.cases.first { $0.isControl }, "the corpus has no Swift control")
        let found = try Self.run(control, in: corpus)
        try #require(!found.isEmpty, "the rule reported nothing on the control \(control.id)")

        #expect(Self.compare(expect: [], source: control.source, found: found).failure == "verdict")

        let misplaced = Self.compare(
            expect: [Span(covers: "fix", occurrence: 1)],
            source: control.source,
            found: found,
        )
        #expect(misplaced.failure == "span", "\(misplaced.detail)")

        let bytes = Array(control.source.utf8)
        let ended = found.map { ByteRange(start: $0.start, end: $0.end ?? min($0.start + 4, bytes.count)) }
        let asReported = ended.map { range in
            Span(
                covers: String(decoding: bytes[range.start..<(range.end ?? range.start)], as: UTF8.self),
                occurrence: 1,
            )
        }
        let agreed = Self.compare(expect: asReported, source: control.source, found: ended)
        #expect(agreed.failure == nil, "\(agreed.detail)")
    }
}
