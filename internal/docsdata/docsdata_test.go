package docsdata

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/docsdata/capture"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// moduleRoot is the directory the generated paths are relative to: go test runs in the package's own
// directory, two below it.
const moduleRoot = "../.."

func sourceInputs(t *testing.T) Inputs {
	t.Helper()
	inputs, err := SourceInputs(moduleRoot)
	if err != nil {
		t.Fatal(err)
	}
	return inputs
}

func built(t *testing.T, inputs Inputs) map[string][]byte {
	t.Helper()
	files, err := Build(inputs)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func staleAgainstCommitted(t *testing.T, files map[string][]byte) []string {
	t.Helper()
	stale, err := Stale(moduleRoot, files)
	if err != nil {
		t.Fatal(err)
	}
	return stale
}

// TestTheCommittedFilesAreCurrent: docs/data is what Build produces now, every file but cli.json, which
// needs the binary built and is checked by the generator's -check.
func TestTheCommittedFilesAreCurrent(t *testing.T) {
	if stale := staleAgainstCommitted(t, built(t, sourceInputs(t))); len(stale) > 0 {
		t.Errorf("stale: %v; run go run ./internal/docsdata/tools/generate", stale)
	}
}

// TestCheckSeesARegistryChange is -check's contract for the registry, in both directions: the committed
// files pass as they are, and fail once a rule is removed or added.
func TestCheckSeesARegistryChange(t *testing.T) {
	inputs := sourceInputs(t)
	if stale := staleAgainstCommitted(t, built(t, inputs)); len(stale) > 0 {
		t.Fatalf("the committed files are already stale (%v), so a change could not be told apart", stale)
	}

	removed := inputs
	removed.Registrations = inputs.Registrations[1:]
	if stale := staleAgainstCommitted(t, built(t, removed)); !slices.Contains(stale, RulesPath) {
		t.Errorf("removing %s from the registry left rules.json current: %v", inputs.Registrations[0].Rule.Name, stale)
	}

	added := inputs
	added.Registrations = append(slices.Clone(inputs.Registrations), rule.Registration{
		Rule: rule.Rule{Name: "nexus/docsdata-test-rule", Run: func(rule.Context, any) rule.Listeners { return nil }},
	})
	if stale := staleAgainstCommitted(t, built(t, added)); !slices.Contains(stale, RulesPath) {
		t.Errorf("adding a rule to the registry left rules.json current: %v", stale)
	}
}

// TestCheckSeesATierChange: a rule set resolving differently makes sets.json and rules.json stale.
func TestCheckSeesATierChange(t *testing.T) {
	inputs := sourceInputs(t)
	changedSet := inputs.SetNames[0]
	var changedRule string
	loadSet := inputs.LoadSet
	inputs.LoadSet = func(name string) (*configuration.Config, error) {
		resolved, err := loadSet(name)
		if err != nil || name != changedSet {
			return resolved, err
		}
		names := make([]string, 0, len(resolved.Rules))
		for ruleName := range resolved.Rules {
			names = append(names, ruleName)
		}
		sort.Strings(names)
		changedRule = names[0]
		setting := resolved.Rules[changedRule]
		setting.Severity = configuration.SeverityError
		if resolved.Rules[changedRule].Severity == configuration.SeverityError {
			setting.Severity = configuration.SeverityWarn
		}
		resolved.Rules[changedRule] = setting
		return resolved, nil
	}
	stale := staleAgainstCommitted(t, built(t, inputs))
	for _, path := range []string{SetsPath, RulesPath} {
		if !slices.Contains(stale, path) {
			t.Errorf("changing %s's severity in %s left %s current: %v", changedRule, changedSet, path, stale)
		}
	}
}

// TestEveryRegisteredRuleHasOneRow: rules.json names every registered rule exactly once, and nothing else.
func TestEveryRegisteredRuleHasOneRow(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot, RulesPath))
	if err != nil {
		t.Fatal(err)
	}
	var rules Rules
	if err := json.Unmarshal(committed, &rules); err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	for _, row := range rules.Rules {
		rows[row.Name]++
	}
	registered := map[string]bool{}
	for _, name := range registry.Names() {
		registered[name] = true
		if rows[name] != 1 {
			t.Errorf("%s is registered and has %d rows in rules.json", name, rows[name])
		}
	}
	for name := range rows {
		if !registered[name] {
			t.Errorf("rules.json has a row for %s, which is not registered", name)
		}
	}
	if rules.Count != len(rules.Rules) {
		t.Errorf("rules.json says count %d over %d rows", rules.Count, len(rules.Rules))
	}
}

// TestBuildIsDeterministic: two builds give the same bytes, so a regeneration diffs to nothing.
func TestBuildIsDeterministic(t *testing.T) {
	inputs := sourceInputs(t)
	first, second := built(t, inputs), built(t, inputs)
	if len(first) != len(second) {
		t.Fatalf("%d files, then %d", len(first), len(second))
	}
	for path, contents := range first {
		if !bytes.Equal(contents, second[path]) {
			t.Errorf("%s differs between two builds", path)
		}
	}
}

// TestEveryFieldHasASourceNote: rules.json says where each field of a row comes from, and names no field
// a row does not have.
func TestEveryFieldHasASourceNote(t *testing.T) {
	encoded, err := json.Marshal(RuleRow{Options: &RuleOptions{}, Swift: &SwiftVerdict{}, Sets: []RuleSetSeverity{{}}, MessageIds: []string{""},
		Namespace: "-", UpstreamName: "-", FixKind: "-"})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for field := range fields {
		if ruleSourceNotes[field] == "" {
			t.Errorf("rules.json rows carry %q and sourceNotes does not say where it comes from", field)
		}
	}
	for field := range ruleSourceNotes {
		if _, present := fields[field]; !present {
			t.Errorf("sourceNotes describes %q, which a row does not carry", field)
		}
	}
}

// TestTheHelpParserReadsWhatTheFlagPackageWrites: a flag set with every shape the binary uses, printed by
// the flag package itself, parses back to exactly what the set holds.
func TestTheHelpParserReadsWhatTheFlagPackageWrites(t *testing.T) {
	set := flag.NewFlagSet("cohere", flag.ContinueOnError)
	set.String("tsconfig", "tsconfig.json", "the tsconfig, quoted \"here\"")
	set.String("directory", "", "the root (default: the nearest tsconfig.json)")
	set.Bool("fix", false, "apply fixes only")
	set.Bool("on", true, "a boolean that defaults on")
	set.Int("fix-passes", 10, "how many passes")
	set.Duration("wait", 2*time.Second, "how long")
	set.String("profile", "", "write a profile to `file`, for pprof")
	set.String("explain", "", "a usage that runs\nonto a second line")

	var printed bytes.Buffer
	set.SetOutput(&printed)
	set.PrintDefaults()
	_, parsed, err := parseFlagHelp(printed.Bytes())
	if err != nil {
		t.Fatalf("%v\n%s", err, printed.String())
	}

	var want []CliFlag
	set.VisitAll(func(each *flag.Flag) {
		argument, usage := flag.UnquoteUsage(each)
		entry := CliFlag{Name: each.Name, Argument: argument, Usage: usage}
		if !map[string]bool{"": true, "false": true, "0": true, "0s": true}[each.DefValue] {
			entry.Default = each.DefValue
		}
		want = append(want, entry)
	})
	if len(parsed) != len(want) {
		t.Fatalf("parsed %d flags, the set holds %d:\n%s", len(parsed), len(want), printed.String())
	}
	for index := range want {
		if parsed[index] != want[index] {
			t.Errorf("flag %d: parsed %+v, want %+v", index, parsed[index], want[index])
		}
	}
}

// TestTheVerbHelpKeepsItsSynopsis: a verb's usage lines and summary are told apart from its flags.
func TestTheVerbHelpKeepsItsSynopsis(t *testing.T) {
	help := "usage: cohere rename <file>:<line>:<column> <newName>\n" +
		"       cohere rename <name> <newName>\n\n" +
		"Renames a symbol.\nNothing is written without --write.\n\n" +
		"  -write\n    \tapply the rename\n"
	cli, err := parseCli([]byte("  -fix\n    \tapply fixes only\n"), map[string][]byte{"rename": []byte(help)})
	if err != nil {
		t.Fatal(err)
	}
	verb := cli.Verbs[0]
	if strings.Join(verb.Usage, "|") != "cohere rename <file>:<line>:<column> <newName>|cohere rename <name> <newName>" {
		t.Errorf("usage %q", verb.Usage)
	}
	if verb.Summary != "Renames a symbol. Nothing is written without --write." {
		t.Errorf("summary %q", verb.Summary)
	}
	if len(verb.Flags) != 1 || verb.Flags[0].Name != "write" {
		t.Errorf("flags %+v", verb.Flags)
	}
}

// TestTheChangelogParsesEveryRelease: every release heading in CHANGELOG.md becomes one entry, and a
// second-level heading that is not a release refuses the parse.
func TestTheChangelogParsesEveryRelease(t *testing.T) {
	changelog, err := os.ReadFile(filepath.Join(moduleRoot, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseChangelog(changelog)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Count("\n"+string(changelog), "\n## "); len(entries) != want {
		t.Errorf("%d entries for %d release headings", len(entries), want)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Body) == "" {
			t.Errorf("%s has an empty body", entry.Version)
		}
	}
	if headingAnchor("1.0.0 (unreleased)") != "100-unreleased" {
		t.Errorf("anchor %q", headingAnchor("1.0.0 (unreleased)"))
	}
	if _, err := parseChangelog([]byte("# Changelog\n\n## Notes\n")); err == nil {
		t.Error("a second-level heading that is not a release parsed")
	}
}

// TestPickExamplesPrefersWhatAReaderCanRun: the shortest case run without options and without other
// files wins, a rule with an asserted fix shows its fixed source, and the message ids and fix kind cover
// every asserted case, those run with options included.
func TestPickExamplesPrefersWhatAReaderCanRun(t *testing.T) {
	finding := func(id string, fix bool) []capture.Finding {
		return []capture.Finding{{Line: 1, Column: 1, MessageId: id, Fix: fix}}
	}
	picked, _ := PickExamples([]capture.Record{
		{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{}`), Outcome: capture.OutcomeFindings, Findings: finding("withOptions", false)},
		{Rule: "r", File: "a.ts", Source: "xx", OtherFiles: 1, Outcome: capture.OutcomeFindings, Findings: finding("other", false)},
		{Rule: "r", File: "a.ts", Source: "longer one", Outcome: capture.OutcomeFixed, Findings: finding("fixed", true), FixedSource: "fixed"},
		{Rule: "r", File: "a.ts", Source: "xyz", Outcome: capture.OutcomeFindings, Findings: finding("plain", false)},
		{Rule: "r", File: "b.ts", Source: "clean, longer", Outcome: capture.OutcomeClean},
		{Rule: "r", File: "a.ts", Source: "clean", Outcome: capture.OutcomeClean},
	})
	examples := picked["r"]
	if examples.Firing == nil || examples.Firing.FixedSource != "fixed" {
		t.Errorf("a rule with an asserted fix should show it: %+v", examples.Firing)
	}
	if examples.Clean == nil || examples.Clean.Source != "clean" {
		t.Errorf("clean %+v", examples.Clean)
	}
	if strings.Join(examples.MessageIds(), ",") != "fixed,other,plain,withOptions" || examples.FixKind() != "fix" {
		t.Errorf("ids %v, fix kind %q", examples.MessageIds(), examples.FixKind())
	}

	unfixed, _ := PickExamples([]capture.Record{
		{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{}`), Outcome: capture.OutcomeFindings},
		{Rule: "r", File: "a.ts", Source: "xx", OtherFiles: 1, Outcome: capture.OutcomeFindings},
		{Rule: "r", File: "a.ts", Source: "xyz", Outcome: capture.OutcomeFindings},
	})
	if unfixed["r"].Firing.Source != "xyz" {
		t.Errorf("the case a reader can run as shown should win: %+v", unfixed["r"].Firing)
	}
}

// TestACaseRunWithOptionsIsNeverShown: a rule whose only asserted cases ran with options has no example,
// and its message ids still count.
func TestACaseRunWithOptionsIsNeverShown(t *testing.T) {
	picked, withOptions := PickExamples([]capture.Record{
		{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{"Mode": "TypeAnnotation"}`), Outcome: capture.OutcomeFindings,
			Findings: []capture.Finding{{MessageId: "only"}}},
		{Rule: "r", File: "a.ts", Source: "y", Options: json.RawMessage(`{"Mode": "TypeAnnotation"}`), Outcome: capture.OutcomeClean},
	})
	examples := picked["r"]
	if examples.Firing != nil || examples.Clean != nil {
		t.Errorf("a case run with options was shown: %+v", examples)
	}
	if strings.Join(examples.MessageIds(), ",") != "only" || withOptions != 2 {
		t.Errorf("ids %v, %d with options", examples.MessageIds(), withOptions)
	}
}

// TestPickExamplesCountsACaseOnce: one case asserted twice, as findings and then as its fixed source, or
// as findings by two tests, is counted once per outcome, and the fixed record is the one shown.
func TestPickExamplesCountsACaseOnce(t *testing.T) {
	findings := capture.Record{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{"a":1}`), Outcome: capture.OutcomeFindings}
	_, withOptions := PickExamples([]capture.Record{findings, findings})
	if withOptions != 1 {
		t.Errorf("one case asserted twice counted %d times", withOptions)
	}

	asserted := capture.Record{Rule: "r", File: "a.ts", Source: "debugger;", Outcome: capture.OutcomeFindings,
		Findings: []capture.Finding{{MessageId: "debugger", Fix: true}}}
	fixed := asserted
	fixed.Outcome, fixed.FixedSource = capture.OutcomeFixed, "\n"
	picked, _ := PickExamples([]capture.Record{asserted, fixed, asserted})
	if picked["r"].Firing == nil || picked["r"].Firing.FixedSource != "\n" {
		t.Errorf("the fixed record should be the one shown: %+v", picked["r"].Firing)
	}
}
