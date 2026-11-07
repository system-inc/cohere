package main

// nameOverrides choose a name where the word-cased one is taken, by the declaring file relative to the root and
// the old name. Each is a person's choice, read in its file.
var nameOverrides = map[string]string{
	// parseScss, upstream's parseScss(text, options), is declared in the same file; this is its scss twin of
	// parse, "the scss parser's parse", as parse is the css parser's.
	"internal/format/css/parser.go:parseSCSS": "scssParse",
	// Word casing reads a run of capitals followed by a lone s as a plural (IDs, Ids). In these sentences the s
	// opens the word Is, and the initialism ends a letter earlier.
	"internal/format/css/format_test.go:TestFormatSCSSIsNotFormat":                                "TestFormatScssIsNotFormat",
	"internal/format/css/format_test.go:TestPrintToDocSCSSIsFormatSCSSWithoutTheTrailingHardline": "TestPrintToDocScssIsFormatScssWithoutTheTrailingHardline",
	"internal/types/program/timing_test.go:TestTheWorkersCPUIsAccountedFor":                       "TestTheWorkersCpuIsAccountedFor",
	"internal/release/packaging/swift_package_test.go:TestMinimumMacOSIsReadFromTheBinary":        "TestMinimumMacOsIsReadFromTheBinary",
	// Here an article runs into the initialism after it: a CRLF file. A JSDoc edit drops its article, since AJs
	// would read as a plural again.
	"command/cohere/line_endings_test.go:TestACRLFFilesWouldChangeFindingSaysWhy":           "TestACrlfFilesWouldChangeFindingSaysWhy",
	"internal/types/program/signatures_test.go:TestAJSDocEditOnAnExportChangesTheSignature": "TestJsDocEditOnAnExportChangesTheSignature",
	// ECMAScript's <LS> and <PS>, U+2028 and U+2029, read as what they are rather than as two letters each.
	"internal/lint/ecmascript/regexp/regexp_test.go:LS": "lineSeparator",
	"internal/lint/ecmascript/regexp/regexp_test.go:PS": "paragraphSeparator",
	// UV reports whether the u or the v flag puts the pattern in a Unicode mode.
	"internal/lint/ecmascript/regexsyntax/charclass.go:UV": "UnicodeMode",
}

// shoutingWord and goEnvironmentArgument are the reasons that recur.
const (
	shoutingWord          = "a word in the shouting rule's list of initialisms it allows, which nothing looks up as a Go name"
	goEnvironmentArgument = "a variable handed to `go env`, whose json output the encoding guard keeps keyed by a tag"
)

// reviewedLiterals are string literals read in their files and judged unable to name the declaration a
// candidate renames: by the name the literal spells, then the file relative to the root, the reason. The engine
// still refuses a rename for any literal of that name in any other file.
var reviewedLiterals = map[string]map[string]string{
	"OS": {
		"internal/shouting/shouting.go": shoutingWord,
	},
	"CSS": {
		"internal/shouting/shouting.go":                   shoutingWord,
		"internal/format/markdown/languages_generated.go": "Linguist's name for the language, in the generated language table",
	},
	"URL": {
		"internal/shouting/shouting.go":                                   shoutingWord,
		"internal/lint/rules/next/no_unwanted_polyfillio.go":              "a JavaScript global polyfill.io can serve",
		"internal/lint/rules/nexus/constant_casing_shapes.go":             "a JavaScript global class",
		"internal/lint/rules/typescript/no_base_to_string.go":             "a TypeScript library type the rule ignores by default",
		"internal/lint/rules/typescript/no_base_to_string_test.go":        "a TypeScript library type the rule's test expects ignored",
		"internal/lint/rules/typescript/restrict_template_expressions.go": "a TypeScript library type the rule allows by default",
	},
	"YAML": {
		"internal/shouting/shouting.go":                             shoutingWord,
		"internal/format/markdown/languages_generated.go":           "Linguist's name for the language, in the generated language table",
		"internal/format/markdown/tools/generate_languages/main.go": "a language name the generator checks Linguist's table against",
	},
	"BOM": {
		"internal/shouting/shouting.go": shoutingWord,
	},
	"GOOS": {
		"internal/goengine/load/excluded.go": goEnvironmentArgument,
		"internal/gotypes/list.go":           "a variable handed to `go env -json` and read back as a key of a map",
		"internal/gotypes/product.go":        "a key of the go env map",
		"internal/gotypes/product_test.go":   "a key of the go env map the test builds",
		"internal/release/dispatch/hash.go":  "a variable handed to `go env`",
	},
	"GOARCH": {
		"internal/goengine/load/excluded.go": goEnvironmentArgument,
		"internal/gotypes/list.go":           "a variable handed to `go env -json` and read back as a key of a map",
		"internal/gotypes/load.go":           "a key of the go env map",
		"internal/gotypes/product.go":        "a key of the go env map",
		"internal/gotypes/product_test.go":   "a key of the go env map the test builds",
		"internal/release/dispatch/hash.go":  "a variable handed to `go env`",
	},
	"GOFLAGS": {
		"internal/goengine/load/excluded.go":          goEnvironmentArgument,
		"internal/goengine/load/load.go":              "a variable handed to `go env`",
		"command/cohere-dev/pool.go":                  "an environment variable's name, matched in a switch",
		"command/cohere/run_cache_end_to_end_test.go": "a variable handed to `go env -json`, decoded into a map",
		"internal/guard/guard_overlay_test.go":        "an environment variable read with os.Getenv",
	},
}
