package consistency_require_separated_folder_words

import "testing"

// The split itself, on every folder the census of cohere and loom reports, each with the spelling it asks for, and
// on the names it must leave alone: words a split would break, names already separated, and names that split no way
// because no word list holds them. An empty want is a name that passes.
func TestSeparated(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		want string
	}{
		// The cohere census.
		{"textrules", "text_rules"},
		{"benchresults", "bench_results"},
		{"runoutput", "run_output"},
		{"docsdata", "docs_data"},
		{"mediaquery", "media_query"},
		{"formatfiles", "format_files"},
		{"formatoptions", "format_options"},
		{"micromark", "micro_mark"},
		{"oracletest", "oracle_test"},
		{"goengine", "go_engine"},
		{"cgotest", "cgo_test"},
		{"analysistest", "analysis_test"},
		{"driverutil", "driver_util"},
		{"inertvalue", "inert_value"},
		{"markertype", "marker_type"},
		{"gotypes", "go_types"},
		{"classmembers", "class_members"},
		{"consistentreturn", "consistent_return"},
		{"dotnotation", "dot_notation"},
		{"nextjs", "next_js"},
		{"regexpattern", "regex_pattern"},
		{"regexsyntax", "regex_syntax"},
		{"housesets", "house_sets"},
		{"optionschema", "option_schema"},
		{"tooldirectory", "tool_directory"},
		{"crosscompile", "cross_compile"},
		{"gocache", "go_cache"},
		{"settingsschema", "settings_schema"},
		{"sqltext", "sql_text"},
		{"testfunctions", "test_functions"},
		{"sourcename", "source_name"},
		{"warningcomment", "warning_comment"},
		{"tsoptions", "ts_options"},
		{"tspath", "ts_path"},
		{"unicodeproperties", "unicode_properties"},
		// The loom census.
		{"moduletest", "module_test"},
		{"gateinputs", "gate_inputs"},
		{"gatelines", "gate_lines"},
		{"housecache", "house_cache"},
		{"jsonlines", "json_lines"},
		{"livestatus", "live_status"},
		{"ownerbridge", "owner_bridge"},
		{"queuebridge", "queue_bridge"},
		{"treebuilder", "tree_builder"},
		// Ties go to the shorter first piece.
		{"gatesample", "gate_sample"},
		{"goplan", "go_plan"},
		// A brand is a word, whole or as a piece.
		{"imessage", ""},
		{"imessagebridge", "imessage_bridge"},
		// Three pieces, two of them closed two-letter words.
		{"gouiengine", "go_ui_engine"},
		// Each part on its own, keeping the underscores already there, leading ones included, and lowercased.
		{"text_formatoptions", "text_format_options"},
		{"_formatoptions", "_format_options"},
		{"FormatOptions", "format_options"},
		{"formatoptions_test", "format_options_test"},
		// Words a split would break.
		{"workspace", ""},
		{"charset", ""},
		{"gitignore", ""},
		{"changelog", ""},
		{"lowercase", ""},
		{"javascript", ""},
		{"toolchain", ""},
		// Already separated.
		{"format_options", ""},
		{"json_value", ""},
		// Names that split no way, so they pass. Without the three-letter minimum, acheck and withc would split as
		// ac_heck and wi_thc.
		{"vfs", ""},
		{"fmt", ""},
		{"sys", ""},
		{"jsx", ""},
		{"ucd", ""},
		{"cachedvfs", ""},
		{"osvfs", ""},
		{"unist", ""},
		{"graphql", ""},
		{"acheck", ""},
		{"withc", ""},
		{"stage1", ""},
		{"", ""},
	}
	for _, testCase := range cases {
		got, runTogether := separated(testCase.name)
		if testCase.want == "" {
			if runTogether {
				test.Errorf("%q is reported as %q, and it passes", testCase.name, got)
			}
			continue
		}
		if !runTogether || got != testCase.want {
			test.Errorf("%q is %q (reported %t), want %q", testCase.name, got, runTogether, testCase.want)
		}
	}
}
