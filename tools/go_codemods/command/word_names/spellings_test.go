package main

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/global_rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
)

// spellingsFixture is a module holding a ported rule's names, its tests, and names that cite JavaScript, CSS and
// React, each beside what a guard must keep.
var spellingsFixture = map[string]string{
	"go.work":                    "go 1.27\n\nuse .\n",
	"go.mod":                     "module fixture\n\ngo 1.27\n",
	"internal/lint/rule/rule.go": "package rule\n\n// Rule is a ported rule, registered under its Name.\ntype Rule struct{ Name string }\n",
	"internal/lint/rules/core/no_unused_vars.go": `package core

import "fixture/internal/lint/rule"

// NoUnusedVars ports no-unused-vars.
var NoUnusedVars = rule.Rule{Name: "no-unused-vars"}

// OneVar ports one-var, and OneVariable already names something else.
var OneVar = rule.Rule{Name: "one-var"}

// OneVariable takes the name OneVar would expand to.
func OneVariable() string { return OneVar.Name }

var pkg = rule.Rule{Name: "pkg"}

func pkgName() string { return pkg.Name }
`,
	"internal/lint/rules/core/no_unused_vars_test.go": `package core

import "testing"

func TestNoUnusedVarsFires(test *testing.T) {
	test.Parallel()
	if NoUnusedVars.Name != "no-unused-vars" {
		test.Fatal(NoUnusedVars.Name)
	}
}

// The gate runs this one by name.
func TestNoUnusedVarsStaysSilent(test *testing.T) { test.Parallel() }

var gateArguments = []string{"-run=^TestNoUnusedVarsStaysSilent$"}
`,
	"internal/lint/rules/core/sort_vars.go": `package core

import (
	"encoding/json"

	"fixture/internal/lint/rule"
)

// The options decode the way NoUnusedVars and SortVars decode theirs, and this sentence runs on long enough that a
// respelling pushes it past the limit, so the rest flows on.

var SortVars = rule.Rule{Name: "sort-vars"}

// sortVarsSettings is decoded from the rule's options, by its fields' names.
type sortVarsSettings struct{ SortVarsIgnoreCase bool }

// decodeSortVars reads the options the way NoUnusedVars does, unlike OneVar, and unlike css's charCodeAt.
func decodeSortVars(data []byte) bool {
	var settings sortVarsSettings
	_ = json.Unmarshal(data, &settings)
	return settings.SortVarsIgnoreCase
}
`,
	"internal/format/css/tokenize.go": `package css

// charCodeAt is css.charCodeAt(index), -1 past either end.
func charCodeAt(css string, index int) int {
	if index < 0 || index >= len(css) {
		return -1
	}
	return int(css[index])
}

func isVarFunctionNode(value string) bool { return value == "var" && charCodeAt(value, 0) > 0 }

// propTypeEntry is one declared prop.
type propTypeEntry struct{ required bool }

var (
	messageUnexpectedConst = "const"
	messageUnexpectedVar   = "var"
)

// isPostcssSimpleVarNode has a doc that cites nothing, and no note.
func isPostcssSimpleVarNode(value string) bool { return value == "$$" }

// decodeURIComponent is upstream's global.
func decodeURIComponent(text string) string { return text }

// charAt is text.charAt(index).
func charAt(text string, index int) string { return text[index : index+1] }

// Use builds a propTypeEntry, and calls charCodeAt as JavaScript would.
func Use() int {
	entry := propTypeEntry{}
	_ = entry.required
	_ = isPostcssSimpleVarNode(messageUnexpectedConst + messageUnexpectedVar)
	_ = decodeURIComponent(charAt("a", 0))
	if isVarFunctionNode("var") {
		return 1
	}
	return 0
}
`,
	"internal/lint/rules/core/one_var_checker.go": `package core

// oneVarChecker walks one file.
type oneVarChecker struct{ count int }

func (checker *oneVarChecker) visit() int {
	checker.count++
	return checker.count
}
`,
	"internal/lint/rules/core/one_var_checker_test.go": `package core

import "testing"

func TestCheckerVisitsOnce(test *testing.T) {
	test.Parallel()
	if checker := (&oneVarChecker{}); checker.visit() != 1 {
		test.Fatal("visit")
	}
}
`,
	"internal/lint/rules/core/sort_vars_group.go": `package core

type sortVarsGroup struct{ names []string }

func (group sortVarsGroup) size() int { return len(group.names) }
`,
	"internal/lint/rules/core/sort_variables_group.go": "package core\n\n// The name sort_vars_group.go would move to is taken.\n",
	"internal/lint/rules/structure/react_component_require_properties_parameter.go": `package structure

// messageUsePropertiesNotPropsText is the finding's catalog text.
var messageUsePropertiesNotPropsText = "use properties"

// messageUsePropertiesNotProps is the finding.
func messageUsePropertiesNotProps() string { return messageUsePropertiesNotPropsText }
`,
	"internal/lint/globals.go": `package lint

// globals are JavaScript's globals and string methods, by their JavaScript names.
var globals = map[string]bool{"decodeURIComponent": true, "charAt": true}
`,
}

// spellingsExternals is the fixture's external table, shaped as external.go's.
var spellingsExternals = map[string]externalName{
	"internal/format/css/tokenize.go:charCodeAt": {reason: javaScriptMethod, spelling: "charCodeAt"},
	"internal/format/css/tokenize.go:isVarFunctionNode": {reason: cssVariable, spelling: "var",
		note: "isVariableFunctionNode is whether a value calls CSS's var()."},
	"internal/format/css/tokenize.go:propTypeEntry": {reason: reactPropTypes, spelling: "propTypes",
		note: "Upstream reads it from a component's propTypes."},
	"internal/format/css/tokenize.go:messageUnexpectedVar": {reason: javaScriptVariable, spelling: "var",
		note: "messageUnexpectedVariable is upstream's unexpectedVar message: a `var` declaration."},
	"internal/format/css/tokenize.go:isPostcssSimpleVarNode": {reason: cssVariable, spelling: "isPostcssSimpleVarNode"},
	"internal/format/css/tokenize.go:decodeURIComponent": {
		reason:   "JavaScript's global",
		spelling: "decodeURIComponent",
		note:     "It ports JavaScript's decodeURIComponent.",
		newName:  "decodeUriComponent",
	},
	"internal/format/css/tokenize.go:charAt": {reason: javaScriptMethod, spelling: "charAt"},
	"internal/lint/rules/structure/react_component_require_properties_parameter.go:messageUsePropertiesNotProps": {
		reason:   "the binding `props`",
		spelling: "props",
		note:     "It asks for `properties` where a parameter is named `props`.",
	},
}

func writeSpellingsFixture(test *testing.T) string {
	test.Helper()
	root := test.TempDir()
	for name, contents := range spellingsFixture {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// Without the flag every name that spells a rule or an outside name is listed, with its reason, and none is
// proposed: the pass as it ran before the ruling.
func TestOwnedSpellingsAreListedWithoutTheFlag(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	root := writeSpellingsFixture(test)
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	candidates := findGlobalCandidates(loaded, loadedVocabulary, spellingsExternals, false)
	for _, proposal := range candidates.proposals {
		test.Errorf("proposed %s -> %s without the flag", proposal.Object.Name(), proposal.NewName)
	}
	reasons := map[string]string{}
	for _, name := range candidates.listed {
		reasons[name.name] = name.reason
	}
	for name, want := range map[string]string{
		"NoUnusedVars":          reasonUpstreamRule + "no-unused-vars",
		"TestNoUnusedVarsFires": reasonUpstreamRule + "no-unused-vars",
		"SortVars":              reasonUpstreamRule + "sort-vars",
		"charCodeAt":            reasonExternalPrefix + javaScriptMethod,
		"messageUnexpectedVar":  reasonExternalPrefix + javaScriptVariable,
	} {
		if reasons[name] != want {
			test.Errorf("%s: listed for %q, want %q", name, reasons[name], want)
		}
	}
	if _, listed := reasons["decodeURIComponent"]; listed {
		test.Errorf(
			"decodeURIComponent listed without the flag; the vocabulary does not flag it, so the pass never sees it",
		)
	}
}

// With the flag the names are proposed and the guards still hold; the accepted renames are applied, and each
// file reads exactly as a person would write it.
func TestOwnedSpellingsAreRenamedWithTheFlag(test *testing.T) {
	test.Parallel()
	loadedVocabulary := loadTestVocabulary(test)
	root := writeSpellingsFixture(test)
	loaded, loadError := workspace.Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	candidates := findGlobalCandidates(loaded, loadedVocabulary, spellingsExternals, true)
	listed := map[string]string{}
	for _, name := range candidates.listed {
		listed[name.name] = name.reason
	}
	for name, want := range map[string]string{
		// A name whose expansion is a keyword is listed, never numbered.
		"pkg": "would become a Go keyword",
		// An external name no doc comment names, with no note to give it one.
		"isPostcssSimpleVarNode": reasonNoNote,
	} {
		if listed[name] != want {
			test.Errorf("%s: listed for %q, want %q", name, listed[name], want)
		}
	}
	result := global_rename.Plan(loaded, candidates.proposals, global_rename.Options{})
	respellings := keepExternalSpellings(result, candidates.externals)
	citationEdits(loaded, result, candidates.externals)
	if reflowed, leftLong := reflowLongComments(loaded, result); reflowed != 1 || leftLong != 0 {
		test.Errorf("reflowed %d paragraphs, %d lines left long; want 1, 0", reflowed, leftLong)
	}

	refused := map[string]string{}
	for _, rejection := range result.Rejections {
		refused[rejection.OldName] = rejection.Class
	}
	for name, want := range map[string]string{
		// OneVariable is taken.
		"OneVar": "conflict: scope",
		// The gate runs it by name.
		"TestNoUnusedVarsStaysSilent": "string literal",
		// json reads the field by its name, and no tag may be added here.
		"SortVarsIgnoreCase": "encoding guard",
	} {
		if refused[name] != want {
			test.Errorf("%s: refused as %q, want %q", name, refused[name], want)
		}
	}
	accepted := map[string]string{}
	for _, renamed := range result.Accepted {
		accepted[renamed.OldName] = renamed.NewName
	}
	for name, want := range map[string]string{
		"NoUnusedVars":          "NoUnusedVariables",
		"TestNoUnusedVarsFires": "TestNoUnusedVariablesFires",
		"SortVars":              "SortVariables",
		"sortVarsSettings":      "sortVariablesSettings",
		"decodeSortVars":        "decodeSortVariables",
		"charCodeAt":            "characterCodeAt",
		"isVarFunctionNode":     "isVariableFunctionNode",
		"propTypeEntry":         "propertyTypeEntry",
		"messageUnexpectedVar":  "messageUnexpectedVariable",
		"decodeURIComponent":    "decodeUriComponent",
		"oneVarChecker":         "oneVariableChecker",
		"sortVarsGroup":         "sortVariablesGroup",
		// An override's name, and the unflagged name it keeps paired, which only an override proposes.
		"messageUsePropertiesNotProps":     "messageUsePropertiesParameter",
		"messageUsePropertiesNotPropsText": "messageUsePropertiesParameterText",
		// The table of globals spells both, and refuses neither: nothing reads a function's name at run time.
		"charAt": "characterAt",
	} {
		if accepted[name] != want {
			test.Errorf("%s: accepted as %q, want %q", name, accepted[name], want)
		}
	}
	outcomes := map[string]string{}
	for _, respelling := range respellings {
		outcomes[respelling.oldName] = respelling.outcome
	}
	for name, want := range map[string]string{
		"charCodeAt": outcomeSpellingNamed, "isVarFunctionNode": outcomeNoteAsDoc, "propTypeEntry": outcomeNoteAdded,
		"messageUnexpectedVar": outcomeNoteAsDoc, "decodeURIComponent": outcomeNoteAdded,
	} {
		if outcomes[name] != want {
			test.Errorf("%s: doc outcome %q, want %q", name, outcomes[name], want)
		}
	}

	moves := planFileMoves(loaded, result, candidates.proposals)
	mismatches, applyError := global_rename.Apply(loaded, root, nil, result, bufio.NewWriter(io.Discard))
	if applyError != nil || mismatches != 0 {
		test.Fatalf("apply: %d mismatches, %v", mismatches, applyError)
	}
	if moveError := moveFiles(moves); moveError != nil {
		test.Fatal(moveError)
	}
	// A file named for a renamed type moves with it, its test beside it; a move onto a taken name is refused.
	for name, want := range map[string]bool{
		"internal/lint/rules/core/one_var_checker.go":           false,
		"internal/lint/rules/core/one_variable_checker.go":      true,
		"internal/lint/rules/core/one_var_checker_test.go":      false,
		"internal/lint/rules/core/one_variable_checker_test.go": true,
		"internal/lint/rules/core/sort_vars_group.go":           true,
		"internal/lint/rules/core/no_unused_vars.go":            true,
	} {
		_, statError := os.Stat(filepath.Join(root, name))
		if exists := statError == nil; exists != want {
			test.Errorf("%s: exists %v, want %v", name, exists, want)
		}
	}
	refusedMoves := 0
	for _, move := range moves {
		if move.refusal != "" {
			refusedMoves++
		}
	}
	if len(moves) != 3 || refusedMoves != 1 {
		test.Errorf("planned %d moves, %d refused; want 3, 1 refused: %+v", len(moves), refusedMoves, moves)
	}
	read := func(name string) string {
		contents, readError := os.ReadFile(filepath.Join(root, name))
		if readError != nil {
			test.Fatal(readError)
		}
		return string(contents)
	}
	wantTokenize := `package css

// characterCodeAt is css.charCodeAt(index), -1 past either end.
func characterCodeAt(css string, index int) int {
	if index < 0 || index >= len(css) {
		return -1
	}
	return int(css[index])
}

// isVariableFunctionNode is whether a value calls CSS's var().
func isVariableFunctionNode(value string) bool {
	return value == "var" && characterCodeAt(value, 0) > 0
}

// propertyTypeEntry is one declared prop.
//
// Upstream reads it from a component's propTypes.
type propertyTypeEntry struct{ required bool }

var (
	messageUnexpectedConst = "const"
	// messageUnexpectedVariable is upstream's unexpectedVar message: a ` + "`var`" + ` declaration.
	messageUnexpectedVariable = "var"
)

// isPostcssSimpleVarNode has a doc that cites nothing, and no note.
func isPostcssSimpleVarNode(value string) bool { return value == "$$" }

// decodeUriComponent is upstream's global.
//
// It ports JavaScript's decodeURIComponent.
func decodeUriComponent(text string) string { return text }

// characterAt is text.charAt(index).
func characterAt(text string, index int) string { return text[index : index+1] }

// Use builds a propertyTypeEntry, and calls charCodeAt as JavaScript would.
func Use() int {
	entry := propertyTypeEntry{}
	_ = entry.required
	_ = isPostcssSimpleVarNode(messageUnexpectedConst + messageUnexpectedVariable)
	_ = decodeUriComponent(characterAt("a", 0))
	if isVariableFunctionNode("var") {
		return 1
	}
	return 0
}
`
	if got := read("internal/format/css/tokenize.go"); got != wantTokenize {
		test.Errorf("tokenize.go:\n%s\nwant:\n%s", got, wantTokenize)
	}
	wantRule := `package core

import "fixture/internal/lint/rule"

// NoUnusedVariables ports no-unused-vars.
var NoUnusedVariables = rule.Rule{Name: "no-unused-vars"}
`
	if got := read("internal/lint/rules/core/no_unused_vars.go"); !strings.HasPrefix(got, wantRule) {
		test.Errorf("no_unused_vars.go:\n%s\nwant it to open with:\n%s", got, wantRule)
	}
	if got := read(
		"internal/lint/rules/core/no_unused_vars_test.go",
	); !strings.Contains(got, "func TestNoUnusedVariablesFires(") ||
		!strings.Contains(got, "func TestNoUnusedVarsStaysSilent(") ||
		!strings.Contains(got, `"-run=^TestNoUnusedVarsStaysSilent$"`) {
		test.Errorf("no_unused_vars_test.go:\n%s", got)
	}
	// A citation of a renamed name is respelled; of a refused one, and of an external one, kept.
	wantCitation := "// decodeSortVariables reads the options the way NoUnusedVariables does, unlike OneVar, and unlike css's charCodeAt."
	// A citation that pushes its line past 120 columns reflows the paragraph from that line on.
	wantReflow := "// The options decode the way NoUnusedVariables and SortVariables decode theirs, and this " +
		"sentence runs on long enough\n" +
		"// that a respelling pushes it past the limit, so the rest flows on.\n"
	if got := read(
		"internal/lint/rules/core/sort_vars.go",
	); !strings.Contains(got, wantCitation) || !strings.Contains(got, wantReflow) {
		test.Errorf("sort_vars.go:\n%s\nwant it to hold:\n%s\nand:\n%s", got, wantCitation, wantReflow)
	}
	if got := read("internal/lint/globals.go"); got != spellingsFixture["internal/lint/globals.go"] {
		test.Errorf("a string literal changed:\n%s", got)
	}
}
