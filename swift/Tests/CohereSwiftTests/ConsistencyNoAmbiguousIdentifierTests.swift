import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 Fixture pairs for `cohere-swift/consistency-no-ambiguous-identifier`, both directions, carrying every case the Go rule's
 own test documents (`consistency_no_ambiguous_identifier_test.go`) that has a Swift shape. Failing cases
 assert the exact line and column of the declared name. The passing cases are the near misses: the
 coordinates, the names somebody else chose, and the discard. Comparators and numeric functions get no exemption
 (Kirk, 2026-11-06), so their letters are failing cases.
 */
struct ConsistencyNoAmbiguousIdentifierTests {
    static func findings(_ source: String) -> [FindingRecord] {
        let url = URL(fileURLWithPath: "/fixture/Subject.swift")
        let file = ParsedFile(
            url: url,
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
        return ConsistencyNoAmbiguousIdentifier().findings(in: file)
    }

    static func positions(_ source: String) -> [String] {
        findings(source).map { "\($0.line):\($0.column)" }
    }

    @Test func singleLettersAreFound() {
        let source = """
            let n = 1
            let a = 1
            let b = 2
            for i in rows {}
            let total = items.map { v in v * 2 }
            let typed = items.map { (v: Int) in v }
            func scale(f: Double, by k: Double) {}
            struct Point { let w: Int }
            enum Axis { case u }
            func identity<t>(value: t) -> t { value }
            if let m = optional {}
            switch value { case let .some(q): use(q); default: break }
            let `n` = 1
            """
        #expect(
            Self.positions(source) == [
                "1:5", "2:5", "3:5", "4:5", "5:25", "6:26", "7:12", "7:26", "8:20", "9:18", "10:15", "11:8", "12:31",
                "13:5",
            ]
        )
    }

    @Test func theMessagesAreTheGoRulesWords() {
        let single = Self.findings("let n = 1")
        #expect(single.map(\.messageId) == ["noSingleLetter"])
        #expect(
            single.first?.message
                == #"Single-letter identifier "n" is not descriptive enough. The name is read everywhere it is used and declared only once, so the saving is at the declaration and the cost is at every call site."#
        )
        let caught = Self.findings("do { try run() } catch let e { report(e) }")
        #expect(caught.map(\.messageId) == ["noAmbiguousE"])
        #expect(
            caught.first?.message
                == #"Variable named "e" is too ambiguous (appears to be an error). It is the one name that could be an error or an event, and a reader has to find the declaration to learn which. Use "error" or a more descriptive name."#
        )
    }

    /* Declared once, read twice: one finding, at the declaration. */
    @Test func referencesAreNotJudged() {
        let source = """
            let n = 1
            let doubled = n + n
            let field = thing.e
            run(e: 1, a: 2)
            """
        #expect(Self.positions(source) == ["1:5"])
    }

    @Test func coordinatesAreAllowed() {
        let source = """
            let x = 1
            let y = 2
            let z = 3
            let points = pairs.map { x, y in Point(x: x, y: y) }
            func move(x: Double, y: Double, z: Double) {}
            """
        #expect(Self.findings(source).isEmpty)
    }

    /* No comparator exemption (Kirk, 2026-11-06, call 3): `sort`, `sorted`, `min` and `max`'s `a` and `b` are reported at their declarations. */
    @Test func comparatorsAAndBAreFound() {
        let source = """
            let ascending = items.sorted { a, b in a < b }
            let labeled = items.sorted(by: { a, b in a < b })
            let typed = items.sorted { (a: Int, b: Int) -> Bool in a < b }
            items.sort { a, b in a > b }
            let smallest = items.min { a, b in a.size < b.size }
            let largest = items.max(by: { a, b in a.size < b.size })
            """
        #expect(
            Self.positions(source) == [
                "1:32", "1:35", "2:34", "2:37", "3:29", "3:37", "4:14", "4:17", "5:28", "5:31", "6:31", "6:34",
            ]
        )
    }

    /*
     No numeric-kernel exemption (Kirk, 2026-11-06, call 3): a function taking and returning only numbers gets no
     paper-notation letters. Barycentric `u` and `v`, the law of cosines' `a`, `b` and `c`, a ray's `p` and `t`, a
     closure's `d`, and a typed lerp are each reported, beside the exempt `x` and `y`.
     */
    @Test func numericFunctionsKeepNoLetters() {
        let source = """
            func barycentric(_ x: Float, _ y: Float, determinant: Float) -> SIMD2<Float> {
                let u = x / determinant
                let v = y / determinant
                return SIMD2(u, v)
            }
            func angle(a: Double, b: Double, c: Double) -> Double { acos((a * a + b * b - c * c) / (2 * a * b)) }
            func hit(direction: SIMD3<Float>, edge: SIMD3<Float>) -> Float? {
                let p = simd_cross(direction, edge)
                let t = simd_dot(edge, p)
                return t > 0 ? t : nil
            }
            func draw(into depth: inout [Float]) { depth = depth.map { d in d + 1 } }
            let lerp = { (a: Float, b: Float, t: Float) -> Float in a + (b - a) * t }
            """
        #expect(
            Self.positions(source) == [
                "2:9", "3:9", "6:12", "6:23", "6:34", "8:9", "9:9", "12:60", "13:15", "13:25", "13:35",
            ]
        )
    }

    /* Other closures taking `a` and `b`, found as every single letter is: a reduce, an unbased call, a nested closure and a filter. */
    @Test func aAndBInOtherClosuresAreFound() {
        let source = """
            let summed = items.reduce(0) { a, b in a + b }
            let unbased = sorted { a, b in a < b }
            let nested = items.sorted { first, second in first.tags.contains { a in a.isEmpty } }
            let filtered = items.filter { a in a.isValid }
            """
        #expect(Self.positions(source) == ["1:32", "1:35", "2:24", "2:27", "3:68", "4:31"])
    }

    /* `e` in a comparator gets its own message, and its partner is reported beside it. */
    @Test func eInAComparatorIsFoundWithItsPartner() {
        let findings = Self.findings("let ordered = items.sorted { e, b in e < b }")
        #expect(findings.map { "\($0.line):\($0.column)" } == ["1:30", "1:33"])
        #expect(findings.map(\.messageId) == ["noAmbiguousE", "noSingleLetter"])
    }

    /* An uppercase single letter is a type parameter, which the Go rule leaves alone; a non-ASCII letter is outside its `^[a-z]$`. */
    @Test func uppercaseAndNonAsciiLettersAreNotJudged() {
        let source = """
            func identity<T>(value: T) -> T { value }
            struct Pair<K, V> { let key: K }
            let π = 3.14159
            """
        #expect(Self.findings(source).isEmpty)
    }

    /* `_` is Swift's discard and empty label, not a name; the Go rule's `noUnderscore` does not carry over. */
    @Test func underscoreIsNotJudged() {
        let source = """
            let _ = compute()
            for _ in 0..<3 {}
            let mapped = items.map { _ in 0 }
            func handle(_ value: Int) {}
            let _event = 1
            """
        #expect(Self.findings(source).isEmpty)
    }

    /* The names the shared visitor already declines: labels that are not names, an override's single-name labels, and `if let` shorthand. */
    @Test func namesChosenElsewhereAreNotJudged() {
        let source = """
            class Pane: Base {
                override func place(a: Int) {}
                func move(t value: Int, to destination: Int) {}
                func update() {
                    if let n { use(n) }
                }
            }
            """
        #expect(Self.findings(source).isEmpty)
    }

    /* An override's parameter name after a label is ours, and judged. */
    @Test func anOverridesOwnParameterNameIsJudged() {
        #expect(Self.positions("class Pane: Base {\n    override func keyDown(with k: Key) {}\n}") == ["2:32"])
    }

    @Test(arguments: [
        ("do { try run() } catch let e { report(e) }", "appears to be an error", "error"),
        ("do { try run() } catch var e { report(e) }", "appears to be an error", "error"),
        ("do { try run() } catch let e as URLError { report(e) }", "appears to be an error", "error"),
        ("do { try run() } catch Failure.denied(let e) { report(e) }", "context unclear", "event"),
        ("let button = Button(onTap: { e in run() })", "appears to be an event", "event"),
        ("Toggle(isOn: value) { label() } onChange: { e in run() }", "appears to be an event", "event"),
        ("let reader = Reader(online: { e in run() })", "context unclear", "event"),
        ("let handleClick = { e in run() }", "appears to be an event", "event"),
        ("var onKeyDown: (Key) -> Void = { e in run() }", "appears to be an event", "event"),
        ("func handleKey(_ e: Key) {}", "appears to be an event", "event"),
        ("func handleAll() { items.forEach { e in run(e) } }", "appears to be an event", "event"),
        /* The Go pattern is case-insensitive, so `on` before any letter reads as a handler: `monitor` does. */
        ("func monitor(_ e: Key) {}", "appears to be an event", "event"),
        ("let start = { e in run() }", "context unclear", "event"),
    ])
    func eSaysWhatItInferred(source: String, hint: String, suggestion: String) throws {
        let finding = try #require(Self.findings(source).first)
        #expect(finding.messageId == "noAmbiguousE")
        #expect(finding.message.contains("(\(hint))"), "\(finding.message)")
        #expect(finding.message.contains("Use \"\(suggestion)\""), "\(finding.message)")
    }

    /* No fix: a rename without following references through scope would leave every other use pointing at a name that is gone. */
    @Test func noFixIsProposed() {
        let findings = Self.findings("do { try run() } catch let e { report(e) }\nlet n = 1")
        #expect(findings.count == 2)
        #expect(findings.allSatisfy { $0.fixes.isEmpty && $0.suggestions.isEmpty })
    }
}
