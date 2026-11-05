import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 consistency-no-shouting past the corpus's cases (`TextRulesConformanceTests` holds those): the masks the shared Go
 package applies, each shown masking and, beside it, the near miss it must leave read; Swift's own notations; the
 strings the rule does not read; and the vocabulary's load and drift checks.
 */
struct ConsistencyNoShoutingTests {
    static func flagged(_ source: String) throws -> [String] {
        let file = ParsedFile(
            url: URL(fileURLWithPath: "/fixture/Subject.swift"),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
        )
        let bytes = Array(source.utf8)
        let starts = TextRulesConformanceTests.lineStarts(bytes)
        return try ConsistencyNoShouting(vocabulary: ShoutingVocabulary.compiledIn()).findings(in: file).map {
            finding in
            let range = TextRulesConformanceTests.byteRange(of: finding, starts: starts)
            let end = range.end ?? range.start
            return "\(finding.line):\(String(decoding: bytes[range.start..<end], as: UTF8.self))"
        }
    }

    @Test func eachShoutedWordIsOneFindingOverTheWord() throws {
        #expect(try Self.flagged("// This is REALLY important, REALLY.\nlet value = 1\n") == ["1:REALLY", "1:REALLY"])
        #expect(try Self.flagged("let value = 1 // NEVER cached\n") == ["1:NEVER"])
        #expect(try Self.flagged("/*\n The pool is\n EMPTY here.\n */\nlet value = 1\n") == ["3:EMPTY"])
        #expect(try Self.flagged("/// Returns NOTHING.\nfunc probe() {}\n") == ["1:NOTHING"])
    }

    /* The shape tests: an allowed word, a digit or an underscore, no vowel, and the named two-letter words. */
    @Test func acronymsAndIdentifiersAreNotShouting() throws {
        #expect(try Self.flagged("// The JSON body arrives over HTTP from the API in USD.\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// Reads DATABASE_URL, then H264 and the CPU count.\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// Read the README and the CHANGELOG.\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// The XKCD comic and the IP range.\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// It IS done, and US is allowed.\nlet value = 1\n") == ["1:IS"])
        /* A word glued to lowercase is a name, never a shout. */
        #expect(try Self.flagged("// NEVERmind the URLs.\nlet value = 1\n").isEmpty)
    }

    @Test func codeIsMasked() throws {
        #expect(try Self.flagged("// the `NOT_FOUND` and `KEEP GOING` branches\nlet value = 1\n").isEmpty)
        #expect(
            try Self.flagged("// a \"STOP NOW\" value, then 'ON CONFLICT DO NOTHING' and 'NOOP'\nlet value = 1\n")
                .isEmpty
        )
        #expect(try Self.flagged("// it says \"the caller must\n// NEVER\" call it\nlet value = 1\n").isEmpty)
        /*
         A fence masks all it holds, a stray backtick included. Only the fence mask holds this: the backtick mask alone
         would pair the fence's third backtick with the stray one and read the word after it.
         */
        #expect(try Self.flagged("// ```\n// a ` b STOP\n// ```\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// Run:\n//\n//     MAKE BUILD\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// $ ECHO HELLO\n// git push ORIGIN\nlet value = 1\n").isEmpty)
        #expect(
            try Self.flagged("/**\n * @example\n * PRINT HELLO\n * @returns NOTHING\n */\nlet value = 1\n") == [
                "4:NOTHING"
            ]
        )
    }

    /* The near misses beside each mask: what looks like code and is not, which is read. */
    @Test func whatOnlyLooksLikeCodeIsRead() throws {
        /* An indented line straight after prose continues the paragraph. */
        #expect(try Self.flagged("// Run this:\n//     MAKE BUILD\nlet value = 1\n") == ["2:MAKE", "2:BUILD"])
        /* An apostrophe is not a quote, and a quote glued to a letter does not open a phrase. */
        #expect(try Self.flagged("// the users' data is GONE\nlet value = 1\n") == ["1:GONE"])
        #expect(try Self.flagged("// it was x'ONE TWO' here\nlet value = 1\n") == ["1:ONE", "1:TWO"])
        /* A lone backtick or an unclosed fence masks nothing. */
        #expect(try Self.flagged("// a ` then STOP\nlet value = 1\n") == ["1:STOP"])
        #expect(try Self.flagged("// ```\n// STOP\nlet value = 1\n") == ["2:STOP"])
        /* Two comments with a blank line between are two texts, so a backtick in each pairs with nothing. */
        #expect(try Self.flagged("// open ` KEEP\n\n// GOING ` close\nlet value = 1\n") == ["1:KEEP", "3:GOING"])
    }

    /* Swift's own notations: Xcode's `MARK:` word, and the generated-code marker only where tools look for it. */
    @Test func swiftNotationsAreNotProse() throws {
        #expect(try Self.flagged("// MARK: - Layout\nlet value = 1\n").isEmpty)
        #expect(try Self.flagged("// MARK: the LOUD part\nlet value = 1\n") == ["1:LOUD"])
        #expect(try Self.flagged("// Code generated by probe. DO NOT EDIT.\nlet value = 1\n").isEmpty)
        #expect(
            try Self.flagged("let value = 1\n// Code generated by probe. DO NOT EDIT.\n")
                == ["2:DO", "2:NOT", "2:EDIT"]
        )
        #expect(try Self.flagged("// DO NOT EDIT.\nlet value = 1\n") == ["1:DO", "1:NOT", "1:EDIT"])
    }

    /* Comments only, as in TypeScript and Go: a string's capitals are often a value someone else spells. */
    @Test func stringsAreNotRead() throws {
        #expect(try Self.flagged("let method = \"POST\"\nlet query = \"SELECT NAME FROM ORDERS\"\n").isEmpty)
    }

    /* Each refusal is the Go loader's, so a file one engine refuses the other refuses too. */
    @Test(arguments: [
        #"[]"#,
        #"{"allowed": [], "shoutedTwoLetterWords": {"reason": "r", "words": ["IS"]}}"#,
        #"{"allowed": [{"reason": "r", "tokens": ["API"]}], "shoutedTwoLetterWords": {"reason": "r", "words": ["IS"]}, "extra": 1}"#,
        #"{"allowed": [{"reason": "r", "tokens": ["Api"]}], "shoutedTwoLetterWords": {"reason": "r", "words": ["IS"]}}"#,
        #"{"allowed": [{"reason": "", "tokens": ["API"]}], "shoutedTwoLetterWords": {"reason": "r", "words": ["IS"]}}"#,
        #"{"allowed": [{"reason": "r", "tokens": ["API"]}], "shoutedTwoLetterWords": {"reason": "r", "words": ["ISO"]}}"#,
        #"{"allowed": [{"reason": "r", "tokens": ["API"], "note": "x"}], "shoutedTwoLetterWords": {"reason": "r", "words": ["IS"]}}"#,
    ])
    func aMalformedFileIsRefused(file: String) {
        #expect(throws: (any Error).self) { try ShoutingVocabulary.load(data: Data(file.utf8)) }
    }

    /*
     The compiled-in words are cohere's `policy/Shouting.json`, byte for byte. A word added to the file and not
     regenerated (`go run ./policy/tools/generate`) fails here, so the two engines cannot drift.
     */
    @Test func theCompiledInVocabularyIsPolicysFile() throws {
        let policyFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("policy/Shouting.json")
        let onDisk = try Data(contentsOf: policyFile)
        #expect(!onDisk.isEmpty)
        #expect(Data(PolicyShouting.file.utf8) == onDisk, "run go run ./policy/tools/generate")
        let vocabulary = try ShoutingVocabulary.compiledIn()
        #expect(vocabulary.allowed.isSuperset(of: ["SSA", "WHATWG", "API", "README", "KRW"]))
        #expect(vocabulary.shoutedTwoLetterWords.contains("IS"))
    }
}
