import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/* One finding per label, at its name, whatever statement it labels and however many jumps name it. */
struct ConsistencyNoLabelTests {
    static func findings(_ source: String) -> [FindingRecord] {
        let file = ParsedFile(
            url: URL(fileURLWithPath: "/fixture/Subject.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
        return ConsistencyNoLabel().findings(in: file)
    }

    @Test func everyLabeledStatementIsReportedOnceAtItsName() throws {
        let source = """
            func scan(_ grid: [[Int]], _ value: Int) {
                outer: for row in grid {
                    inner: while true {
                        if row.isEmpty { continue outer }
                        break inner
                    }
                }
                check: if value > 0 {
                    break check
                }
                choose: switch value {
                    default: break choose
                }
                attempt: do {
                    break attempt
                }
                repeating: repeat {} while false
            }
            """
        let found = Self.findings(source)
        #expect(
            found.map { "\($0.line):\($0.column)-\($0.endColumn ?? 0)" } == [
                "2:5-10", "3:9-14", "8:5-10", "11:5-11", "14:5-12", "17:5-14",
            ]
        )
        let first = try #require(found.first)
        #expect(first.messageId == "label")
        #expect(first.message.hasPrefix("`outer` is a label, and a jump to it lands where a reader is not looking"))
    }

    /* A colon that labels nothing, in a dictionary, a ternary, a type annotation, an argument label or a `case`, is not a label. */
    @Test func aColonThatLabelsNothingIsNotReported() {
        let source = """
            let table: [String: Int] = ["a": 1]
            func pick(flag: Bool, from values: [Int]) -> Int {
                switch values.count {
                    case 0: return flag ? 1 : 2
                    default: return values.max(by: <) ?? 0
                }
            }
            """
        #expect(Self.findings(source).isEmpty)
    }
}
