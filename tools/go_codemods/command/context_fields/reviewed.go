package main

// javaScriptName and abbreviationWord are the reasons that recur.
const (
	javaScriptName   = "a name in the JavaScript a test hands the linter, which no Go selector reads"
	abbreviationWord = "a word of the TypeScript abbreviation vocabulary under test, which nothing looks up as a Go name"
)

// reviewedLiterals are string literals read in their files and judged unable to name a ctx field: by the name
// the literal spells, then the file relative to the root, the reason. The engine still refuses a rename for any
// literal of that name in any other file.
var reviewedLiterals = map[string]map[string]string{
	"ctx": {
		"internal/guard/reads_program_test.go": "the guard's fallback name for a function literal's rule.Context " +
			"parameter whose type is elided; it finds a rule.Context field by its type",
		"internal/lint/ecmascript/high_level_intermediate_representation/reactive_test.go": javaScriptName,
		"internal/lint/rules/nexus/abbreviation_gate_test.go":                              abbreviationWord,
		"internal/lint/rules/nexus/abbreviation_vocabulary_test.go":                        abbreviationWord,
		"internal/lint/rules/nexus/consistency_no_abbreviated_identifier_test.go":          abbreviationWord,
	},
}
