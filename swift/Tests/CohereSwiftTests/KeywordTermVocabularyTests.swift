import Foundation
import Testing

@testable import CohereSwift

/*
 The keyword terms' loader, held to the Go loader's own vectors (`policy/keyword_terms_test.go`), and the abbreviation
 vocabulary's reading of them, held to Go's (`internal/naming/abbreviation/keyword_term_test.go`), so the engines
 refuse the same files and judge the same names (#hvyvfr5).
 */
struct KeywordTermVocabularyTests {
    static let minimal =
        #"{"about": "a", "keywordTerms": ["#
        + #"{"term": "var", "language": "JavaScript", "spellings": ["VarKeyword", "VarScoped"], "reason": "r", "#
        + #""citation": "https://example.com/var"}, "#
        + #"{"term": "let", "language": "JavaScript", "spellings": ["LetKeyword"], "reason": "r", "#
        + #""citation": "https://example.com/let"}]}"#

    @Test func theMinimalFileLoads() throws {
        let vocabulary = try KeywordTermVocabulary.load(data: Data(Self.minimal.utf8))
        #expect(
            vocabulary.keywordTerms == [
                .init(term: "var", spellings: ["VarKeyword", "VarScoped"]),
                .init(term: "let", spellings: ["LetKeyword"]),
            ]
        )
    }

    @Test(arguments: [
        ("an unknown top-level key", #""about": "a""#, #""about": "a", "aliases": []"#),
        (
            "an unknown key on an entry", #""language": "JavaScript", "spellings": ["VarKeyword""#,
            #""language": "JavaScript", "note": "n", "spellings": ["VarKeyword""#,
        ),
        ("an empty about", #""about": "a""#, #""about": " ""#),
        ("a term listed twice", #""term": "let""#, #""term": "var""#),
        ("a capitalized term", #""term": "var""#, #""term": "Var""#),
        ("a term with a digit", #""term": "var""#, #""term": "var2""#),
        ("an empty term", #""term": "var""#, #""term": """#),
        ("no language", #""language": "JavaScript", "spellings": ["VarKeyword""#, #""spellings": ["VarKeyword""#),
        ("no spellings", #""spellings": ["VarKeyword", "VarScoped"]"#, #""spellings": []"#),
        (
            "a lone spelling rather than a list", #""spellings": ["VarKeyword", "VarScoped"]"#,
            #""spellings": "VarKeyword""#,
        ),
        ("a spelling that is the keyword alone", #""VarScoped""#, #""Var""#),
        ("a spelling of another word", #""VarScoped""#, #""LetScoped""#),
        ("a spelling not capitalized", #""VarScoped""#, #""varScoped""#),
        ("a spelling that runs on in lowercase", #""VarScoped""#, #""Varscoped""#),
        ("a spelling with an underscore", #""VarScoped""#, #""Var_Scoped""#),
        ("a spelling listed twice", #""VarScoped""#, #""VarKeyword""#),
        (
            "no reason", #""reason": "r", "citation": "https://example.com/var""#,
            #""reason": " ", "citation": "https://example.com/var""#,
        ),
        ("no citation", #", "citation": "https://example.com/var""#, ""),
        ("a citation that is not an address", #""https://example.com/var""#, #""the specification""#),
        ("a citation over plain http", #""https://example.com/var""#, #""http://example.com/var""#),
    ])
    func whatWouldReadAsSomethingElseIsRefused(name: String, original: String, replacement: String) throws {
        /* The minimal file loads, so each refusal below is the change's doing. */
        _ = try KeywordTermVocabulary.load(data: Data(Self.minimal.utf8))
        let range = try #require(Self.minimal.range(of: original), "\(name): the change does not apply")
        let changed = Self.minimal.replacingCharacters(in: range, with: replacement)
        #expect(throws: KeywordTermVocabulary.LoadFailure.self, "\(name) is accepted: \(changed)") {
            try KeywordTermVocabulary.load(data: Data(changed.utf8))
        }
    }

    @Test func theCompiledInTermsHoldTheJavaScriptKeyword() throws {
        let vocabulary = try KeywordTermVocabulary.compiledIn()
        let term = try #require(vocabulary.keywordTerms.first { $0.term == "var" })
        #expect(term.spellings.contains("VarDeclaration"))
    }

    /* The compiled-in terms are cohere's `policy/KeywordTerms.json`, byte for byte. A spelling added to the file and not regenerated fails here. */
    @Test func theCompiledInTermsArePolicysFile() throws {
        let policyFile = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .deletingLastPathComponent()
            .appendingPathComponent("policy/KeywordTerms.json")
        let onDisk = try Data(contentsOf: policyFile)
        #expect(!onDisk.isEmpty)
        #expect(Data(PolicyKeywordTerms.file.utf8) == onDisk, "run go run ./policy/tools/generate")
    }

    /*
     A name holding a listed spelling is clean, and the same name is reported by the words read with no keyword terms,
     which is the control: it is the keyword term that silences it, never a rule that judged nothing.
     */
    @Test(arguments: [
        "isVarDeclaration", "VarKeywordFix", "VarDeclarations", "TopLevelVarScopedDeclarations",
        "collectVarDeclaredNames", "VarKeyword", "hoistVarDeclarationsToTop", "VarScoped2",
    ])
    func aKeywordTermReadsAsAWord(name: String) throws {
        #expect(try AbbreviationVocabulary.compiledIn().find(name) == nil)
        let withoutKeywordTerms = try AbbreviationVocabulary.load(data: Data(PolicyAbbreviations.file.utf8))
        #expect(withoutKeywordTerms.find(name)?.abbreviation == "var")
    }

    /* A bare `Var`, a spelling run on into a longer word, and a keyword term beside a bare `Var` or another word still report. */
    @Test(arguments: [
        ("NoVar", "var"),
        ("themeVar", "var"),
        ("themeVarName", "var"),
        ("VarScopedness", "var"),
        ("isVarKeywordish", "var"),
        ("VarDeclarationForThemeVar", "var"),
        ("themeVarBeforeVarKeyword", "var"),
        ("VarKeywordCmd", "cmd"),
        ("VarDeclarationSrcPath", "src"),
        ("VarKeywordVars", "vars"),
    ])
    func everyOtherSegmentIsStillReported(name: String, abbreviation: String) throws {
        let finding = try #require(try AbbreviationVocabulary.compiledIn().find(name))
        #expect(finding.form == "segment")
        #expect(finding.abbreviation == abbreviation)
    }

    /* `var` alone, so a refusal below is its own and never another term's. */
    static let varAlone =
        #"{"about": "a", "keywordTerms": [{"term": "var", "language": "JavaScript", "spellings": ["VarKeyword"], "#
        + #""reason": "r", "citation": "https://example.com/var"}]}"#

    /* A keyword term whose word has no advice-style segment would silence nothing, or a rename, so it is refused. */
    @Test(arguments: [
        #"{"abbreviations": [{"abbreviation": "val", "expansions": ["value"], "segment": {}}]}"#,
        #"{"abbreviations": [{"abbreviation": "var", "advice": "Say.", "whole": {"style": "advice"}}]}"#,
        #"{"abbreviations": [{"abbreviation": "var", "expansions": ["variable"], "segment": {}}]}"#,
    ])
    func aKeywordTermWithNoAdvisingSegmentIsRefused(file: String) throws {
        let keywordTerms = try KeywordTermVocabulary.load(data: Data(Self.varAlone.utf8))
        #expect(throws: AbbreviationVocabulary.LoadFailure.self) {
            try AbbreviationVocabulary.load(data: Data(file.utf8), keywordTerms: keywordTerms)
        }
    }

    /* The entry beside the refusals: an advice-style segment carries its keyword term, and the term silences its spelling. */
    @Test func aKeywordTermWithAnAdvisingSegmentLoads() throws {
        let keywordTerms = try KeywordTermVocabulary.load(data: Data(Self.varAlone.utf8))
        let file = #"{"abbreviations": [{"abbreviation": "var", "advice": "Say.", "segment": {"style": "advice"}}]}"#
        let vocabulary = try AbbreviationVocabulary.load(data: Data(file.utf8), keywordTerms: keywordTerms)
        #expect(vocabulary.find("isVarKeyword") == nil)
        #expect(vocabulary.find("NoVar") != nil)
    }
}
