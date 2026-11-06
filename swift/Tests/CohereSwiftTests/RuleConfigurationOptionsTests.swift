import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 A rule's options in CohereSettings.json: read by the rule that reads them, and refused by name otherwise
 (#4x9h976). `file-length`'s `max` was once dropped without a word, so the first fixture fails when the option
 is ignored: a file over a small `max` is flagged with it and clean without it.
 */
struct RuleConfigurationOptionsTests {
    static let path = "/project/CohereSettings.json"

    static func configuration(_ rules: String) throws -> RuleConfiguration {
        try RuleConfiguration.parse(Data(#"{"swift":{"rules":{\#(rules)}}}"#.utf8), path: path)
    }

    /* What the registry's file-length, built from this configuration, finds in a file of `lines` lines. */
    static func fileLengthFindings(lines: Int, configuration: RuleConfiguration) -> [FindingRecord] {
        let source = FileLengthTests.source(lines: lines)
        let file = ParsedFile(
            url: URL(fileURLWithPath: "/fixture/Pane.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
        let rules = RuleRegistry.fileRules(
            vocabulary: AbbreviationVocabulary(),
            shoutingVocabulary: ShoutingVocabulary(),
            brandVocabulary: BrandVocabulary(),
            configuration: configuration,
        )
        return rules.filter { $0.name == FileLength().name }.flatMap { $0.findings(in: file) }
    }

    /* The reason a configuration is refused for, or nil when it is read. */
    static func refusal(_ rules: String) -> String? {
        do {
            _ = try configuration(rules)
            return nil
        }
        catch let failure as RuleConfiguration.ReadFailure {
            #expect(failure.path == path)
            return failure.reason
        }
        catch {
            return "refused, but not as a settings failure: \(error)"
        }
    }

    @Test func aStricterMaximumIsTheOneJudgedBy() throws {
        let configured = try Self.configuration(#""cohere-swift/file-length":["error",{"max":10}]"#)
        #expect(configured.options == ["cohere-swift/file-length": ["max": 10]])
        let findings = Self.fileLengthFindings(lines: 11, configuration: configured)
        #expect(findings.count == 1)
        #expect(findings.first?.message.hasPrefix("This file is 11 lines, over the 10 a file may hold.") == true)
        #expect(Self.fileLengthFindings(lines: 10, configuration: configured).isEmpty)

        let houseOnly = try Self.configuration(#""cohere-swift/file-length":"error""#)
        #expect(Self.fileLengthFindings(lines: 11, configuration: houseOnly).isEmpty, "the house limit is 2,000")
    }

    /* JSON's 1 also casts to a boolean on Darwin, so the smallest maximum is the case that tells them apart. */
    @Test func theSmallestMaximumIsANumberNotABoolean() throws {
        let configured = try Self.configuration(#""cohere-swift/file-length":["error",{"max":1}]"#)
        #expect(configured.options["cohere-swift/file-length"]?["max"] == 1)
        #expect(Self.refusal(#""cohere-swift/file-length":["error",{"max":true}]"#)?.contains("whole number") == true)
    }

    @Test func aLooserMaximumIsRefused() {
        #expect(
            Self.refusal(#""cohere-swift/file-length":["error",{"max":2001}]"#)
                == "cohere-swift/file-length's max is 2001, above the house's 2000; an option may only make a rule stricter"
        )
        #expect(Self.refusal(#""cohere-swift/file-length":["error",{"max":2000}]"#) == nil)
    }

    @Test func anOptionNoRuleReadsIsRefusedByName() {
        #expect(
            Self.refusal(#""cohere-swift/file-length":["error",{"skipComments":true}]"#)
                == "cohere-swift/file-length is given the option skipComments, which it does not read (it reads max); remove it"
        )
        #expect(
            Self.refusal(#""cohere-swift/consistency-no-print":["error",{"allow":["debug"]}]"#)
                == "cohere-swift/consistency-no-print is given options (allow), and it reads none; remove them"
        )
        #expect(Self.refusal(#""cohere-swift/file-length":["error",{"max":"ten"}]"#)?.contains("whole number") == true)
        #expect(Self.refusal(#""cohere-swift/file-length":["error",{"max":10},{}]"#)?.contains("at most one") == true)
        #expect(Self.refusal(#""cohere-swift/file-length":["error",10]"#)?.contains("at most one") == true)
    }
}
