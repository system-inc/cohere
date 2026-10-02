import Foundation
import SwiftParser
import SwiftSyntax
import Testing

@testable import CohereSwift

/*
 Fixture pairs for cyclomatic-complexity. Most fixtures run against a small threshold so the source stays
 readable, as the Go rule's own fixtures do; one pair pins the house threshold of 20 at its edge. The
 scoring tests read every frame's score directly, so a construct counted twice or not at all fails even
 when it would not cross a threshold. The incumbent's documented examples are run at its default of 10.
 */
struct CyclomaticComplexityTests {
    static func findings(_ source: String, maximum: Int) -> [FindingRecord] {
        let url = URL(fileURLWithPath: "/fixture/Subject.swift")
        let file = ParsedFile(url: url, targetName: "Fixture", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
        return CyclomaticComplexity(maximum: maximum).findings(in: file)
    }

    static func positions(_ findings: [FindingRecord]) -> [String] {
        findings.map { "\($0.line):\($0.column)" }
    }

    /* Every frame's label and score, in the order the frames close. */
    static func scores(_ source: String) -> [String] {
        let visitor = CyclomaticComplexity.Visitor(viewMode: .sourceAccurate)
        visitor.walk(Parser.parse(source: source))
        return visitor.scored.map { "\($0.label)=\($0.complexity)" }
    }

    /* The Go rule's own pair: `if (x) {} if (y) {}` is 3, over a threshold of 2. */
    @Test func overTheThresholdIsFoundAtTheName() {
        let source = """
            func decide() {
                if first {}
                if second {}
            }
            """
        let found = Self.findings(source, maximum: 2)
        #expect(Self.positions(found) == ["1:6"])
        #expect(found.first?.message.hasPrefix("Function 'decide' has a complexity of 3. Maximum allowed is 2.") == true)
    }

    @Test func atTheThresholdIsNot() {
        let source = """
            func decide() {
                if first {}
            }
            """
        #expect(Self.findings(source, maximum: 2).isEmpty)
    }

    /* The house threshold: 19 decisions score 20 and pass, 20 decisions score 21 and are found. */
    @Test func houseThresholdIsTwentyAtItsEdge() {
        let twenty = "func atLimit() {\n" + String(repeating: "    if condition {}\n", count: 19) + "}\n"
        let twentyOne = "func overLimit() {\n" + String(repeating: "    if condition {}\n", count: 20) + "}\n"
        let file = { (source: String) in
            ParsedFile(url: URL(fileURLWithPath: "/fixture/Subject.swift"), targetName: "Fixture", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
        }
        #expect(CyclomaticComplexity().findings(in: file(twenty)).isEmpty)
        let found = CyclomaticComplexity().findings(in: file(twentyOne))
        #expect(Self.positions(found) == ["1:6"])
        #expect(found.first?.message.hasPrefix("Function 'overLimit' has a complexity of 21. Maximum allowed is 20.") == true)
    }

    @Test func statementsCountOneEach() {
        let source = """
            func plain() {}
            func branches() {
                if a {} else if b {} else {}
                guard c else { return }
                while d {}
                repeat {} while e
                for item in items {}
                do { try work() } catch Failure.first { } catch { }
            }
            """
        #expect(Self.scores(source) == ["Function 'plain'=1", "Function 'branches'=9"])
    }

    @Test func operatorsCountOneEach() {
        let source = """
            func logic() {
                let both = a && b
                let either = a || b || c
                let fallback = optional ?? other ?? last
                let picked = flag ? one : two
            }
            """
        #expect(Self.scores(source) == ["Function 'logic'=7"])
    }

    /* The Swift comma in a condition list is the JavaScript `&&`, and a `where` is an `if` written in place. */
    @Test func conditionCommasAndWhereClausesCount() {
        let source = """
            func conditions() {
                if let first = a, let second = b, second > first {}
                guard let value = c, value > 0 else { return }
                while let next = iterator.next(), next.isValid {}
                for item in items where item.isReady {}
                do { try work() } catch let failure as Failure where failure.isFatal { }
            }
            """
        #expect(Self.scores(source) == ["Function 'conditions'=12"])
    }

    /* Each `case` item counts, as JavaScript's stacked `case A: case B:` does; `default` never; `fallthrough` takes nothing away. */
    @Test func switchCountsCaseItemsNotDefault() {
        let source = """
            func pick(code: Int) {
                switch code {
                case 0: fallthrough
                case 1, 2: break
                case let other where other > 9: break
                @unknown default: break
                }
            }
            """
        #expect(Self.scores(source) == ["Function 'pick'=6"])
    }

    @Test func optionalChainingCountsEachQuestionMark() {
        let source = """
            func chains() {
                let one = a?.b
                let two = a?.b?.c
                let three = a?.b.c
                let four = handler?()
                let five = list?[0]
                a?.b = value
            }
            """
        #expect(Self.scores(source) == ["Function 'chains'=8"])
    }

    /* A pattern's `x?` is a match, not a chain, and `try?`, `as?` and a key path's `?` have no JavaScript construct to map from. */
    @Test func questionMarksThatAreNotChainsDoNotCount() {
        let source = """
            func patterns() {
                switch optional {
                case let value?: use(value)
                case nil: break
                }
                let tried = try? load()
                let cast = item as? NSView
                let path = \\Model.child?.name
            }
            """
        #expect(Self.scores(source) == ["Function 'patterns'=3"])
    }

    @Test func parameterDefaultsCount() {
        let source = """
            func configure(width: Int = 1, height: Int, depth: Int = 3) {}
            struct Grid {
                init(size: Int = 4) {}
                subscript(index: Int, wrap: Bool = false) -> Int { 0 }
            }
            """
        #expect(Self.scores(source) == ["Function 'configure'=3", "Initializer=2", "Subscript=2"])
    }

    /* A nested function or closure is scored on its own and adds nothing to the body around it, as the Go rule does with a nested function. */
    @Test func nestedBodiesAreScoredSeparately() {
        let source = """
            func outer() {
                if a {}
                let callback = { if b {}; if c {} }
                func inner() { if d {}; if e {}; if f {} }
            }
            """
        #expect(Self.scores(source) == ["Closure=3", "Function 'inner'=4", "Function 'outer'=2"])
        #expect(Self.positions(Self.findings(source, maximum: 2)) == ["3:20", "4:10"])
    }

    @Test func everyKindOfBodyIsFoundAtItsNameOrKeyword() {
        let source = """
            final class Store {
                static func make() { if a {}; if b {} }
                var title: String { if a { return "" }; if b { return "" }; return "" }
                var count: Int {
                    get { if a {}; if b {}; return 0 }
                    set { if a {}; if b {} }
                }
                var level = 0 {
                    didSet { if a {}; if b {} }
                }
                let mode = a ? (b ? 1 : 2) : 3
                init() { if a {}; if b {} }
                deinit { if a {}; if b {} }
                subscript(index: Int) -> Int { if a { return 0 }; if b { return 1 }; return 2 }
            }
            """
        let found = Self.findings(source, maximum: 2)
        #expect(Self.positions(found) == ["2:17", "3:9", "5:9", "6:9", "9:9", "11:9", "12:5", "13:5", "14:5"])
        #expect(found.map { $0.message.components(separatedBy: " has a").first ?? "" } == [
            "Static method 'make'",
            "Getter 'title'",
            "Getter 'count'",
            "Setter 'count'",
            "Accessor didSet of 'level'",
            "Property initializer 'mode'",
            "Initializer",
            "Deinitializer",
            "Subscript",
        ])
    }

    /* Top-level code is the program, which the Go rule never reports, and a global's initializer belongs to it; a local property belongs to its function. */
    @Test func topLevelCodeAndLocalPropertiesAreNotFrames() {
        let source = """
            if a {}; if b {}; if c {}
            let global = a ? (b ? 1 : 2) : (c ? 3 : 4)
            func holder() {
                let local = a ? 1 : 2
            }
            """
        #expect(Self.scores(source) == ["Function 'holder'=2"])
    }

    /* Only one `#if` clause is compiled, so the busiest one counts; the `#if` condition is not a runtime branch. */
    @Test func compilerConditionalsCountTheBusiestClause() {
        let source = """
            func platform() {
                if shared {}
                #if os(macOS) && DEBUG
                if a {}; if b {}; if c {}
                #elseif os(iOS) || os(tvOS)
                if d {}
                #else
                #endif
            }
            """
        #expect(Self.scores(source) == ["Function 'platform'=5"])
    }

    @Test func compilerConditionalsNestAndStillOpenFrames() {
        let source = """
            struct Holder {
                #if DEBUG
                func debugOnly() {
                    #if os(macOS)
                    if a {}
                    #if TRACE
                    if b {}; if c {}
                    #endif
                    #else
                    if d {}
                    #endif
                }
                #endif
            }
            """
        #expect(Self.scores(source) == ["Method 'debugOnly'=4"])
    }

    /* SwiftLint's documented non-triggering examples, at its default warning of 10: all pass here too. */
    @Test func incumbentPassingExamplesPass() {
        let nested = """
            func f1() {
                if true {
                    for _ in 1..5 { }
                }
                if false { }
            }
            """
        let fallthroughSwitch = """
            func f(code: Int) -> Int {
                switch code {
                case 0: fallthrough
                case 1: return 1
                case 2: return 1
                case 3: return 1
                case 4: return 1
                case 5: return 1
                case 6: return 1
                case 7: return 1
                case 8: return 1
                default: return 1
                }
            }
            """
        let nestedFunction = """
            func f1() {
                if true {}; if true {}; if true {}; if true {}; if true {}; if true {}
                func f2() {
                    if true {}; if true {}; if true {}; if true {}; if true {}
                }
            }
            """
        #expect(Self.findings(nested, maximum: 10).isEmpty)
        #expect(Self.findings(fallthroughSwitch, maximum: 10).isEmpty)
        #expect(Self.findings(nestedFunction, maximum: 10).isEmpty)
        #expect(Self.scores(fallthroughSwitch) == ["Function 'f'=10"])
    }

    static func incumbentTriggeringExample(firstArm: String) -> String {
        """
        func f1() {
            if true {
                if true {
                    if false {}
                }
            }
            if false {}
            let i = 0
            switch i {
                case 1: \(firstArm)
                case 2: break
                case 3: break
                case 4: break
                default: break
            }
            for _ in 1...5 {
                guard true else {
                    return
                }
            }
        }
        """
    }

    /*
     SwiftLint's documented triggering example scores 11 there and 8 here: its switch is a table of `break`
     arms, which counts once. A deliberate difference, explained on the rule. Found at a threshold of 7.
     */
    @Test func incumbentTriggeringExampleCountsItsTableOnce() {
        let source = Self.incumbentTriggeringExample(firstArm: "break")
        #expect(Self.scores(source) == ["Function 'f1'=8"])
        #expect(Self.findings(source, maximum: 10).isEmpty)
        #expect(Self.positions(Self.findings(source, maximum: 7)) == ["1:6"])
    }

    /* With one arm that is logic rather than a table entry, the same example scores 11 here as it does there, by different routes: it counts `default`, this counts the function itself. */
    @Test func incumbentTriggeringExampleWithLogicIsFound() {
        let source = Self.incumbentTriggeringExample(firstArm: "fallthrough")
        let found = Self.findings(source, maximum: 10)
        #expect(Self.positions(found) == ["1:6"])
        #expect(found.first?.message.hasPrefix("Function 'f1' has a complexity of 11. Maximum allowed is 10.") == true)
    }

    /* Enum cases and literals mapped to one plain statement each: the Swift lookup the message asks for, counted once. */
    @Test func lookupTableSwitchesCountOnce() {
        let source = """
            func symbol(for weight: Weight) -> String {
                switch weight {
                case .light, .regular, .medium: "thin"
                case Weight.bold: "bold"
                @unknown default: "other"
                }
            }
            func handle(_ key: String) throws {
                switch key {
                case "up": moveUp()
                case "down": return
                case "quit": throw Stop.requested
                default: break
                }
            }
            func scale(_ code: Int) -> Double {
                switch code {
                case 1: return 0.5
                case 2: return flag ? 1 : 2
                case 3: return fallback ?? 3
                default: return 4
                }
            }
            """
        #expect(Self.scores(source) == ["Function 'symbol'=2", "Function 'handle'=2", "Function 'scale'=4"])
    }

    /* Each of these is logic, not a table, and counts item by item. */
    @Test func switchesThatAreNotTablesCountEveryItem() {
        let source = """
            func binding() { switch event { case .move(let distance): go(distance); case .stop: halt() } }
            func guarded() { switch code { case .a where ready: go(); case .b: halt() } }
            func ranged() { switch code { case 0..<10: low(); case 10: ten() } }
            func named() { switch code { case limit: stop(); case .b: go() } }
            func twoStatements() { switch code { case .a: go(); stop(); case .b: halt() } }
            func nested() { switch code { case .a: if ready { go() }; case .b: halt() } }
            func chained() { switch code { case .a: fallthrough; case .b: halt() } }
            func tuple() { switch (left, right) { case (.a, .b): go(); case (.b, .a): halt() } }
            func conditional() {
                switch code {
                case .a: go()
                #if DEBUG
                case .b: trace()
                #endif
                }
            }
            """
        #expect(Self.scores(source) == [
            "Function 'binding'=3",
            "Function 'guarded'=4",
            "Function 'ranged'=3",
            "Function 'named'=3",
            "Function 'twoStatements'=3",
            "Function 'nested'=4",
            "Function 'chained'=3",
            "Function 'tuple'=3",
            "Function 'conditional'=3",
        ])
    }

    @Test func messageSaysWhatToDoWithoutEmDashes() {
        let source = "func decide() { if a {}; if b {} }"
        let message = Self.findings(source, maximum: 1).first?.message ?? ""
        #expect(message.contains("Split the decision out into named functions"))
        #expect(!message.contains("\u{2014}"))
    }
}
