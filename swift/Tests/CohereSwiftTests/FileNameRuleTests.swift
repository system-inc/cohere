import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 Fixture pairs for consistency-no-utils-file-name and consistency-no-abbreviated-file-name, ported from the Go rules'
 own cases (`internal/file_name_words`, `consistency_no_utils_file_name_test.go`). Each path's package root is the
 directory named `project` on it, as the pipeline would set it. The silent cases are the near misses and the names
 nobody inside the package chose.
 */
struct FileNameRuleTests {
    static let source = "/* A header comment, so the finding is seen to sit before it. */\nlet value = 1\n"

    static func file(_ path: String) -> ParsedFile {
        ParsedFile(
            url: URL(fileURLWithPath: path),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
            packageRoot: ConsistencyNoUtilsFolderTests.packageRoot(of: path),
        )
    }

    static func described(_ findings: [FindingRecord]) -> [String] {
        findings.map { "\($0.line):\($0.column)-\($0.endLine ?? 0):\($0.endColumn ?? 0) \($0.messageId) \($0.message)" }
    }

    static func utilsFindings(_ path: String) -> [String] {
        described(ConsistencyNoUtilsFileName().findings(in: file(path)))
    }

    static func abbreviationFindings(_ path: String) throws -> [String] {
        described(
            ConsistencyNoAbbreviatedFileName(vocabulary: try AbbreviationVocabulary.compiledIn()).findings(
                in: file(path)
            )
        )
    }

    static func utilsMessage(_ fileName: String, _ word: String, _ replacement: String) -> String {
        "1:1-1:1 utilsFileName File name \"\(fileName)\" holds \"\(word)\", which names nothing in particular, so the "
            + "file collects whatever has no other home. Name the file for what it holds, or at the least spell the "
            + "word \"\(replacement)\", as the house spells it in a folder's name."
    }

    /* The sentence both abbreviation messages end on, the `reasoning` phrase of the rule's message file. */
    static let reasoning =
        "A file's name is read on every path and every import, so the letters saved when it was named are paid back by "
        + "every reader who has to expand the word themselves and hope they expanded it the way the author meant."

    @Test(arguments: [
        ("/project/Sources/Fixture/Utils.swift", FileNameRuleTests.utilsMessage("Utils.swift", "Utils", "Utilities")),
        (
            "/project/Sources/Fixture/ViewUtil.swift",
            FileNameRuleTests.utilsMessage("ViewUtil.swift", "Util", "Utilities"),
        ),
        (
            "/project/Sources/Fixture/String+Utils.swift",
            FileNameRuleTests.utilsMessage("String+Utils.swift", "Utils", "Utilities"),
        ),
        (
            "/project/Sources/Fixture/_utils.swift",
            FileNameRuleTests.utilsMessage("_utils.swift", "utils", "utilities"),
        ),
        ("/project/Sources/Fixture/UTILS.swift", FileNameRuleTests.utilsMessage("UTILS.swift", "UTILS", "UTILITIES")),
        /* A nested type's dot is a word boundary like any other. */
        (
            "/project/Sources/Fixture/Outer.Util.swift",
            FileNameRuleTests.utilsMessage("Outer.Util.swift", "Util", "Utilities"),
        ),
    ])
    func utilsWordsAreFound(path: String, expected: String) {
        #expect(Self.utilsFindings(path) == [expected])
    }

    @Test(arguments: [
        "/project/Sources/Fixture/Utilities.swift",
        "/project/Sources/Fixture/StringFormatting.swift",
        /* One run-together word is not the word, as a folder named myutils passes. */
        "/project/Sources/Fixture/Myutils.swift",
        "/project/Sources/Fixture/Utility.swift",
        /* A folder is the folder rule's. */
        "/project/Sources/Fixture/Utils/Thing.swift",
        /* Named for the rule that bans the word, which is what it holds. */
        "/project/Sources/Fixture/ConsistencyNoUtilsFolder.swift",
        /* Names nobody inside the package chose. */
        "/project/Sources/Fixture/ViewUtil.generated.swift",
        "/project/Sources/Fixture/testdata/Utils.swift",
        "/project/third_party/Library/Utils.swift",
        "/project/vendor/Utils.swift",
    ])
    func utilsNearMissesAreNot(path: String) {
        #expect(Self.utilsFindings(path).isEmpty)
    }

    @Test(arguments: [
        (
            "/project/Sources/Fixture/Cfg.swift",
            "abbreviatedFileName File name \"Cfg.swift\" abbreviates \"cfg\". Spell it out as \"configuration\".",
        ),
        (
            "/project/Sources/Fixture/MsgPrinter.swift",
            "abbreviatedFileName File name \"MsgPrinter.swift\" abbreviates \"msg\". Spell it out as \"message\".",
        ),
        (
            "/project/Sources/Fixture/String+Msg.swift",
            "abbreviatedFileName File name \"String+Msg.swift\" abbreviates \"msg\". Spell it out as \"message\".",
        ),
        (
            "/project/Sources/Fixture/PosLabel.swift",
            "abbreviatedFileName File name \"PosLabel.swift\" abbreviates \"pos\". Spell it out as \"position\" or "
                + "\"positive\", whichever it means.",
        ),
        (
            "/project/Sources/Fixture/Fn.swift",
            "abbreviatedFileNameAdvice File name \"Fn.swift\" abbreviates \"fn\". Use \"callback\", \"handler\", "
                + "\"factory\", or a more descriptive name.",
        ),
    ])
    func abbreviationsAreFound(path: String, expected: String) throws {
        #expect(try Self.abbreviationFindings(path) == ["1:1-1:1 \(expected) \(Self.reasoning)"])
    }

    @Test(arguments: [
        "/project/Sources/Fixture/MessagePrinter.swift",
        "/project/Sources/Fixture/Configuration.swift",
        /* An initialism in capitals, here Switzerland's code, is no abbreviation of the vocabulary's. */
        "/project/Sources/Fixture/FlagCH.swift",
        "/project/Package.swift",
        "/project/Package@swift-6.0.swift",
        "/project/Sources/Fixture/main.swift",
        "/project/Sources/Fixture/MsgPrinter.generated.swift",
        "/project/Sources/Fixture/testdata/Cfg.swift",
        "/project/third_party/Cfg.swift",
    ])
    func abbreviationNearMissesAreNot(path: String) throws {
        #expect(try Self.abbreviationFindings(path).isEmpty)
    }

    /* The words `FileNameWords` reads off a name, Go's `Words` for Swift, case and punctuation separating them. */
    @Test(arguments: [
        ("ViewUtil.swift", ["View", "Util"]),
        ("Outer.Inner+Purpose.swift", ["Outer", "Inner", "Purpose"]),
        ("URLSession+Cfg.swift", ["URL", "Session", "Cfg"]),
        ("View_Util2.swift", ["View", "Util"]),
    ])
    func wordsOfAName(fileName: String, words: [String]) {
        #expect(FileNameWords.words(ofName: fileName) == words)
    }

    /* A registered rule's name is set aside, and the words beside it are judged, as Go's `withoutRuleName` does. */
    @Test func aRegisteredRulesNameIsSetAside() {
        let names = FileNameWords.ruleNames(["max-lines", "cohere-swift/force-unwrapping", "no-var"])
        #expect(FileNameWords.withoutRuleName(["Max", "Lines", "Rule"], ruleNames: names) == ["Rule"])
        #expect(FileNameWords.withoutRuleName(["Force", "Unwrapping"], ruleNames: names) == [])
        #expect(FileNameWords.withoutRuleName(["no", "var", "cfg"], ruleNames: names) == ["cfg"])
        #expect(FileNameWords.withoutRuleName(["Msg", "Printer"], ruleNames: names) == ["Msg", "Printer"])
        let oneWord = FileNameWords.ruleNames(["boundaries/dependencies"])
        #expect(FileNameWords.withoutRuleName(["Dependencies"], ruleNames: oneWord) == ["Dependencies"])
        #expect(
            FileNameWords.withoutRuleName(["Dependencies", "Ref", "Current"], ruleNames: oneWord)
                == ["Dependencies", "Ref", "Current"]
        )
    }

    /* The rule reads the registry the engine holds: its own name is among the names it sets aside. */
    @Test func theRuleReadsTheEnginesRegistry() {
        #expect(ConsistencyNoAbbreviatedFileName.registeredRuleNames.contains("consistency no abbreviated file name"))
        #expect(ConsistencyNoAbbreviatedFileName.registeredRuleNames.contains("force unwrapping"))
    }
}
