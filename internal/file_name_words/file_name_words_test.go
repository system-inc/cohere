package file_name_words

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/naming/abbreviation"
)

// A file name's words in each language's convention: Go's snake_case, TypeScript's camelCase, PascalCase and
// kebab-case, and Swift's PascalCase with its `+Purpose`, each with what a tool reads off the name set aside, and the
// names never judged, which give no words at all.
func TestWords(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName string
		language Language
		want     []string
		judged   bool
	}{
		// Go: underscores separate, and the toolchain's suffixes are no words.
		{"util.go", LanguageGo, []string{"util"}, true},
		{"string_utils.go", LanguageGo, []string{"string", "utils"}, true},
		{"util_test.go", LanguageGo, []string{"util"}, true},
		{"error_msg_test.go", LanguageGo, []string{"error", "msg"}, true},
		{"registry_linux.go", LanguageGo, []string{"registry"}, true},
		{"registry_amd64.go", LanguageGo, []string{"registry"}, true},
		{"registry_linux_amd64_test.go", LanguageGo, []string{"registry"}, true},
		// go/build reads no constraint from a name with no underscore before it, so `linux.go` is a word.
		{"linux.go", LanguageGo, []string{"linux"}, true},
		// A build tag go/build does not read from a name, unix, is the author's word.
		{"registry_unix.go", LanguageGo, []string{"registry", "unix"}, true},
		{"doc.go", LanguageGo, nil, false},
		// TypeScript: case, hyphens and underscores separate, and a role marker is no word.
		{"stringUtils.ts", LanguageTypeScript, []string{"string", "Utils"}, true},
		{"StringUtils.tsx", LanguageTypeScript, []string{"String", "Utils"}, true},
		{"string-utils.ts", LanguageTypeScript, []string{"string", "utils"}, true},
		{"_utils.ts", LanguageTypeScript, []string{"utils"}, true},
		{"errorMsg.test.ts", LanguageTypeScript, []string{"error", "Msg"}, true},
		{"Button.stories.tsx", LanguageTypeScript, []string{"Button"}, true},
		{"globals.d.ts", LanguageTypeScript, []string{"globals"}, true},
		{"next.config.ts", LanguageTypeScript, []string{"next"}, true},
		// The first part is the name, never a role: config.ts and test.ts are named for those words.
		{"config.ts", LanguageTypeScript, []string{"config"}, true},
		{"test.ts", LanguageTypeScript, []string{"test"}, true},
		{"URLSearchParser.ts", LanguageTypeScript, []string{"URL", "Search", "Parser"}, true},
		// A dot never hides a word: a part that is no tool's marker is the author's.
		{"string.utils.ts", LanguageTypeScript, []string{"string", "utils"}, true},
		{"icon2.tsx", LanguageTypeScript, []string{"icon"}, true},
		{"GraphQlSchema.generated.ts", LanguageTypeScript, nil, false},
		// Swift: case and the plus separate, and a nested type's dot is a word boundary.
		{"ViewUtil.swift", LanguageSwift, []string{"View", "Util"}, true},
		{"Utils.swift", LanguageSwift, []string{"Utils"}, true},
		{"String+Utils.swift", LanguageSwift, []string{"String", "Utils"}, true},
		{"Outer.Inner+Purpose.swift", LanguageSwift, []string{"Outer", "Inner", "Purpose"}, true},
		{"PolicyWords.generated.swift", LanguageSwift, nil, false},
		{"Package.swift", LanguageSwift, nil, false},
		{"Package@swift-6.0.swift", LanguageSwift, nil, false},
		{"main.swift", LanguageSwift, nil, false},
	}
	for _, testCase := range cases {
		got, judged := Words(testCase.fileName, testCase.language)
		if judged != testCase.judged || !slices.Equal(got, testCase.want) {
			test.Errorf("%s in %s: %q (judged %t), want %q (judged %t)", testCase.fileName, testCase.language, got,
				judged, testCase.want, testCase.judged)
		}
	}
}

// A name is `util` or `utils` lowercased with its leading underscores removed, matched whole, the reading the folder
// rules share with the file rules.
func TestIsUtils(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{
		"util":          true,
		"utils":         true,
		"Utils":         true,
		"UTIL":          true,
		"_utils":        true,
		"__Util":        true,
		"utilities":     false,
		"utility":       false,
		"myutils":       false,
		"utilsomething": false,
		"utils_":        false,
		"":              false,
	} {
		if got := IsUtils(name); got != want {
			test.Errorf("IsUtils(%q) = %t, want %t", name, got, want)
		}
	}
}

// The word a finding names is the first `util` or `utils` as the name spells it, and the house spelling keeps its case.
func TestUtilsWordAndItsReplacement(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName    string
		language    Language
		word        string
		replacement string
	}{
		{"util.go", LanguageGo, "util", "utilities"},
		{"string_utils.go", LanguageGo, "utils", "utilities"},
		{"stringUtils.ts", LanguageTypeScript, "Utils", "Utilities"},
		{"ViewUtil.swift", LanguageSwift, "Util", "Utilities"},
		{"STRING_UTILS.ts", LanguageTypeScript, "UTILS", "UTILITIES"},
	}
	for _, testCase := range cases {
		words, _ := Words(testCase.fileName, testCase.language)
		word, found := UtilsWord(words)
		if !found || word != testCase.word {
			test.Errorf("%s: word %q (found %t), want %q", testCase.fileName, word, found, testCase.word)
			continue
		}
		if got := UtilitiesReplacement(word); got != testCase.replacement {
			test.Errorf("%s: replacement %q, want %q", testCase.fileName, got, testCase.replacement)
		}
	}
	// The last two are named for the rule that bans the word, which they hold.
	for _, fileName := range []string{
		"utilities.go",
		"stringutils.go",
		"string_formatting.go",
		"utility_test.go",
		"consistency_no_utils_folder.go",
		"consistency_no_utils_file_name_test.go",
	} {
		words, _ := Words(fileName, LanguageGo)
		if word, found := UtilsWord(words); found {
			test.Errorf("%s: reported %q, and it passes", fileName, word)
		}
	}
}

// A file name's words are judged as the identifier holding them in the same order, in every convention, so the word an
// identifier may not abbreviate a file name may not either, and a suffix a tool reads is never what ends the name.
func TestAbbreviated(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName string
		language Language
		word     string
	}{
		{"cfg.go", LanguageGo, "cfg"},
		{"msg_printer.go", LanguageGo, "msg"},
		{"msgPrinter.ts", LanguageTypeScript, "msg"},
		{"msg-printer.ts", LanguageTypeScript, "msg"},
		{"MsgPrinter.swift", LanguageSwift, "msg"},
		// The suffix form reads the last word, which is the name's own only once the tool's suffix is set aside.
		{"error_msg_test.go", LanguageGo, "msg"},
		{"error_msg_linux.go", LanguageGo, "msg"},
		{"errorMsg.test.ts", LanguageTypeScript, "msg"},
		{"timeout_ms.go", LanguageGo, "ms"},
		{"output_dir.go", LanguageGo, "dir"},
		// The words go/ast, go/types and go/ir taught, judged where the word ends (#r0fa8pa).
		{"importer_func.go", LanguageGo, "func"},
		{"PrintExpr.swift", LanguageSwift, "expr"},
	}
	for _, testCase := range cases {
		words, _ := Words(testCase.fileName, testCase.language)
		finding, found := Abbreviated(words, nil)
		if !found || finding.Entry.Abbreviation != testCase.word {
			test.Errorf("%s: %+v (found %t), want the word %q", testCase.fileName, finding.Entry, found, testCase.word)
		}
	}
	for _, fileName := range []string{
		"message_printer.go", "configuration.go", "registry_linux.go", "client.go", "condition.go",
		"change_set.go", "open_file.go", "progress.go", "select_all.go", "type_queries.go",
	} {
		words, _ := Words(fileName, LanguageGo)
		if finding, found := Abbreviated(words, nil); found {
			test.Errorf("%s: reported %q, and it passes", fileName, finding.Entry.Abbreviation)
		}
	}
	// An initialism in capitals is no abbreviation of the vocabulary's: the flags of Switzerland, Montserrat, Mauritius
	// and the Solomon Islands, and a capitalized initialism opening a name.
	for _, fileName := range []string{"FlagCH.tsx", "FlagMS.tsx", "FlagMU.tsx", "FlagSB.tsx", "MSWHandler.ts"} {
		words, _ := Words(fileName, LanguageTypeScript)
		if finding, found := Abbreviated(words, nil); found {
			test.Errorf("%s: reported %q, and it passes", fileName, finding.Entry.Abbreviation)
		}
	}
}

// A word policy/NameWords.json holds is judged in a file name in every convention, `docs` as documentation, while the
// shared vocabulary leaves it alone, so the identifier rule never reads it (#zyq63kk). Google Docs, its allowed
// segment, passes, and a word the shared vocabulary holds beside the brand is still reported.
func TestAbbreviatedReadsTheNameWords(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		fileName string
		language Language
	}{
		{"docs_data.go", LanguageGo},
		{"docs_capture_test.go", LanguageGo},
		{"api_docs.go", LanguageGo},
		{"docs.ts", LanguageTypeScript},
		{"DocsReader.tsx", LanguageTypeScript},
		{"ApiDocs.swift", LanguageSwift},
	} {
		words, _ := Words(testCase.fileName, testCase.language)
		finding, found := Abbreviated(words, nil)
		if !found || finding.Entry.Abbreviation != "docs" {
			test.Errorf("%s: %+v (found %t), want the word \"docs\"", testCase.fileName, finding.Entry, found)
			continue
		}
		id, values := Advice(testCase.fileName, finding.Entry)
		if id != AbbreviatedFileName || values["expansions"] != `"documentation"` {
			test.Errorf("%s: %s %v, want documentation", testCase.fileName, id, values)
		}
	}
	for _, fileName := range []string{
		"GoogleDocsApi.ts", "GoogleDocsCommandLineInterface.ts", "googleDocs.ts", "documentation.ts",
		"docstring.ts", "Docstring.ts", "doc.ts", "DocReader.ts",
	} {
		words, _ := Words(fileName, LanguageTypeScript)
		if finding, found := Abbreviated(words, nil); found {
			test.Errorf("%s: reported %q, and it passes", fileName, finding.Entry.Abbreviation)
		}
	}
	words, _ := Words("GoogleDocsCfg.ts", LanguageTypeScript)
	if finding, found := Abbreviated(words, nil); !found || finding.Entry.Abbreviation != "cfg" {
		test.Errorf("GoogleDocsCfg.ts: %+v (found %t), want the word \"cfg\"", finding.Entry, found)
	}
	if _, found := abbreviation.Policy.Find("docsData"); found {
		test.Errorf("the shared vocabulary judges docs, which the identifier rules would then read")
	}
}

// A folder's name is judged by policy/NameWords.json alone, every word of it, and a brand spelled across the folder
// and its parent passes, so google/docs is Google Docs and docs/ anywhere else is documentation (#zyq63kk). A word
// only the shared vocabulary holds is a folder's plain word: Op art, a stage prop, `src` until its own census.
func TestFolderAbbreviated(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		parent string
		folder string
		word   string
	}{
		{"", "docs", "docs"},
		{"src", "docs", "docs"},
		{"web", "api-docs", "docs"},
		{"modules", "Docs", "docs"},
		{"docs", "docs", "docs"},
		// A brand's letters in the parent alone do not make the folder the brand's.
		{"google", "api-docs", "docs"},
		{"googledrive", "docs", "docs"},
	} {
		finding, found := FolderAbbreviated(testCase.parent, testCase.folder, nil)
		if !found || finding.Entry.Abbreviation != testCase.word {
			test.Errorf("%s/%s: %+v (found %t), want the word %q", testCase.parent, testCase.folder, finding.Entry,
				found, testCase.word)
			continue
		}
		id, values := FolderAdvice(testCase.folder, finding.Entry)
		if id != AbbreviatedFolderName || values["folder"] != testCase.folder || values["word"] != testCase.word {
			test.Errorf("%s/%s: %s %v", testCase.parent, testCase.folder, id, values)
		}
	}
	for _, testCase := range []struct {
		parent string
		folder string
	}{
		{"", "documentation"},
		{"format", "doc"},
		{"google", "docs"},
		{"Google", "Docs"},
		{"", "_components"},
		{"", "docstrings"},
		{"modules", "utilities"},
		{"styles", "op-art-one"},
		{"universes", "stage-prop"},
		{"", "src"},
		{"", "cfg"},
		{"modules", "msg_queue"},
	} {
		if finding, found := FolderAbbreviated(testCase.parent, testCase.folder, nil); found {
			test.Errorf("%s/%s: reported %q, and it passes", testCase.parent, testCase.folder,
				finding.Entry.Abbreviation)
		}
	}
	if _, found := FolderAbbreviated("", "no_var_docs", NewRuleNames([]string{"no-var"})); !found {
		test.Errorf("a folder spelling a rule's name beside its own words is judged on them")
	}
	if _, found := FolderAbbreviated("", "require_docs_link", NewRuleNames([]string{"require-docs-link"})); found {
		test.Errorf("a folder named for a registered rule holds the rule's name, which is not its author's")
	}
}

// `vars` is judged in folder and file names as in identifiers, whole, as a prefix and as a segment, and asks rather
// than renames, since it can mean JavaScript's var keyword (#1ykwdb9): a folder through policy/NameWords.json, a file
// through the shared vocabulary. Loom's wire/vars/ is the folder Kirk saw. A rule's name holding it is the rule's.
func TestTheVariablesAbbreviationIsJudgedInFolderAndFileNames(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct{ parent, folder string }{
		{"wire", "vars"},
		{"", "env-vars"},
		{"", "vars_reader"},
		{"", "EnvVars"},
	} {
		finding, found := FolderAbbreviated(testCase.parent, testCase.folder, nil)
		if !found || finding.Entry.Abbreviation != "vars" {
			test.Errorf("%s/%s: %+v (found %t), want the word \"vars\"", testCase.parent, testCase.folder,
				finding.Entry, found)
			continue
		}
		id, values := FolderAdvice(testCase.folder, finding.Entry)
		if id != AbbreviatedFolderNameAdvice || !strings.Contains(values["advice"], "var keyword") {
			test.Errorf("%s/%s: %s %v, want the advice naming the keyword", testCase.parent, testCase.folder, id,
				values)
		}
	}
	for _, testCase := range []struct {
		fileName string
		language Language
	}{
		{"vars.ts", LanguageTypeScript},
		{"env_vars.go", LanguageGo},
		{"VarsReader.swift", LanguageSwift},
	} {
		words, _ := Words(testCase.fileName, testCase.language)
		finding, found := Abbreviated(words, nil)
		if !found || finding.Entry.Abbreviation != "vars" {
			test.Errorf("%s: %+v (found %t), want the word \"vars\"", testCase.fileName, finding.Entry, found)
			continue
		}
		if id, _ := Advice(testCase.fileName, finding.Entry); id != AbbreviatedFileNameAdvice {
			test.Errorf("%s: %s, want the advice", testCase.fileName, id)
		}
	}
	for _, folder := range []string{"variables", "environment-variables", "varsity"} {
		if finding, found := FolderAbbreviated("", folder, nil); found {
			test.Errorf("%s: reported %q, and it passes", folder, finding.Entry.Abbreviation)
		}
	}
	if _, found := FolderAbbreviated("", "no_unused_vars", NewRuleNames([]string{"no-unused-vars"})); found {
		test.Errorf("a folder named for a registered rule holds the rule's name, which is not its author's")
	}
	words, _ := Words("no_unused_vars.go", LanguageGo)
	if _, found := Abbreviated(words, NewRuleNames([]string{"no-unused-vars"})); found {
		test.Errorf("a file named for a registered rule holds the rule's name, which is not its author's")
	}
}

// The message names every meaning of a word with more than one and asks for whichever it means, and a word with no
// expansion gives its entry's advice.
func TestAdvice(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName string
		language Language
		id       string
		values   map[string]string
	}{
		{"cfg.go", LanguageGo, AbbreviatedFileName, map[string]string{
			"fileName": "cfg.go", "word": "cfg", "expansions": `"configuration"`,
		}},
		{"pos_label.go", LanguageGo, AbbreviatedFileName, map[string]string{
			"fileName": "pos_label.go", "word": "pos", "expansions": `"position" or "positive", whichever it means`,
		}},
		{"res_body.go", LanguageGo, AbbreviatedFileName, map[string]string{
			"fileName":   "res_body.go",
			"word":       "res",
			"expansions": `"result", "response", "resource" or "reserve", whichever it means`,
		}},
		{"sb.go", LanguageGo, AbbreviatedFileNameAdvice, map[string]string{
			"fileName": "sb.go", "word": "sb", "advice": "Spell out what it holds.",
		}},
	}
	for _, testCase := range cases {
		words, _ := Words(testCase.fileName, testCase.language)
		finding, found := Abbreviated(words, nil)
		if !found {
			test.Errorf("%s: not found", testCase.fileName)
			continue
		}
		id, values := Advice(testCase.fileName, finding.Entry)
		if id != testCase.id || !maps.Equal(values, testCase.values) {
			test.Errorf("%s: %s %v, want %s %v", testCase.fileName, id, values, testCase.id, testCase.values)
		}
	}
}

// The suffix go/build reads, and none where it reads none.
func TestToolchainSuffix(test *testing.T) {
	test.Parallel()
	for stem, want := range map[string]string{
		"registry_linux":       "_linux",
		"registry_amd64":       "_amd64",
		"registry_linux_amd64": "_linux_amd64",
		"registry_unix":        "",
		"linux":                "",
		"registry":             "",
	} {
		if got := ToolchainSuffix(stem); got != want {
			test.Errorf("ToolchainSuffix(%q) = %q, want %q", stem, got, want)
		}
	}
}

// A foreign folder's files are someone else's or a test's input, and any other folder's are the project's.
func TestIsForeignFolder(test *testing.T) {
	test.Parallel()
	for folder, want := range map[string]bool{
		"node_modules": true,
		"vendor":       true,
		"third_party":  true,
		"testdata":     true,
		"source":       false,
		"vendors":      false,
	} {
		if got := IsForeignFolder(folder); got != want {
			test.Errorf("IsForeignFolder(%q) = %t, want %t", folder, got, want)
		}
	}
}

// registeredForTest stands in for a language's registry, upstream names and namespaced ones among them.
var registeredForTest = NewRuleNames([]string{
	"max-lines",
	"no-var",
	"max-classes-per-file",
	"@typescript-eslint/no-unused-vars",
	"structure/react-component-no-forward-ref",
	"boundaries/dependencies",
	"react-hooks/config",
})

// A file named for a registered rule is the rule's name, not an abbreviation, in any convention and with any suffix
// a tool reads, and a word the house adds after it, Rule or Test, is judged like any other and passes.
func TestAbbreviatedLeavesARegisteredRulesNameAlone(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName string
		language Language
	}{
		{"max_lines.go", LanguageGo},
		{"max_lines_register.go", LanguageGo},
		{"no_var_test.go", LanguageGo},
		{"no_unused_vars_fix_corpus_test.go", LanguageGo},
		{"react_component_no_forward_ref_test.go", LanguageGo},
		{"MaxClassesPerFileRule.ts", LanguageTypeScript},
		{"MaxClassesPerFileRule.node.test.ts", LanguageTypeScript},
		{"maxLines.ts", LanguageTypeScript},
	}
	for _, testCase := range cases {
		words, _ := Words(testCase.fileName, testCase.language)
		if finding, found := Abbreviated(words, registeredForTest); found {
			test.Errorf("%s: reported %q, and it names a registered rule", testCase.fileName,
				finding.Entry.Abbreviation)
		}
		// Without the registry the same name reports, so the registry is what passes it.
		if _, found := Abbreviated(words, nil); !found {
			test.Errorf("%s: passes with no registry, so this case proves nothing", testCase.fileName)
		}
	}
}

// A name holding a rule's name and more is judged on the more, so a rule's name never hides an abbreviation beside it.
func TestAbbreviatedJudgesTheWordsBesideARulesName(test *testing.T) {
	test.Parallel()
	cases := []struct {
		fileName string
		language Language
		word     string
	}{
		{"no_var_cfg.go", LanguageGo, "cfg"},
		{"msg_max_lines.go", LanguageGo, "msg"},
		{"MaxLinesCfg.ts", LanguageTypeScript, "cfg"},
		// A rule named in one word, react-hooks/config, is never set aside: the word is the file's own, alone or not.
		{"config.go", LanguageGo, "config"},
		{"config_test.go", LanguageGo, "config"},
		{"config_loader.go", LanguageGo, "config"},
	}
	for _, testCase := range cases {
		words, _ := Words(testCase.fileName, testCase.language)
		finding, found := Abbreviated(words, registeredForTest)
		if !found || finding.Entry.Abbreviation != testCase.word {
			test.Errorf("%s: %+v (found %t), want the word %q", testCase.fileName, finding.Entry, found, testCase.word)
		}
	}
	// helpers is a word in full; and dependencies, a rule's one-word name, is the file's own word, so the whole name is
	// judged, as the identifier dependenciesRefCurrent would be, and passes.
	for _, fileName := range []string{"no_var_helpers.go", "dependencies_ref_current_test.go"} {
		words, _ := Words(fileName, LanguageGo)
		if finding, found := Abbreviated(words, registeredForTest); found {
			test.Errorf("%s: reported %q, and it passes", fileName, finding.Entry.Abbreviation)
		}
	}
}
