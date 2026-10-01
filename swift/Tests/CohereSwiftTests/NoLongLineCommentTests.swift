import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The comment rule rewrites source, so these assert the fixed text, not only that the rule fired: a correct
 detection with a wrong repair reads as a correct finding. Cases follow the TypeScript rule's own: four
 lines stay, five fold, a trailing comment never joins, a directive breaks the run, a star-slash withholds
 the fix. Swift's additions: `///` runs fold to `/** */`, and `MARK:` never folds.
 */
struct NoLongLineCommentTests {
    static func file(_ source: String) -> ParsedFile {
        ParsedFile(url: URL(fileURLWithPath: "/fixture/Subject.swift"), targetName: "Fixture", targetKind: "library", source: source, tree: Parser.parse(source: source), nodeCount: 0)
    }

    static func fixed(_ source: String) -> String {
        FileFixer(configuration: RuleConfiguration(severities: [:], note: ""), maximumPasses: 10).fix(file(source)).file.source
    }

    @Test func fourLinesStay() {
        let source = "// one\n// two\n// three\n// four\nlet value = 1\n"
        #expect(NoLongLineComment().findings(in: Self.file(source)).isEmpty)
    }

    @Test func fiveLinesFoldKeepingTextAndIndentation() {
        let source = """
            struct Example {
                // one
                // two
                //
                // four
                // five
                let value = 1
            }

            """
        let expected = """
            struct Example {
                /*
                 * one
                 * two
                 *
                 * four
                 * five
                 */
                let value = 1
            }

            """
        #expect(Self.fixed(source) == expected)
    }

    @Test func documentationRunsStayDocumentation() {
        let source = "/// one\n/// two\n/// three\n/// four\n/// five\nfunc subject() {}\n"
        #expect(Self.fixed(source) == "/**\n * one\n * two\n * three\n * four\n * five\n */\nfunc subject() {}\n")
    }

    @Test func aTrailingCommentNeverJoins() {
        let source = "let value = 1 // trailing\n// one\n// two\n// three\n// four\nlet other = 2\n"
        #expect(NoLongLineComment().findings(in: Self.file(source)).isEmpty)
    }

    @Test func aDirectiveBreaksTheRun() {
        let source = "// one\n// two\n// MARK: - Section\n// three\n// four\n// five\nlet value = 1\n"
        #expect(NoLongLineComment().findings(in: Self.file(source)).isEmpty)
    }

    @Test func aStarSlashWithholdsTheFix() {
        let source = "// one\n// a */ inside\n// three\n// four\n// five\nlet value = 1\n"
        let findings = NoLongLineComment().findings(in: Self.file(source))
        #expect(findings.count == 1)
        #expect(findings.first?.fixes.isEmpty == true)
        #expect(Self.fixed(source) == source)
    }

    /* Swift block comments nest, so a slash-star in the text would open a block that never closes. */
    @Test func aSlashStarWithholdsTheFix() {
        let source = "// one\n// serves /provider-proxy/* routes\n// three\n// four\n// five\nlet value = 1\n"
        let findings = NoLongLineComment().findings(in: Self.file(source))
        #expect(findings.count == 1)
        #expect(findings.first?.fixes.isEmpty == true)
    }

    @Test func aCommentInsideAStringIsNotAComment() {
        let source = "let text = \"\"\"\n// one\n// two\n// three\n// four\n// five\n\"\"\"\n"
        #expect(NoLongLineComment().findings(in: Self.file(source)).isEmpty)
    }

    @Test func differentColumnsAreDifferentRuns() {
        let source = "// one\n// two\n// three\n    // four\n    // five\nlet value = 1\n"
        #expect(NoLongLineComment().findings(in: Self.file(source)).isEmpty)
    }

    @Test func overlappingEditsRefuseTheLaterOne() {
        let result = FixApplier.apply([
            .init(start: 0, end: 5, text: "A"),
            .init(start: 3, end: 8, text: "B"),
            .init(start: 9, end: 10, text: "C"),
        ], to: "0123456789")
        #expect(result.text == "A5678C")
        #expect(result.applied == 2)
        #expect(result.refusedOverlapping == 1)
    }

    @Test func anOutOfRangeEditIsRefused() {
        let result = FixApplier.apply([.init(start: 5, end: 99, text: "x")], to: "short")
        #expect(result.text == "short")
        #expect(result.refusedInvalidRange == 1)
    }
}
