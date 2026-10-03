import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 Fixture pairs for max-file-lines: the limit itself and one past it, the way a final newline is counted,
 and the one finding's place and wording.
 */
struct MaxFileLinesTests {
    static func findings(_ source: String) -> [FindingRecord] {
        let file = ParsedFile(
            url: URL(fileURLWithPath: "/fixture/Pane.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0
        )
        let rule = MaxFileLines()
        guard rule.applies(to: file) else { return [] }
        return rule.findings(in: file)
    }

    /* `count` lines of code, each ending in a newline, as an editor saves them. */
    static func source(lines count: Int) -> String {
        (0..<count).map { "let value\($0) = \($0)\n" }.joined()
    }

    @Test func aFileAtTheLimitPasses() {
        #expect(Self.findings(Self.source(lines: MaxFileLines.maximumLines)).isEmpty)
    }

    @Test func aFileOnePastTheLimitIsReportedOnceAtTheTop() throws {
        let findings = Self.findings(Self.source(lines: MaxFileLines.maximumLines + 1))
        #expect(findings.count == 1)
        let finding = try #require(findings.first)
        #expect("\(finding.line):\(finding.column)-\(finding.endLine ?? 0):\(finding.endColumn ?? 0)" == "1:1-1:1")
        #expect(finding.messageId == "tooManyLines")
        #expect(finding.message.hasPrefix("This file is 1001 lines, over the 1000 a file may hold."))
    }

    /* The limit is 1,000, the number the measurement chose; a change to it should be a decision, not a drift. */
    @Test func theLimitIsAThousand() {
        #expect(MaxFileLines.maximumLines == 1_000)
    }

    /* A final newline ends the last line; it does not start another. Without one, the last line still counts. */
    @Test func linesAreCountedAsAnEditorShowsThem() {
        #expect(MaxFileLines.lineCount(of: "") == 0)
        #expect(MaxFileLines.lineCount(of: "let a = 1") == 1)
        #expect(MaxFileLines.lineCount(of: "let a = 1\n") == 1)
        #expect(MaxFileLines.lineCount(of: "let a = 1\n\nlet b = 2") == 3)
        #expect(MaxFileLines.lineCount(of: "let a = 1\r\nlet b = 2\r\n") == 2)
    }

    /* Comments and blank lines are part of what a reader holds, so they count. */
    @Test func commentsAndBlankLinesCount() {
        let source = "/*\n" + String(repeating: " A line of explanation.\n\n", count: MaxFileLines.maximumLines / 2) + "*/\n"
        #expect(Self.findings(source).count == 1)
    }

    @Test func theRuleIsRegistered() {
        #expect(RuleRegistry.allNames.contains(MaxFileLines().name))
    }
}
