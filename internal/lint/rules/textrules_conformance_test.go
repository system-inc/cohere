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

// textRulesHouseSets are the sets the TypeScript adapter reads each rule's options from: the ones the
// confirmation probe extended (textrules-confirmation.md, "How it was run"), so a rule runs with the options
// the house gives it, read through the configuration layer rather than copied by hand.
var textRulesHouseSets = []string{configuration.TypeScriptSetName, configuration.SetPrefix + "style"}

// textRulesHouse is the house sets loaded once per test, for reading any rule's options from.
type textRulesHouse struct {
	resolved configuration.Resolved
}

// loadTextRulesHouse loads the house sets through a settings file that extends them.
func loadTextRulesHouse(t *testing.T) textRulesHouse {
	t.Helper()
	directory := t.TempDir()
	settings, err := json.Marshal(map[string][]string{"extends": textRulesHouseSets})
	if err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(directory, "CohereSettings.json")
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := configuration.LoadFor(settingsPath, registry.Names())
	if err != nil {
		t.Fatalf("loading the house sets %v: %v", textRulesHouseSets, err)
	}
	return textRulesHouse{resolved: loaded.Resolve(filepath.Join(directory, "case.ts"))}
}

// optionsFor decodes a rule's options as the house sets give them, with a case's own options laid over
// them key by key, through the registry's decoder, exactly as a lint run decodes them.
func (house textRulesHouse) optionsFor(t *testing.T, ruleName string, caseOptions map[string]json.RawMessage) any {
	t.Helper()
	if status, _ := house.resolved.StatusOf(ruleName); status == configuration.StatusUnconfigured {
		t.Fatalf("the house sets %v never configure %s, so there are no house options to run it with",
			textRulesHouseSets, ruleName)
	}
	elements := house.resolved.RawOptionsFor(ruleName)
	if len(caseOptions) > 0 {
		if len(elements) > 1 {
			t.Fatalf("%s takes %d option elements, and a case's options are one object laid over one",
				ruleName, len(elements))
		}
		merged := map[string]json.RawMessage{}
		if len(elements) == 1 {
			if err := json.Unmarshal(elements[0], &merged); err != nil {
				t.Fatalf("%s's house options are not an object a case's options can be laid over: %v", ruleName, err)
			}
		}
		for key, value := range caseOptions {
			merged[key] = value
		}
		element, err := json.Marshal(merged)
		if err != nil {
			t.Fatal(err)
		}
		elements = []json.RawMessage{element}
	}
	decoded, err := registry.Options().Decode(ruleName, elements)
	if err != nil {
		t.Fatalf("decoding %s's options: %v", ruleName, err)
	}
	return decoded
}

// textRulesRule is the registered rule a family names for TypeScript.
func textRulesRule(t *testing.T, corpus *textrules.Corpus, family string) rule.Rule {
	t.Helper()
	entry, found := corpus.RuleFor(family, textrules.EngineTypeScript)
	if !found {
		t.Fatalf("the corpus has no family %s", family)
	}
	if entry.None != "" {
		t.Fatalf("family %s has no TypeScript rule (%s), and has a TypeScript case", family, entry.None)
	}
	for _, registered := range registry.All() {
		if registered.Name == entry.Rule {
			return registered
		}
	}
	t.Fatalf("families.json names %s for TypeScript in %s, and no rule is registered by that name", entry.Rule, family)
	return rule.Rule{}
}

// runTextRulesCase runs a case's rule over its TypeScript source, and returns each finding as a byte range.
func runTextRulesCase(t *testing.T, corpus *textrules.Corpus, house textRulesHouse, testCase textrules.Case) []textrules.Range {
	t.Helper()
	subject := textRulesRule(t, corpus, testCase.Family)
	options := house.optionsFor(t, subject.Name, testCase.Options)
	fileName := "/project/" + testCase.FileName(textrules.EngineTypeScript)
	result := rule_testing.RunWithOptions(t, subject, fileName, testCase.Sources[textrules.EngineTypeScript], options)
	found := make([]textrules.Range, 0, len(result.Diagnostics))
	for _, diagnostic := range result.Diagnostics {
		found = append(found, textrules.Range{Start: diagnostic.Range.Pos(), End: diagnostic.Range.End()})
	}
	return found
}

// Every TypeScript case in the corpus, through the rule its family names, held to the case's expectation and
// the ledger: a failure needs a row of its kind, a pass must have none, and a control's count and start
// lines must hold whatever the ledger says.
func TestTextRulesConformanceTypeScript(t *testing.T) {
	t.Parallel()

	corpus, err := textrules.Load()
	if err != nil {
		t.Fatal(err)
	}
	house := loadTextRulesHouse(t)
	cases := corpus.CasesFor(textrules.EngineTypeScript)
	if len(cases) == 0 {
		t.Fatal("the corpus has no TypeScript cases, so this adapter would be green over nothing")
	}

	// Counted after every case's comparison, so a case that returned early, or never ran, is missed by
	// count. Read in a cleanup, which runs once every parallel subtest has finished.
	var ran atomic.Int64
	t.Cleanup(func() {
		if got := ran.Load(); got != int64(len(cases)) {
			t.Errorf("compared %d TypeScript cases, and the corpus has %d", got, len(cases))
		}
	})
	for _, testCase := range cases {
		t.Run(testCase.Id(), func(t *testing.T) {
			t.Parallel()
			found := runTextRulesCase(t, corpus, house, testCase)
			result := textrules.Compare(testCase, textrules.EngineTypeScript, found)
			ran.Add(1)
			if err := corpus.Judge(textrules.EngineTypeScript, testCase, result); err != nil {
				t.Error(err)
			}
		})
	}
}

// The comparison can fail: the control's own findings, held to deliberately wrong expectations, fail with
// the kind each is wrong in, and held to exactly what the rule reported, pass. So a green run above is the
// comparator agreeing, not a comparator unable to disagree.
func TestTextRulesConformanceTypeScriptComparatorCanFail(t *testing.T) {
	t.Parallel()

	corpus, err := textrules.Load()
	if err != nil {
		t.Fatal(err)
	}
	house := loadTextRulesHouse(t)
	var control textrules.Case
	for _, testCase := range corpus.CasesFor(textrules.EngineTypeScript) {
		if testCase.Control {
			control = testCase
			break
		}
	}
	if control.Name == "" {
		t.Fatal("the corpus has no TypeScript control")
	}
	found := runTextRulesCase(t, corpus, house, control)
	if len(found) == 0 {
		t.Fatalf("the rule reported nothing on the control %s", control.Id())
	}
	source := control.Sources[textrules.EngineTypeScript]

	asReported := control
	asReported.Expect = nil
	for _, finding := range found {
		text := source[finding.Start:finding.End]
		asReported.Expect = append(asReported.Expect, textrules.Span{Covers: &text, Occurrence: 1})
	}
	if result := textrules.Compare(asReported, textrules.EngineTypeScript, found); !result.Passed() {
		t.Errorf("held to exactly what it reported, the control failed: %s", result.Detail)
	}

	clean := control
	clean.Expect = []textrules.Span{}
	if result := textrules.Compare(clean, textrules.EngineTypeScript, found); result.Failure != textrules.KindVerdict {
		t.Errorf("expecting the control clean was not a verdict failure (%q): %s", result.Failure, result.Detail)
	}

	wrongWord := "fix"
	misplaced := control
	misplaced.Expect = []textrules.Span{{Covers: &wrongWord, Occurrence: 1}}
	if result := textrules.Compare(misplaced, textrules.EngineTypeScript, found); result.Failure != textrules.KindSpan {
		t.Errorf("expecting the control over %q was not a span failure (%q): %s", wrongWord, result.Failure, result.Detail)
	}
}
