import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The Swift adapter of the text rules' conformance corpus (#4b337jk): one set of cases under the repository's
 `conformance/text_rules/testdata`, one expected verdict each, which every engine's own suite must meet. The Go
 loader validates the corpus's shape for both Go adapters; this reads the same files by walking up from its own
 path, as the policy tests do, and keeps its own copy of the ledger's checks, since only Swift can run Swift:

 - a failing case with no row fails, and so does a row of the other kind than the failure;
 - a row whose case passes fails as stale;
 - a row naming a case that does not exist fails.

 A failure is `verdict` (reported where clean, silent where reported, or a different number of findings) or
 `span` (the right findings, one at the wrong bytes or with no end). A control proves the adapter and the rule
 are live, so its count is checked whatever the ledger says, and so is the start line of each finding whose span
 covers text, since the text is the defect's identity. A span over the whole file (fileStart, linesFrom) holds
 a control to its count alone, since where its finding sits is placement. Only a span row may sit on a control.

 Where families.json names Swift `{"missing": true}`, Swift should have the family's rule and does not yet: none
 of the family's cases runs, a family-level row (one with no case) names the task, and that row goes stale the
 moment a rule is named there.

 The TypeScript adapter resolves each case's own file through the house configuration, overrides included. The
 Swift engine has no per-file settings to resolve: its rule configuration is one severity and one set of options
 per rule, and the one file-name exemption it has (`*.generated.swift` under file-length) is in the rule itself,
 which a case reaches by naming its file. A case reaches a target kind the same way: a path under `Tests/` is a
 test target's file, where `consistency-no-print` does not apply.
 */
struct TextRulesConformanceTests {
    static let corpusRoot = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent()
        .appendingPathComponent("conformance/text_rules")

    static let engine = "swift"

    /* An engine's entry for a family: its rules' registered names, one written as a string or several as a list, whose findings for the family are their union; why the language has no such idea; or a rule it should have and does not yet. */
    enum EngineRule: Decodable {
        case rules([String])
        case none(String)
        case missing

        struct Entry: Decodable {
            let none: String?
            let missing: Bool?
        }

        init(from decoder: any Decoder) throws {
            let container = try decoder.singleValueContainer()
            if let name = try? container.decode(String.self) {
                self = .rules([name])
                return
            }
            if let names = try? container.decode([String].self) {
                guard !names.isEmpty else {
                    throw DecodingError.dataCorruptedError(
                        in: container,
                        debugDescription: "an empty list of rules names none; write none or missing instead",
                    )
                }
                self = .rules(names)
                return
            }
            let entry = try container.decode(Entry.self)
            if entry.missing == true {
                self = .missing
            }
            else if let none = entry.none {
                self = .none(none)
            }
            else {
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription:
                        "an engine's entry is a rule name, a list of them, {\"none\": ...} or {\"missing\": true}",
                )
            }
        }
    }

    /* Which occurrence of a span's text it covers, from 1: one for every language, or one per language, where 0 says that language's form has no such text, since its grammar can't express the construct. */
    enum Occurrence: Decodable {
        case all(Int)
        case byLanguage([String: Int])

        init(from decoder: any Decoder) throws {
            let container = try decoder.singleValueContainer()
            if let number = try? container.decode(Int.self) {
                self = .all(number)
            }
            else {
                self = .byLanguage(try container.decode([String: Int].self))
            }
        }

        func `for`(_ language: String) -> Int? {
            switch self {
                case let .all(number): number
                case let .byLanguage(numbers): numbers[language]
            }
        }
    }

    /* One expected finding: the text it covers and which occurrence, the file's start, or the lines from one on. */
    struct Span: Decodable {
        var covers: String?
        var occurrence: Occurrence?
        var fileStart: Bool?
        var linesFrom: Int?
    }

    /* An option's value: a whole number, the only kind a Swift rule reads, or anything else, which none reads. */
    enum OptionValue: Decodable {
        case number(Int)
        case other

        init(from decoder: any Decoder) throws {
            let container = try decoder.singleValueContainer()
            self = (try? container.decode(Int.self)).map(OptionValue.number) ?? .other
        }
    }

    struct CaseFile: Decodable {
        var why: String
        var control: Bool?
        var expect: [Span]
        var options: [String: OptionValue]?
        var path: String?
        var root: String?
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

    /* One case an engine fails today, or with no case and no kind, one family it has no rule for yet. */
    struct LedgerRow: Decodable {
        var engine: String
        var family: String
        var `case`: String?
        var kind: String?
        var task: String
        var reason: String
    }

    struct Corpus {
        var families: [String: [String: EngineRule]]
        /* The Swift cases compared: each with a Swift form, in a family Swift has a rule for. */
        var cases: [Case]
        var everyCaseId: Set<String>
        /* Every case with a Swift form, compared or not. */
        var swiftCaseIds: Set<String>
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
        var swiftCaseIds: Set<String> = []
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
                swiftCaseIds.insert("\(family)/\(name)")
                guard case .rules = families[family]?[engine] else { continue }
                let source = try String(
                    contentsOf: caseDirectory.appendingPathComponent("case.swift"),
                    encoding: .utf8,
                )
                cases.append(Case(family: family, name: name, file: file, source: source))
            }
        }
        return Corpus(
            families: families,
            cases: cases,
            everyCaseId: everyCaseId,
            swiftCaseIds: swiftCaseIds,
            ledger: ledger,
        )
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
        if let covers = span.covers, let occurrence = span.occurrence?.for(engine) {
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
        /* Each expected range with whether its span covers text, which holds a control to its line. */
        var expected: [(range: ByteRange, coversText: Bool)] = []
        for (index, span) in expect.enumerated() where span.occurrence?.for(engine) != 0 {
            guard let resolved = resolve(span, in: bytes) else {
                return Comparison(
                    failure: "verdict",
                    controlHeld: false,
                    detail: "expect \(index) does not resolve in case.swift",
                )
            }
            expected.append((resolved, span.covers != nil))
        }
        expected.sort { $0.range.start < $1.range.start }
        let sorted = found.sorted { ($0.start, $0.end ?? -1) < ($1.start, $1.end ?? -1) }
        let detail =
            "expected \(describe(expected.map(\.range), bytes, starts)), found \(describe(sorted, bytes, starts))"
        guard sorted.count == expected.count else {
            return Comparison(failure: "verdict", controlHeld: false, detail: detail)
        }
        var comparison = Comparison(failure: nil, controlHeld: true, detail: detail)
        for (finding, expectation) in zip(sorted, expected) {
            if expectation.coversText,
                line(of: finding.start, in: starts) != line(of: expectation.range.start, in: starts)
            {
                comparison.controlHeld = false
            }
            if finding != expectation.range {
                comparison.failure = "span"
            }
        }
        return comparison
    }

    /* The rules a family names for Swift, found by their names among the registered rules, each built with the options it reads of the given ones, as the engine builds it from a project's settings. */
    static func rules(for family: String, in corpus: Corpus, options: [String: Int] = [:]) throws -> [any FileRule] {
        guard case let .rules(names) = corpus.families[family]?[engine] else {
            throw ConformanceError("family \(family) has no Swift rule, and has a Swift case")
        }
        return try names.map { name in
            let read = options.filter { RuleConfiguration.optionsRead[name]?[$0.key] != nil }
            let configuration = RuleConfiguration(
                severities: [:],
                options: read.isEmpty ? [:] : [name: read],
                note: "",
            )
            let registered = RuleRegistry.fileRules(
                vocabulary: try AbbreviationVocabulary.compiledIn(),
                shoutingVocabulary: try ShoutingVocabulary.compiledIn(),
                brandVocabulary: try BrandVocabulary.compiledIn(),
                configuration: configuration,
            )
            guard let found = registered.first(where: { $0.name == name }) else {
                throw ConformanceError(
                    "families.json names \(name) for Swift in \(family), and no rule is registered by that name"
                )
            }
            return found
        }
    }

    struct ConformanceError: Error, CustomStringConvertible {
        var description: String
        init(_ description: String) { self.description = description }
    }

    /* A case's findings from its family's rules, the union of each one's, built with the case's options, as byte ranges of its source. */
    static func run(_ testCase: Case, in corpus: Corpus, options: [String: Int] = [:]) throws -> [ByteRange] {
        let rules = try rules(for: testCase.family, in: corpus, options: options)
        let root = testCase.file.root ?? "/project"
        let path = testCase.file.path ?? "case"
        /* SwiftPM's default layout puts a test target's sources under `Tests/<target>/`, so a case whose path starts there is a test target's file, as the engine's manifest reading would kind it. Every other case is a library's. */
        let targetKind = path.hasPrefix("Tests/") ? "test" : "library"
        let file = ParsedFile(
            url: URL(fileURLWithPath: "\(root)/\(path).swift"),
            targetName: "Fixture",
            targetKind: targetKind,
            source: testCase.source,
            tree: Parser.parse(source: testCase.source),
            nodeCount: 0,
            packageRoot: URL(fileURLWithPath: root),
        )
        let starts = lineStarts(Array(testCase.source.utf8))
        return rules.flatMap { rule in
            (rule.applies(to: file) ? rule.findings(in: file) : []).map { byteRange(of: $0, starts: starts) }
        }
    }

    /*
     The comparison for a case, with its options translated into the engine's own: the whole numbers a rule reads, as
     `RuleConfiguration.optionsRead` names them. An option the rule does not read fails the case as a verdict, since
     the engine can't take it.
     */
    static func judge(_ testCase: Case, in corpus: Corpus) throws -> Comparison {
        guard case let .rules(names) = corpus.families[testCase.family]?[engine] else {
            throw ConformanceError(
                "family \(testCase.family) has no Swift rule, and its case \(testCase.id) was compared"
            )
        }
        var options: [String: Int] = [:]
        for (key, value) in testCase.file.options ?? [:] {
            guard names.contains(where: { RuleConfiguration.optionsRead[$0]?[key] != nil }),
                case let .number(number) = value
            else {
                return Comparison(
                    failure: "verdict",
                    controlHeld: false,
                    detail:
                        "the case gives the option \(key), and \(names.joined(separator: ", ")) takes no such option",
                )
            }
            options[key] = number
        }
        return compare(
            expect: testCase.file.expect,
            source: testCase.source,
            found: try run(testCase, in: corpus, options: options),
        )
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
            guard let entry = corpus.families[row.family]?[Self.engine] else {
                Issue.record("ledger.json has a Swift row for the family \(row.family), which does not exist")
                continue
            }
            guard let caseName = row.case else {
                #expect(row.kind == nil, "\(row.family): a family-level row judges no case, so it has no kind")
                switch entry {
                    case .missing:
                        break
                    case let .rules(names):
                        Issue.record(
                            "\(row.family) has a family-level Swift row, and families.json names \(names.joined(separator: ", ")) there now, so the row is stale: delete it and let the cases run"
                        )
                    case .none:
                        Issue.record("\(row.family) has a family-level Swift row where Swift has no such idea")
                }
                continue
            }
            let id = "\(row.family)/\(caseName)"
            if case .missing = entry {
                Issue.record("ledger.json has a case-level Swift row for \(id), where Swift has no rule for the family")
                continue
            }
            guard corpus.everyCaseId.contains(id) else {
                Issue.record("ledger.json has a Swift row for \(id), a case that does not exist")
                continue
            }
            guard corpus.swiftCaseIds.contains(id), let testCase = corpus.cases.first(where: { $0.id == id }) else {
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
        for (family, entries) in corpus.families {
            guard case .missing = entries[Self.engine] else { continue }
            #expect(
                corpus.ledger.contains { $0.engine == Self.engine && $0.family == family && $0.case == nil },
                "families.json names Swift as missing in \(family), and ledger.json has no family-level row naming the task that adds the rule",
            )
        }
    }

    /*
     A control's check depends on its span's kind: a span over the whole file holds its count alone, one covering text
     holds its line too, and a wrong count fails either.
     */
    @Test func aControlIsHeldToItsLineOnlyWhereItsSpanCoversText() {
        let source = "// one\n// TODO\n"
        let longFile = Self.compare(
            expect: [Span(linesFrom: 2)],
            source: source,
            found: [ByteRange(start: 0, end: 0)],
        )
        #expect(longFile.failure == "span" && longFile.controlHeld, "\(longFile.detail)")
        let folder = Self.compare(
            expect: [Span(fileStart: true)],
            source: source,
            found: [ByteRange(start: 7, end: 7)],
        )
        #expect(folder.failure == "span" && folder.controlHeld, "\(folder.detail)")
        let word = Self.compare(
            expect: [Span(covers: "TODO", occurrence: .all(1))],
            source: source,
            found: [ByteRange(start: 0, end: 4)],
        )
        #expect(word.failure == "span" && !word.controlHeld, "\(word.detail)")
        let twice = Self.compare(
            expect: [Span(linesFrom: 2)],
            source: source,
            found: [ByteRange(start: 0, end: 0), ByteRange(start: 7, end: 15)],
        )
        #expect(twice.failure == "verdict" && !twice.controlHeld, "\(twice.detail)")
    }

    /*
     The comparison can fail: the control's own findings, held to deliberately wrong expectations, fail with the kind
     each is wrong in, and held to exactly what the rule reported (with an end added where it gave none), pass. So a
     green run above is the comparator agreeing, not a comparator unable to disagree.
     */
    @Test func theComparatorCanFail() throws {
        let corpus = try Self.loadCorpus()
        /* The warning-comments control, whose comment holds the word `fix`, a misplacement the engine's finding there differs from. */
        let control = try #require(
            corpus.cases.first { $0.isControl && $0.family == "warning-comments" },
            "the corpus has no Swift control in warning-comments",
        )
        let found = try Self.run(control, in: corpus)
        try #require(!found.isEmpty, "the rule reported nothing on the control \(control.id)")

        #expect(Self.compare(expect: [], source: control.source, found: found).failure == "verdict")

        let misplaced = Self.compare(
            expect: [Span(covers: "fix", occurrence: .all(1))],
            source: control.source,
            found: found,
        )
        #expect(misplaced.failure == "span", "\(misplaced.detail)")

        let bytes = Array(control.source.utf8)
        let ended = found.map { ByteRange(start: $0.start, end: $0.end ?? min($0.start + 4, bytes.count)) }
        let asReported = ended.map { range in
            Span(
                covers: String(decoding: bytes[range.start..<(range.end ?? range.start)], as: UTF8.self),
                occurrence: .all(1),
            )
        }
        let agreed = Self.compare(expect: asReported, source: control.source, found: ended)
        #expect(agreed.failure == nil, "\(agreed.detail)")
    }
}
