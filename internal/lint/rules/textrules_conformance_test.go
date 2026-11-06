// Package rules holds the TypeScript adapter of the text rules' conformance corpus (#4b337jk). It sits above
// the rule packages rather than in one, because it runs a rule of any of them, found by its registered name.
package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/system-inc/cohere/conformance/textrules"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// textRulesHouseSets are the sets the TypeScript adapter reads each rule's status and options from: the ones
// the confirmation probe extended (textrules-confirmation.md, "How it was run"), so a rule runs with what the
// house gives it, read through the configuration layer rather than copied by hand.
var textRulesHouseSets = []string{configuration.TypeScriptSetName, configuration.SetPrefix + "style"}

// textRulesProbeRules are the rules the probe turned on in its own settings, beside the sets, because the
// sets leave them off: `nexus/consistency-no-abbreviated-identifier` lives in `cohere:system-inc/base`. The
// adapter turns them on the same way, with a bare severity.
var textRulesProbeRules = map[string]string{"nexus/consistency-no-abbreviated-identifier": "error"}

// textRulesHouse is the house configuration loaded once per test, which each case's own file resolves through.
type textRulesHouse struct {
	loaded *configuration.Config
	// directory is where the settings file sits, which a case's project-relative file name resolves under, so
	// the house's overrides (`**/*.generated.*` under max-lines) reach it as they reach a project's file.
	directory string
}

// loadTextRulesHouse loads the house sets, and the probe's rules, through a settings file that extends them.
func loadTextRulesHouse(test *testing.T) textRulesHouse {
	test.Helper()
	directory := test.TempDir()
	settings, encodeError := json.Marshal(map[string]any{"extends": textRulesHouseSets, "rules": textRulesProbeRules})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	settingsPath := filepath.Join(directory, "CohereSettings.json")
	if writeError := os.WriteFile(settingsPath, settings, 0o600); writeError != nil {
		test.Fatal(writeError)
	}
	loaded, loadForError := configuration.LoadFor(settingsPath, registry.Names())
	if loadForError != nil {
		test.Fatalf("loading the house sets %v: %v", textRulesHouseSets, loadForError)
	}
	return textRulesHouse{loaded: loaded, directory: directory}
}

// optionsFor resolves a rule for a case's own file, and decodes its options as the house gives them there,
// with the case's own options laid over them key by key, through the registry's decoder, exactly as a lint
// run decodes them. It reports false where the rule resolves off for the file, so the rule does not run.
func (house textRulesHouse) optionsFor(test *testing.T, ruleName string, testCase textrules.Case) (rule.Bound, bool) {
	test.Helper()
	resolved := house.loaded.Resolve(
		filepath.Join(house.directory, filepath.FromSlash(testCase.FileName(textrules.EngineTypeScript))),
	)
	switch status, _ := resolved.StatusOf(ruleName); status {
	case configuration.StatusUnconfigured:
		test.Fatalf("the house sets %v never configure %s, so there is nothing to run it with", textRulesHouseSets,
			ruleName)
	case configuration.StatusScopedOff, configuration.StatusFileIgnored:
		return rule.Bound{}, false
	}
	elements := resolved.RawOptionsFor(ruleName)
	if len(testCase.Options) > 0 {
		if len(elements) > 1 {
			test.Fatalf("%s takes %d option elements, and a case's options are one object laid over one",
				ruleName, len(elements))
		}
		merged := map[string]json.RawMessage{}
		if len(elements) == 1 {
			if decodeError := json.Unmarshal(elements[0], &merged); decodeError != nil {
				test.Fatalf(
					"%s's house options are not an object a case's options can be laid over: %v",
					ruleName,
					decodeError,
				)
			}
		}
		for key, value := range testCase.Options {
			merged[key] = value
		}
		element, encodeError := json.Marshal(merged)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		elements = []json.RawMessage{element}
	}
	decoded, decodeError := registry.Options().Decode(ruleName, elements)
	if decodeError != nil {
		test.Fatalf("decoding %s's options: %v", ruleName, decodeError)
	}
	return decoded, true
}

// textRulesRule is the registered rule a family names for TypeScript.
func textRulesRule(test *testing.T, corpus *textrules.Corpus, family string) rule.Entry {
	test.Helper()
	entry, found := corpus.RuleFor(family, textrules.EngineTypeScript)
	if !found {
		test.Fatalf("the corpus has no family %s", family)
	}
	if entry.None != "" {
		test.Fatalf("family %s has no TypeScript rule (%s), and has a TypeScript case", family, entry.None)
	}
	for _, registered := range registry.All() {
		if registered.Name == entry.Rule {
			return registered
		}
	}
	test.Fatalf(
		"families.json names %s for TypeScript in %s, and no rule is registered by that name",
		entry.Rule,
		family,
	)
	return rule.Entry{}
}

// runTextRulesCase runs a case's rule over its TypeScript source, and returns each finding as a byte range. A
// rule the house turns off for the case's file does not run, and finds nothing, as in a lint run.
func runTextRulesCase(
	test *testing.T,
	corpus *textrules.Corpus,
	house textRulesHouse,
	testCase textrules.Case,
) []textrules.Range {
	test.Helper()
	subject := textRulesRule(test, corpus, testCase.Family)
	bound, runs := house.optionsFor(test, subject.Name, testCase)
	if !runs {
		return nil
	}
	fileName := testCase.AbsoluteFileName(textrules.EngineTypeScript)
	result := rule_testing.RunBound(test, bound, fileName, testCase.Sources[textrules.EngineTypeScript])
	found := make([]textrules.Range, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		found = append(found, textrules.Range{Start: diagnostic.Range.Pos(), End: diagnostic.Range.End()})
	}
	return found
}

// Every TypeScript case in the corpus, through the rule its family names, held to the case's expectation and
// the ledger: a failure needs a row of its kind, a pass must have none, and a control's count and start
// lines must hold whatever the ledger says.
func TestTextRulesConformanceTypeScript(test *testing.T) {
	test.Parallel()

	corpus, loadError := textrules.Load()
	if loadError != nil {
		test.Fatal(loadError)
	}
	house := loadTextRulesHouse(test)
	cases := corpus.CasesFor(textrules.EngineTypeScript)
	if len(cases) == 0 {
		test.Fatal("the corpus has no TypeScript cases, so this adapter would be green over nothing")
	}

	// Counted after every case's comparison, so a case that returned early, or never ran, is missed by
	// count. Read in a cleanup, which runs once every parallel subtest has finished.
	var ran atomic.Int64
	test.Cleanup(func() {
		if got := ran.Load(); got != int64(len(cases)) {
			test.Errorf("compared %d TypeScript cases, and the corpus has %d", got, len(cases))
		}
	})
	for _, testCase := range cases {
		test.Run(testCase.Id(), func(test *testing.T) {
			test.Parallel()
			found := runTextRulesCase(test, corpus, house, testCase)
			result := textrules.Compare(testCase, textrules.EngineTypeScript, found)
			ran.Add(1)
			if judgeError := corpus.Judge(textrules.EngineTypeScript, testCase, result); judgeError != nil {
				test.Error(judgeError)
			}
		})
	}
}

// textRulesComparatorFamily is the family whose control the comparator's own test runs on.
const textRulesComparatorFamily = "warning-comments"

// The comparison can fail: the control's own findings, held to deliberately wrong expectations, fail with
// the kind each is wrong in, and held to exactly what the rule reported, pass. So a green run above is the
// comparator agreeing, not a comparator unable to disagree.
func TestTextRulesConformanceTypeScriptComparatorCanFail(test *testing.T) {
	test.Parallel()

	corpus, loadError := textrules.Load()
	if loadError != nil {
		test.Fatal(loadError)
	}
	house := loadTextRulesHouse(test)
	// The warning-comments control, whose comment holds the word `fix`, a misplacement every engine's
	// finding there differs from.
	var control textrules.Case
	for _, testCase := range corpus.CasesFor(textrules.EngineTypeScript) {
		if testCase.Control && testCase.Family == textRulesComparatorFamily {
			control = testCase
		}
	}
	if control.Name == "" {
		test.Fatalf("the corpus has no TypeScript control in %s", textRulesComparatorFamily)
	}
	found := runTextRulesCase(test, corpus, house, control)
	if len(found) == 0 {
		test.Fatalf("the rule reported nothing on the control %s", control.Id())
	}
	source := control.Sources[textrules.EngineTypeScript]

	asReported := control
	asReported.Expect = nil
	for _, finding := range found {
		text := source[finding.Start:finding.End]
		asReported.Expect = append(asReported.Expect, textrules.Span{Covers: &text, Occurrence: 1})
	}
	if result := textrules.Compare(asReported, textrules.EngineTypeScript, found); !result.Passed() {
		test.Errorf("held to exactly what it reported, the control failed: %s", result.Detail)
	}

	clean := control
	clean.Expect = []textrules.Span{}
	if result := textrules.Compare(clean, textrules.EngineTypeScript, found); result.Failure != textrules.KindVerdict {
		test.Errorf("expecting the control clean was not a verdict failure (%q): %s", result.Failure, result.Detail)
	}

	wrongWord := "fix"
	misplaced := control
	misplaced.Expect = []textrules.Span{{Covers: &wrongWord, Occurrence: 1}}
	if result := textrules.Compare(misplaced, textrules.EngineTypeScript, found); result.Failure != textrules.KindSpan {
		test.Errorf(
			"expecting the control over %q was not a span failure (%q): %s",
			wrongWord,
			result.Failure,
			result.Detail,
		)
	}
}
