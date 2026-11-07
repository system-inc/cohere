import Foundation
import SwiftParser
import Testing

@testable import CohereSwift

/*
 consistency-require-separated-folder-words: the split held to the Go rule's own table (`separated_test.go`), which
 every lowercase name reads the same way in both engines, then what Swift adds: the parts a capital or a character
 that is not a letter ends, and the replacement in each part's own style. Each path's package root is the directory
 named `project` on it, as the pipeline would set it.
 */
struct ConsistencyRequireSeparatedFolderWordsTests {
    static let source = "/* A header comment, so the finding is seen to sit before it. */\nlet value = 1\n"

    static func rule() throws -> ConsistencyRequireSeparatedFolderWords {
        ConsistencyRequireSeparatedFolderWords(
            words: try WordList.compiledIn(),
            brands: try BrandVocabulary.compiledIn(),
        )
    }

    static func findings(_ path: String, packageRoot: URL? = nil) throws -> [FindingRecord] {
        let file = ParsedFile(
            url: URL(fileURLWithPath: path),
            targetName: "Fixture",
            targetKind: "library",
            source: source,
            tree: Parser.parse(source: source),
            nodeCount: 0,
            packageRoot: packageRoot ?? ConsistencyNoUtilsFolderTests.packageRoot(of: path),
        )
        let rule = try rule()
        guard rule.applies(to: file) else { return [] }
        return rule.findings(in: file)
    }

    static func described(_ findings: [FindingRecord]) -> [String] {
        findings.map { "\($0.line):\($0.column)-\($0.endLine ?? 0):\($0.endColumn ?? 0) \($0.messageId) \($0.message)" }
    }

    /*
     Go's table, name for name: the cohere and loom censuses, the tie-break, the brand, three pieces, words a split
     would break, names already separated, and names that split no way. An empty want is a name that passes. Go's
     `FormatOptions` is left out, since Swift reads its capital as the separator Go reads an underscore as.
     */
    @Test(arguments: [
        ("textrules", "text_rules"),
        ("benchresults", "bench_results"),
        ("runoutput", "run_output"),
        ("docsdata", "docs_data"),
        ("mediaquery", "media_query"),
        ("formatfiles", "format_files"),
        ("formatoptions", "format_options"),
        ("micromark", "micro_mark"),
        ("oracletest", "oracle_test"),
        ("goengine", "go_engine"),
        ("cgotest", "cgo_test"),
        ("analysistest", "analysis_test"),
        ("driverutil", "driver_util"),
        ("inertvalue", "inert_value"),
        ("markertype", "marker_type"),
        ("gotypes", "go_types"),
        ("classmembers", "class_members"),
        ("consistentreturn", "consistent_return"),
        ("dotnotation", "dot_notation"),
        ("nextjs", "next_js"),
        ("regexpattern", "regex_pattern"),
        ("regexsyntax", "regex_syntax"),
        ("housesets", "house_sets"),
        ("optionschema", "option_schema"),
        ("tooldirectory", "tool_directory"),
        ("crosscompile", "cross_compile"),
        ("gocache", "go_cache"),
        ("settingsschema", "settings_schema"),
        ("sqltext", "sql_text"),
        ("testfunctions", "test_functions"),
        ("sourcename", "source_name"),
        ("warningcomment", "warning_comment"),
        ("tsoptions", "ts_options"),
        ("tspath", "ts_path"),
        ("unicodeproperties", "unicode_properties"),
        ("moduletest", "module_test"),
        ("gateinputs", "gate_inputs"),
        ("gatelines", "gate_lines"),
        ("housecache", "house_cache"),
        ("jsonlines", "json_lines"),
        ("livestatus", "live_status"),
        ("ownerbridge", "owner_bridge"),
        ("queuebridge", "queue_bridge"),
        ("treebuilder", "tree_builder"),
        /* Ties go to the shorter first piece: not runout_put, gates_ample or gop_lan. */
        ("gatesample", "gate_sample"),
        ("goplan", "go_plan"),
        /* A brand is a word, whole or as a piece. */
        ("imessage", ""),
        ("imessagebridge", "imessage_bridge"),
        /* Three pieces, two of them closed two-letter words. */
        ("gouiengine", "go_ui_engine"),
        /* Each part on its own, keeping the underscores already there, leading ones included. */
        ("text_formatoptions", "text_format_options"),
        ("_formatoptions", "_format_options"),
        ("formatoptions_test", "format_options_test"),
        /* Words a split would break. */
        ("workspace", ""),
        ("charset", ""),
        ("gitignore", ""),
        ("changelog", ""),
        ("lowercase", ""),
        ("javascript", ""),
        ("toolchain", ""),
        /* Already separated. */
        ("format_options", ""),
        ("json_value", ""),
        /* Names that split no way, so they pass, the three-letter minimum keeping acheck and withc whole. */
        ("vfs", ""),
        ("fmt", ""),
        ("sys", ""),
        ("jsx", ""),
        ("ucd", ""),
        ("cachedvfs", ""),
        ("osvfs", ""),
        ("unist", ""),
        ("graphql", ""),
        ("acheck", ""),
        ("withc", ""),
        ("stage1", ""),
        ("", ""),
    ])
    func goReadsTheSameSplit(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == (want.isEmpty ? nil : want))
    }

    /* A part ends at every character that is not an ASCII letter, and at a case boundary, an acronym kept whole before the word that follows it. */
    @Test(arguments: [
        ("AISTBench", ["AIST", "Bench"]),
        ("iOSTerminal", ["i", "OS", "Terminal"]),
        ("AhraOsServices", ["Ahra", "Os", "Services"]),
        ("CohereSwiftTests", ["Cohere", "Swift", "Tests"]),
        ("GLTF2RealityKit", ["GLTF", "Reality", "Kit"]),
        ("URLSession", ["URL", "Session"]),
        ("HTTP", ["HTTP"]),
        ("format_options", ["format", "options"]),
        ("my-folder.v2", ["my", "folder", "v"]),
        ("caf\u{E9}Menu", ["caf", "Menu"]),
        ("__", []),
    ])
    func partsEndAtCapitalsAndAtWhatIsNotALetter(name: String, parts: [String]) {
        let bytes = Array(name.utf8)
        let found = ConsistencyRequireSeparatedFolderWords.parts(of: bytes).map {
            String(decoding: bytes[$0], as: UTF8.self)
        }
        #expect(found == parts)
    }

    /* A capitalized part, in a folder of both cases, has each piece capitalized and run together, the rest of the name as written. */
    @Test(arguments: [
        ("Formatoptions", "FormatOptions"),
        ("TextFormatoptions", "TextFormatOptions"),
        ("GoengineTests", "GoEngineTests"),
        ("Imessagebridge", "ImessageBridge"),
        ("Formatoptions2.Jsonlines", "FormatOptions2.JsonLines"),
    ])
    func aCapitalizedPartIsCapitalizedPieces(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == want)
    }

    /* A lowercase part, in a folder of both cases, keeps its first piece as written and capitalizes the rest. */
    @Test(arguments: [
        ("goengineTests", "goEngineTests"),
        ("jsonlinesReader", "jsonLinesReader"),
        ("goengine_Tests", "goEngine_Tests"),
    ])
    func aLowercasePartInAMixedCaseFolderIsCamelCase(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == want)
    }

    /* An all-capitals part, in a folder of both cases, is capitalized pieces run together, the house's PascalCase acronyms. */
    @Test(arguments: [
        ("JSONLINESReader", "JsonLinesReader"),
        ("TextJSONLINES", "TextJsonLines"),
    ])
    func anAllCapitalsPartInAMixedCaseFolderIsPascalCase(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == want)
    }

    /* A folder with no lowercase letter keeps capitals, joined by `_`. */
    @Test(arguments: [
        ("JSONLINES", "JSON_LINES"),
        ("FORMATOPTIONS", "FORMAT_OPTIONS"),
        ("TEXT_FORMATOPTIONS", "TEXT_FORMAT_OPTIONS"),
    ])
    func aFolderWithNoLowercaseLetterIsCapitalsJoinedByUnderscores(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == want)
    }

    /* A folder with no capital joins with `_`, or with `-` where it uses hyphens and no underscores. */
    @Test(arguments: [
        ("formatoptions", "format_options"),
        ("text-formatoptions", "text-format-options"),
        ("text-formatoptions_v2", "text-format_options_v2"),
    ])
    func aFolderWithNoCapitalIsJoinedByItsSeparator(name: String, want: String) throws {
        #expect(try Self.rule().separated(name) == want)
    }

    /* Swift's own names: words its capitals already separate, acronyms, and a brand spelled as its owner spells it. */
    @Test(arguments: [
        "AhraOsServices",
        "CohereSwiftTests",
        "GLTF2RealityKit",
        "AISTBench",
        "iOSTerminal",
        "iMessageBridge",
        "FormatOptions",
        "Sources",
        "Tests",
        "Workspace",
    ])
    func separatedSwiftNamesPass(name: String) throws {
        #expect(try Self.rule().separated(name) == nil)
    }

    @Test func theFindingSitsAtTheFileStartWithTheMessage() throws {
        #expect(
            Self.described(try Self.findings("/project/Sources/Fixture/Formatoptions/Thing.swift")) == [
                "1:1-1:1 runTogetherFolderWords Folder name \"Formatoptions\" runs its words together. Use \"FormatOptions\" instead. A mind reading cold reads `FormatOptions` at a glance and has to stop and parse `Formatoptions`, and a folder's name is read on every path, so its words are separated: by capitals in a capitalized name, by underscores or hyphens in a lowercase one."
            ]
        )
    }

    /* One finding per file, for the outermost run-together folder. */
    @Test func theOutermostFolderIsNamed() throws {
        let found = try Self.findings("/project/Sources/Goengine/Formatoptions/Thing.swift")
        #expect(found.count == 1)
        #expect(
            found.first?.message.hasPrefix("Folder name \"Goengine\" runs its words together. Use \"GoEngine\"") == true
        )
    }

    @Test(arguments: [
        /* A folder above the package is never the package's to rename. */
        "/Users/someone/goengine/project/Sources/Fixture/Thing.swift",
        /* A file whose name runs words together is not a folder that does. */
        "/project/Sources/Fixture/Formatoptions.swift",
        /* No known package root: not judged, rather than judged against a guess. */
        "/Users/someone/goengine/elsewhere/Thing.swift",
    ])
    func foldersOutsideThePackageAreNotJudged(path: String) throws {
        #expect(try Self.findings(path).isEmpty)
    }

    /* The project's root folder is the checkout's name, never judged (fw12), though the folders under it are. */
    @Test func theRootFolderIsNotJudged() throws {
        let root = URL(fileURLWithPath: "/checkouts/goengine", isDirectory: true)
        #expect(try Self.findings("/checkouts/goengine/Thing.swift", packageRoot: root).isEmpty)
        #expect(try Self.findings("/checkouts/goengine/Sources/Thing.swift", packageRoot: root).isEmpty)
        #expect(try Self.findings("/checkouts/goengine/jsonlines/Thing.swift", packageRoot: root).count == 1)
    }
}
