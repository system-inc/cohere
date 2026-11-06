import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 The naming rules. The vocabulary tests are the Go rule's own vectors (`abbreviation_vocabulary_test.go`),
 so the two engines are held to the same answers on the same file.
 */
struct NamingRuleTests {
    static func vocabulary() throws -> AbbreviationVocabulary {
        try AbbreviationVocabulary.compiledIn()
    }

    static func file(_ source: String, name: String = "Subject.swift") -> ParsedFile {
        ParsedFile(
            url: URL(fileURLWithPath: "/fixture/\(name)"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
    }

    /* `name:line` for each finding, so an expectation reads as what was flagged and where. */
    static func flagged(_ rule: some FileRule, _ source: String, name: String = "Subject.swift") -> [String] {
        rule.findings(in: file(source, name: name)).map { finding in
            /* swift-format quotes the name last in single quotes ("remove the leading '_' from the name '_cache'"); the vocabulary quotes it first in double quotes. */
            let singleQuoted = finding.message.split(separator: "'", omittingEmptySubsequences: false)
            let quoted =
                singleQuoted.count >= 3
                ? String(singleQuoted[singleQuoted.count - 2])
                : finding.message.split(separator: "\"").dropFirst().first.map(String.init) ?? ""
            return "\(quoted):\(finding.line)"
        }
    }

    @Test func theSharedFileCarriesEveryFormInOrder() throws {
        let vocabulary = try Self.vocabulary()
        #expect(vocabulary.earlyPrefixes.map(\.abbreviation) == ["ctx", "db", "tx", "opts", "cur", "pct", "prev"])
        #expect(
            vocabulary.segments.map(\.abbreviation) == [
                "cwd", "dir", "env", "cli", "len", "seq", "db", "tx", "vars", "var",
                "buf", "tmp", "pkg", "src", "dst", "conn", "cmd", "cfg", "pos", "wg", "mu", "ch", "sb",
            ]
        )
        #expect(vocabulary.suffixes.map(\.abbreviation).prefix(5) == ["prop", "props", "param", "params", "ms"])
        #expect(vocabulary.allowedNames == ["URLSearchParams"])
        #expect(vocabulary.allowedSegments == ["InnoDb"])
    }

    /* The Go test's count: every form of every entry judged once, 131 in all. */
    @Test func everyFormIsJudged() throws {
        let vocabulary = try Self.vocabulary()
        var judged = 0
        for word in vocabulary.wholeByName.keys {
            #expect(
                vocabulary.find(word)?.form == "whole" && vocabulary.find(word)?.messageId == "abbreviatedIdentifier",
                "whole \(word)",
            )
            judged += 1
        }
        for entry in vocabulary.earlyPrefixes + vocabulary.latePrefixes {
            let name = entry.abbreviation + "Widget"
            #expect(
                vocabulary.find(name)?.form == "prefix" && vocabulary.find(name)?.messageId == "abbreviatedIdentifier",
                "prefix \(name)",
            )
            judged += 1
        }
        for entry in vocabulary.suffixes {
            let name = "widget" + AbbreviationVocabulary.capitalized(entry.abbreviation)
            #expect(vocabulary.find(name)?.form == "suffix", "suffix \(name)")
            judged += 1
        }
        for entry in vocabulary.segments {
            let name = "widget" + AbbreviationVocabulary.capitalized(entry.abbreviation) + "Name"
            #expect(
                vocabulary.find(name)?.form == "segment" && vocabulary.find(name)?.abbreviation == entry.abbreviation,
                "segment \(name)",
            )
            judged += 1
        }
        #expect(judged == 131)
    }

    @Test(arguments: [
        ("themeVarsVar", "abbreviatedWordSegment", #"Use "themeVariablesVar"."#),
        ("timeoutMsRef", "millisecondSuffix", #"Use "timeoutInMillisecondsRef""#),
        ("dbVal", "abbreviatedIdentifier", #"Use "databaseVal"."#),
    ])
    func orderIsObservableWhereItMatters(name: String, messageId: String, advice: String) throws {
        let finding = try #require(try Self.vocabulary().find(name))
        #expect(finding.messageId == messageId)
        #expect(finding.message.contains(advice), "\(finding.message)")
    }

    /* The sentence every message but the millisecond one ends on, so a rendered text is compared whole. */
    static let reasoning =
        "A name is written once and read everywhere, so the letters saved at the declaration are paid back at every call site by a reader who has to expand the abbreviation themselves and hope they expanded it the way the author meant."

    /* A word with one expansion renders every form byte for byte as it did when an entry held a single expansion: Go's `TestFindRendersAWordWithOneExpansionAsBefore`. */
    @Test(arguments: [
        ("idx", #"Identifier "idx" should not be abbreviated. Use "index" or a more descriptive name."#),
        ("db", #"Identifier "db" should not be abbreviated. Use "database"."#),
        ("idxStart", #"Identifier "idxStart" should not be abbreviated. Use "indexStart"."#),
        ("rowProps", #"Identifier "rowProps" should not end with "Props". Use "rowProperties"."#),
        ("outputDirPath", #"Identifier "outputDirPath" abbreviates "Dir". Use "outputDirectoryPath"."#),
        ("themeVarsVar", #"Identifier "themeVarsVar" abbreviates "Vars". Use "themeVariablesVar"."#),
    ])
    func aWordWithOneExpansionRendersAsBefore(name: String, want: String) throws {
        let finding = try #require(try Self.vocabulary().find(name))
        #expect(finding.message == want + " " + Self.reasoning)
    }

    /*
     A word with more than one meaning names them all and asks for whichever it is, never one guess: Go's
     `TestFindNamesEveryMeaningOfAWordWithMore`, `resultPos` beside `resultNeg` among them, where it means positive.
     */
    @Test(arguments: [
        (
            "policy", "pos",
            #"Identifier "pos" should not be abbreviated. Use "position" or "positive", whichever it means, or a more descriptive name."#,
        ),
        (
            "policy", "posLabel",
            #"Identifier "posLabel" should not be abbreviated. Use "positionLabel" or "positiveLabel", whichever it means."#,
        ),
        (
            "policy", "resultPos",
            #"Identifier "resultPos" abbreviates "Pos". Use "resultPosition" or "resultPositive", whichever it means."#,
        ),
        (
            "listed", "neg",
            #"Identifier "neg" should not be abbreviated. Use "negative" or "negation", whichever it means."#,
        ),
        (
            "listed", "resultNeg",
            #"Identifier "resultNeg" should not end with "Neg". Use "resultNegative" or "resultNegation", whichever it means."#,
        ),
        (
            "listed", "readRelPath",
            #"Identifier "readRelPath" abbreviates "Rel". Use "readRelativePath", "readRelationPath" or "readReleasePath", whichever it means."#,
        ),
    ])
    func aWordWithMoreThanOneMeaningNamesThemAll(vocabularyName: String, name: String, want: String) throws {
        /* The plain whole form, the suffix form and three meanings, which no word in policy's file has yet. */
        let listed = try AbbreviationVocabulary.load(
            data: Data(
                #"""
                {"abbreviations": [
                    {"abbreviation": "neg", "expansions": ["negative", "negation"], "whole": {"style": "plain"}, "suffix": {}},
                    {"abbreviation": "rel", "expansions": ["relative", "relation", "release"], "segment": {}}
                ]}
                """#.utf8
            )
        )
        let vocabulary = vocabularyName == "policy" ? try Self.vocabulary() : listed
        let finding = try #require(vocabulary.find(name))
        #expect(finding.message == want + " " + Self.reasoning)
    }

    /* `sb` fits more words than a list could hold, a scroll region's bottom among them, so every form asks what it holds. */
    @Test(arguments: [
        ("sb", "whole", #"Identifier "sb" should not be abbreviated. Spell out what it holds."#),
        ("sbBottom", "prefix", #"Identifier "sb" should not be abbreviated. Spell out what it holds."#),
        ("scrollSbOffset", "segment", #"Identifier "scrollSbOffset" abbreviates "Sb". Spell out what it holds."#),
    ])
    func unlistableWordAsksWhatItHolds(name: String, form: String, want: String) throws {
        let finding = try #require(try Self.vocabulary().find(name))
        #expect(finding.form == form)
        #expect(finding.message == want + " " + Self.reasoning)
    }

    @Test func allowedNamesAndSegmentsAreNotJudged() throws {
        let vocabulary = try Self.vocabulary()
        #expect(vocabulary.find("URLSearchParams") == nil)
        #expect(vocabulary.find("InnoDbMaximumIndexKeyBytes") == nil)
        #expect(vocabulary.find("properties") == nil)
        #expect(vocabulary.find("durationInMilliseconds") == nil)
    }

    @Test(arguments: [
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "suffx": {}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"]}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "whole": {"style": "loud"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "whole": {"style": "plain"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "prefix": {"phase": "middle"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "Val", "expansions": ["value"], "segment": {}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "segment": {}}, {"abbreviation": "val", "expansions": ["value"], "segment": {}}]}"#,
        #"{"abbreviations": [{"abbreviation": "ms", "expansions": ["m"], "suffix": {"matcher": "seconds"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "whole": {"messageId": "noVal", "style": "plain"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansion": "value", "whole": {"style": "plain"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": [], "whole": {"style": "plain"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value", ""], "segment": {}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": "value", "whole": {"style": "plain"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "segment": {"style": "loud"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "segment": {"style": "advice"}}]}"#,
    ])
    func aMalformedFileIsRefused(file: String) {
        #expect(throws: (any Error).self) { try AbbreviationVocabulary.load(data: Data(file.utf8)) }
    }

    /*
     The compiled-in words are cohere's `policy/Abbreviations.json`, byte for byte. A word added to the file
     and not regenerated (`go run ./policy/tools/generate`) fails here, so the two engines cannot drift.
     */
    @Test func theCompiledInVocabularyIsPolicysFile() throws {
        let policyFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("policy/Abbreviations.json")
        let onDisk = try Data(contentsOf: policyFile)
        #expect(!onDisk.isEmpty)
        #expect(Data(PolicyAbbreviations.file.utf8) == onDisk, "run go run ./policy/tools/generate")
    }

    @Test func declaredNamesAreJudgedAndReferencesAreNot() throws {
        let source = """
            struct Config {
                let maxBytes: Int
                func load(params: [Int], from src: String, max maximum: Int) -> Int {
                    let idx = params.count + Int.max
                    return idx
                }
            }
            """
        #expect(
            Self.flagged(ConsistencyNoAbbreviatedIdentifier(vocabulary: try Self.vocabulary()), source) == [
                "Config:1", "maxBytes:2", "params:3", "src:3", "idx:4",
            ]
        )
    }

    /* An override's name and single-name labels are the superclass's; a separate label is API and only the name after it is ours. */
    @Test func spellingsChosenElsewhereAreNotJudged() throws {
        let source = """
            class Pane: Base {
                override func setConfig(params: Int) {}
                override var maxWidth: Double { 1 }
                func requestOpenLink(source: String, params parameters: [String: String]) {}
                func update() {
                    if let config { use(config) }
                    let x = 1, i = 2
                }
            }
            """
        #expect(Self.flagged(ConsistencyNoAbbreviatedIdentifier(vocabulary: try Self.vocabulary()), source).isEmpty)
    }

    @Test func lowerCamelCaseIsSwiftFormatsOwnRule() {
        let source = """
            import XCTest
            let max_bytes = 1
            func LoadAll() {}
            final class ScrollTests: XCTestCase {
                func test_scrolls_to_bottom() {}
            }
            """
        #expect(Self.flagged(SwiftFormatRule.requireLowerCamelCase, source) == ["max_bytes:2", "LoadAll:3"])
    }

    /* swift-format's text, made a sentence, then the house's reason, filed under the catalog's id. */
    @Test func lowerCamelCaseSaysSwiftFormatsWordsAndTheReason() {
        let findings = SwiftFormatRule.requireLowerCamelCase.findings(in: Self.file("let max_bytes = 1"))
        #expect(findings.map(\.messageId) == ["alwaysUseLowerCamelCase"])
        #expect(
            findings.first?.message
                == "Rename the constant 'max_bytes' using lowerCamelCase. Swift spells values in lowerCamelCase and types in UpperCamelCase, so a reader tells which is which at a glance, and an underscore inside a name is a word boundary camel case already marks."
        )
    }

    /* A finding covers the name swift-format pointed at, from its first byte to its last, counted in UTF-8 bytes. */
    @Test func lowerCamelCaseCoversTheName() throws {
        let findings = SwiftFormatRule.requireLowerCamelCase.findings(in: Self.file("let café = 1, max_bytes = 2"))
        let finding = try #require(findings.first)
        #expect(findings.count == 1)
        #expect(finding.line == 1 && finding.column == 16)
        #expect(finding.endLine == 1 && finding.endColumn == 25)
    }

    /* Types are UpperCamelCase with no `_` inside, which swift-format leaves alone, under their own message. */
    @Test func typesAreUpperCamelCase() {
        let source = """
            struct Order_Line<element_Type> {}
            final class lowercaseWidget {}
            enum Shape_Kind {}
            protocol Drawing_Surface { associatedtype canvas }
            actor Ledger_Writer {}
            typealias row_Index = Int
            struct OrderLine<Element> {}
            struct _Storage {}
            """
        #expect(
            Self.flagged(SwiftFormatRule.requireLowerCamelCase, source) == [
                "Order_Line:1", "element_Type:1", "lowercaseWidget:2", "Shape_Kind:3", "Drawing_Surface:4", "canvas:4",
                "Ledger_Writer:5", "row_Index:6",
            ]
        )
        let finding = SwiftFormatRule.requireLowerCamelCase.findings(in: Self.file("struct Order_Line {}")).first
        #expect(finding?.messageId == "alwaysUseUpperCamelCaseForType")
        #expect(finding?.message.hasPrefix("Rename the struct 'Order_Line' using UpperCamelCase. Swift spells") == true)
        #expect(finding?.column == 8 && finding?.endColumn == 18)
    }

    /* A value spelled exactly as the external name it reads is the owner's spelling, and is not judged. */
    @Test func aValueMirroringTheNameItReadsIsNotJudged() {
        let source = """
            import Darwin
            let DATABASE_URL = ProcessInfo.processInfo.environment["DATABASE_URL"] ?? ""
            let HOME_DIRECTORY = getenv("HOME_DIRECTORY")
            let PORT = Int(ProcessInfo.processInfo.environment["PORT"] ?? "") ?? 80
            let O_RDONLY = Darwin.O_RDONLY
            let O_WRONLY = (Darwin.O_WRONLY)
            func check(MAX_SIZE: Int?) {
                if let MAX_SIZE { use(MAX_SIZE) }
                guard let MAX_SIZE = MAX_SIZE else { return }
            }
            """
        #expect(Self.flagged(SwiftFormatRule.requireLowerCamelCase, source) == ["MAX_SIZE:7"])
    }

    /* The mirror is exact and read: a renamed upstream value, a key under another name, or a literal held is ours. */
    @Test func aValueThatMirrorsNothingIsJudged() {
        let source = """
            let O_RDONLY_MODE = O_RDONLY
            let DATABASE_ADDRESS = ProcessInfo.processInfo.environment["DATABASE_URL"]
            let API_KEY = "API_KEY"
            let SECRET_NAME = ["SECRET_NAME": 1]
            let TOKEN_VALUE = environment["\\(prefix)TOKEN_VALUE"]
            if let CACHE_SIZE = limits["cache_size"] {}
            """
        #expect(
            Self.flagged(SwiftFormatRule.requireLowerCamelCase, source) == [
                "O_RDONLY_MODE:1", "DATABASE_ADDRESS:2", "API_KEY:3", "SECRET_NAME:4", "TOKEN_VALUE:5", "CACHE_SIZE:6",
            ]
        )
    }

    @Test func leadingUnderscoresAreSwiftFormatsOwnRule() {
        #expect(
            Self.flagged(SwiftFormatRule.noLeadingUnderscores, "let _cache = 1\nlet _ = 2\nfunc _reset() {}\n") == [
                "_cache:1", "_reset:3",
            ]
        )
    }
}
