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

	"github.com/system-inc/cohere/internal/benchresults"
	"github.com/system-inc/cohere/internal/docsdata/capture"
	goenginerules "github.com/system-inc/cohere/internal/goengine/rules"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// moduleRoot is the directory the generated paths are relative to: go test runs in the package's own
// directory, two below it.
const moduleRoot = "../.."

func sourceInputs(test *testing.T) Inputs {
	test.Helper()
	inputs, sourceInputsError := SourceInputs(moduleRoot)
	if sourceInputsError != nil {
		test.Fatal(sourceInputsError)
	}
	return inputs
}

func built(test *testing.T, inputs Inputs) map[string][]byte {
	test.Helper()
	files, buildError := Build(inputs)
	if buildError != nil {
		test.Fatal(buildError)
	}
	return files
}

func staleAgainstCommitted(test *testing.T, files map[string][]byte) []string {
	test.Helper()
	stale, staleError := Stale(moduleRoot, files)
	if staleError != nil {
		test.Fatal(staleError)
	}
	return stale
}

// TestTheRenderedFilesAreCurrent: docs/data is what Build renders now from the committed examples, every
// file but cli.json, which needs the binary built. It is -check-rendering's depth: the examples themselves
// are recaptured only by the generator's -check, which takes minutes and runs in the release's module check.
func TestTheRenderedFilesAreCurrent(test *testing.T) {
	test.Parallel()
	if stale := staleAgainstCommitted(test, built(test, sourceInputs(test))); len(stale) > 0 {
		test.Errorf("stale: %v; run go run ./internal/docsdata/tools/generate", stale)
	}
}

// TestCheckSeesARegistryChange is -check's contract for the registry, in both directions: the committed
// files pass as they are, and fail once a rule is removed or added.
func TestCheckSeesARegistryChange(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	if stale := staleAgainstCommitted(test, built(test, inputs)); len(stale) > 0 {
		test.Fatalf("the committed files are already stale (%v), so a change could not be told apart", stale)
	}

	removed := inputs
	removed.Registrations = inputs.Registrations[1:]
	if stale := staleAgainstCommitted(test, built(test, removed)); !slices.Contains(stale, RulesPath) {
		test.Errorf("removing %s from the registry left rules.json current: %v", inputs.Registrations[0].Rule.Name, stale)
	}

	added := inputs
	added.Registrations = append(slices.Clone(inputs.Registrations), rule.Registration{
		Rule: rule.Rule{Name: "nexus/consistency-no-docsdata-test-rule", Run: func(rule.Context, any) rule.Listeners { return nil }},
	})
	if stale := staleAgainstCommitted(test, built(test, added)); !slices.Contains(stale, RulesPath) {
		test.Errorf("adding a rule to the registry left rules.json current: %v", stale)
	}
}

// TestCheckSeesATierChange: a rule set resolving differently makes sets.json and rules.json stale.
func TestCheckSeesATierChange(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
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
	stale := staleAgainstCommitted(test, built(test, inputs))
	for _, path := range []string{SetsPath, RulesPath} {
		if !slices.Contains(stale, path) {
			test.Errorf("changing %s's severity in %s left %s current: %v", changedRule, changedSet, path, stale)
		}
	}
}

// TestEveryRegisteredRuleHasOneRow: rules.json names every rule each engine registers exactly once, under
// that engine's language, and nothing else. The Swift registry is read from swift/Rules.json, which a
// Swift test holds to the Swift engine's registry.
func TestEveryRegisteredRuleHasOneRow(test *testing.T) {
	test.Parallel()
	committed, err := os.ReadFile(filepath.Join(moduleRoot, RulesPath))
	if err != nil {
		test.Fatal(err)
	}
	var rules Rules
	if decodeError := json.Unmarshal(committed, &rules); decodeError != nil {
		test.Fatal(decodeError)
	}
	swiftRegistry, err := os.ReadFile(filepath.Join(moduleRoot, "swift", "Rules.json"))
	if err != nil {
		test.Fatal(err)
	}
	var swiftEntries []struct {
		Name string `json:"name"`
	}
	if decodeError := json.Unmarshal(swiftRegistry, &swiftEntries); decodeError != nil {
		test.Fatal(decodeError)
	}
	registered := map[string][]string{"TypeScript": registry.Names(), "Go": {}}
	for _, entry := range swiftEntries {
		registered["Swift"] = append(registered["Swift"], entry.Name)
	}
	for _, goRule := range goenginerules.All() {
		registered["Go"] = append(registered["Go"], goRule.Name)
	}

	rows := map[string]map[string]int{}
	for _, row := range rules.Rules {
		if rows[row.Language] == nil {
			rows[row.Language] = map[string]int{}
		}
		rows[row.Language][row.Name]++
	}
	// Go is held to its registry in both directions like the others, but is not refused when empty: its
	// rules arrive one unit at a time, and until the first does, zero Go rows is the true answer. The
	// vacuity guard stays on the two engines that have always registered rules.
	for _, language := range []string{"TypeScript", "Swift", "Go"} {
		if len(registered[language]) == 0 && language != "Go" {
			test.Fatalf("no %s rule is registered, so the rows were checked against nothing", language)
		}
		names := map[string]bool{}
		for _, name := range registered[language] {
			names[name] = true
			if rows[language][name] != 1 {
				test.Errorf("%s is a registered %s rule and has %d %s rows in rules.json", name, language, rows[language][name], language)
			}
		}
		for name := range rows[language] {
			if !names[name] {
				test.Errorf("rules.json has a %s row for %s, which that engine does not register", language, name)
			}
		}
	}
	for language := range rows {
		if registered[language] == nil {
			test.Errorf("rules.json has rows in %s, which no engine registers", language)
		}
	}
	if rules.Count != len(rules.Rules) {
		test.Errorf("rules.json says count %d over %d rows", rules.Count, len(rules.Rules))
	}
}

// swiftRegistryEntry is one rule in swift/Rules.json, every field Build reads and every field the file holds,
// since Build refuses one it does not know: so an entry decoded here and encoded again is the entry the file
// wrote.
type swiftRegistryEntry struct {
	Name         string `json:"name"`
	Origin       string `json:"origin"`
	UpstreamName string `json:"upstreamName,omitempty"`
	TypeAware    bool   `json:"typeAware"`
}

// TestCheckSeesASwiftRegistryChange: a Swift rule removed from swift/Rules.json makes rules.json stale, and
// a Swift rule with an unknown origin, a misnamed house rule, or a verdict naming a missing Swift rule
// refuses the build.
func TestCheckSeesASwiftRegistryChange(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	if stale := staleAgainstCommitted(test, built(test, inputs)); len(stale) > 0 {
		test.Fatalf("the committed files are already stale (%v), so a change could not be told apart", stale)
	}
	// Read as Build reads it, refusing a field the entry does not name, so a field added to the file fails
	// here rather than vanishing from every changed copy below.
	var entries []swiftRegistryEntry
	if decodeError := rule.UnmarshalOptions(inputs.SwiftRules, &entries); decodeError != nil {
		test.Fatal(decodeError)
	}
	withEntries := func(change func([]swiftRegistryEntry) []swiftRegistryEntry) Inputs {
		encoded, encodeError := json.Marshal(change(slices.Clone(entries)))
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		changed := inputs
		changed.SwiftRules = encoded
		return changed
	}

	removed := withEntries(func(entries []swiftRegistryEntry) []swiftRegistryEntry {
		// The last row, so a rule no verdict needs is the likelier one removed; a verdict-carrying rule
		// would refuse the build instead, which is the case below.
		for index := len(entries) - 1; index >= 0; index-- {
			if entries[index].Origin != houseOrigin {
				return append(entries[:index], entries[index+1:]...)
			}
		}
		test.Fatal("no ported Swift rule to remove")
		return nil
	})
	if stale := staleAgainstCommitted(test, built(test, removed)); !slices.Contains(stale, RulesPath) {
		test.Errorf("removing a Swift rule left rules.json current: %v", stale)
	}

	for name, change := range map[string]func([]swiftRegistryEntry) []swiftRegistryEntry{
		"an unknown origin": func(entries []swiftRegistryEntry) []swiftRegistryEntry {
			entries[0].Origin = "swiftlint-ish"
			return entries
		},
		"a house rule outside the scheme": func(entries []swiftRegistryEntry) []swiftRegistryEntry {
			return append(entries, swiftRegistryEntry{
				Name:      swiftNamespace + "/docsdata-test-rule",
				Origin:    houseOrigin,
				TypeAware: false,
			})
		},
		"a verdict naming a missing Swift rule": func(entries []swiftRegistryEntry) []swiftRegistryEntry {
			for index, entry := range entries {
				if entry.Name == swiftNamespace+"/consistency-no-bare-throw" {
					return append(entries[:index], entries[index+1:]...)
				}
			}
			test.Fatal("the bare-throw rule, which a verdict names, is not in swift/Rules.json")
			return nil
		},
	} {
		if _, buildError := Build(withEntries(change)); buildError == nil {
			test.Errorf("%s built", name)
		}
	}
}

// TestEveryHouseRuleFollowsTheNamingScheme: every house rule builds with a category and no port carries
// one, and a house rule registered under a name outside the scheme refuses the build.
func TestEveryHouseRuleFollowsTheNamingScheme(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	var rules Rules
	if decodeError := json.Unmarshal(built(test, inputs)[RulesPath], &rules); decodeError != nil {
		test.Fatal(decodeError)
	}
	house := 0
	for _, row := range rules.Rules {
		switch {
		case row.Origin == houseOrigin && row.Category == "":
			test.Errorf("%s is a house rule with no category", row.Name)
		case row.Origin != houseOrigin && row.Category != "":
			test.Errorf("%s is a port and carries the category %q", row.Name, row.Category)
		}
		if row.Origin == houseOrigin {
			house++
		}
	}
	if house == 0 {
		test.Fatal("no house rule was built, so the scheme was checked against nothing")
	}

	misnamed := inputs
	misnamed.Registrations = append(slices.Clone(inputs.Registrations), rule.Registration{
		Rule: rule.Rule{Name: "nexus/docsdata-test-rule", Run: func(rule.Context, any) rule.Listeners { return nil }},
	})
	if _, buildError := Build(misnamed); buildError == nil || !strings.Contains(buildError.Error(), "nexus/docsdata-test-rule") {
		test.Errorf("a house rule named outside the scheme built: %v", buildError)
	}
}

// TestBuildIsDeterministic: two builds give the same bytes, so a regeneration diffs to nothing.
func TestBuildIsDeterministic(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	first, second := built(test, inputs), built(test, inputs)
	if len(first) != len(second) {
		test.Fatalf("%d files, then %d", len(first), len(second))
	}
	for path, contents := range first {
		if !bytes.Equal(contents, second[path]) {
			test.Errorf("%s differs between two builds", path)
		}
	}
}

// TestEveryFieldHasASourceNote: rules.json says where each field of a row comes from, and names no field
// a row does not have.
func TestEveryFieldHasASourceNote(test *testing.T) {
	test.Parallel()
	encoded, encodeError := json.Marshal(RuleRow{Options: &RuleOptions{}, Swift: &SwiftVerdict{}, Sets: []RuleSetSeverity{{}}, MessageIds: []string{""},
		Namespace: "-", UpstreamName: "-", Category: "-", FixKind: "-", TypeScriptRules: []string{""}})
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	var fields map[string]json.RawMessage
	if decodeError := json.Unmarshal(encoded, &fields); decodeError != nil {
		test.Fatal(decodeError)
	}
	for field := range fields {
		if ruleSourceNotes[field] == "" {
			test.Errorf("rules.json rows carry %q and sourceNotes does not say where it comes from", field)
		}
	}
	for field := range ruleSourceNotes {
		if _, present := fields[field]; !present {
			test.Errorf("sourceNotes describes %q, which a row does not carry", field)
		}
	}
}

// TestTheHelpParserReadsWhatTheFlagPackageWrites: a flag set with every shape the binary uses, printed by
// the flag package itself, parses back to exactly what the set holds.
func TestTheHelpParserReadsWhatTheFlagPackageWrites(test *testing.T) {
	test.Parallel()
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
	_, parsed, parseFlagHelpError := parseFlagHelp(printed.Bytes())
	if parseFlagHelpError != nil {
		test.Fatalf("%v\n%s", parseFlagHelpError, printed.String())
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
		test.Fatalf("parsed %d flags, the set holds %d:\n%s", len(parsed), len(want), printed.String())
	}
	for index := range want {
		if parsed[index] != want[index] {
			test.Errorf("flag %d: parsed %+v, want %+v", index, parsed[index], want[index])
		}
	}
}

// TestTheVerbHelpKeepsItsSynopsis: a verb's usage lines and summary are told apart from its flags.
func TestTheVerbHelpKeepsItsSynopsis(test *testing.T) {
	test.Parallel()
	help := "usage: cohere rename <file>:<line>:<column> <newName>\n" +
		"       cohere rename <name> <newName>\n\n" +
		"Renames a symbol.\nNothing is written without --write.\n\n" +
		"  -write\n    \tapply the rename\n"
	cli, parseCommandLineInterfaceError := parseCli([]byte("  -fix\n    \tapply fixes only\n"), map[string][]byte{"rename": []byte(help)})
	if parseCommandLineInterfaceError != nil {
		test.Fatal(parseCommandLineInterfaceError)
	}
	verb := cli.Verbs[0]
	if strings.Join(verb.Usage, "|") != "cohere rename <file>:<line>:<column> <newName>|cohere rename <name> <newName>" {
		test.Errorf("usage %q", verb.Usage)
	}
	if verb.Summary != "Renames a symbol. Nothing is written without --write." {
		test.Errorf("summary %q", verb.Summary)
	}
	if len(verb.Flags) != 1 || verb.Flags[0].Name != "write" {
		test.Errorf("flags %+v", verb.Flags)
	}
}

// TestTheChangelogParsesEveryRelease: every release heading in CHANGELOG.md becomes one entry, and a
// second-level heading that is not a release refuses the parse.
func TestTheChangelogParsesEveryRelease(test *testing.T) {
	test.Parallel()
	changelog, err := os.ReadFile(filepath.Join(moduleRoot, "CHANGELOG.md"))
	if err != nil {
		test.Fatal(err)
	}
	entries, err := parseChangelog(changelog)
	if err != nil {
		test.Fatal(err)
	}
	if want := strings.Count("\n"+string(changelog), "\n## "); len(entries) != want {
		test.Errorf("%d entries for %d release headings", len(entries), want)
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Body) == "" {
			test.Errorf("%s has an empty body", entry.Version)
		}
	}
	if headingAnchor("1.0.0 (unreleased)") != "100-unreleased" {
		test.Errorf("anchor %q", headingAnchor("1.0.0 (unreleased)"))
	}
	if _, parseChangelogError := parseChangelog([]byte("# Changelog\n\n## Notes\n")); parseChangelogError == nil {
		test.Error("a second-level heading that is not a release parsed")
	}
}

// TestPickExamplesPrefersWhatAReaderCanRun: the shortest case run without options and without other
// files wins, a rule with an asserted fix shows its fixed source, and the message ids and fix kind cover
// every asserted case, those run with options included.
func TestPickExamplesPrefersWhatAReaderCanRun(test *testing.T) {
	test.Parallel()
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
		test.Errorf("a rule with an asserted fix should show it: %+v", examples.Firing)
	}
	if examples.Clean == nil || examples.Clean.Source != "clean" {
		test.Errorf("clean %+v", examples.Clean)
	}
	if strings.Join(examples.MessageIds(), ",") != "fixed,other,plain,withOptions" || examples.FixKind() != "fix" {
		test.Errorf("ids %v, fix kind %q", examples.MessageIds(), examples.FixKind())
	}

	unfixed, _ := PickExamples([]capture.Record{
		{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{}`), Outcome: capture.OutcomeFindings},
		{Rule: "r", File: "a.ts", Source: "xx", OtherFiles: 1, Outcome: capture.OutcomeFindings},
		{Rule: "r", File: "a.ts", Source: "xyz", Outcome: capture.OutcomeFindings},
	})
	if unfixed["r"].Firing.Source != "xyz" {
		test.Errorf("the case a reader can run as shown should win: %+v", unfixed["r"].Firing)
	}
}

// TestACaseRunWithOptionsIsNeverShown: a rule whose only asserted cases ran with options has no example,
// and its message ids still count.
func TestACaseRunWithOptionsIsNeverShown(test *testing.T) {
	test.Parallel()
	picked, withOptions := PickExamples([]capture.Record{
		{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{"Mode": "TypeAnnotation"}`), Outcome: capture.OutcomeFindings,
			Findings: []capture.Finding{{MessageId: "only"}}},
		{Rule: "r", File: "a.ts", Source: "y", Options: json.RawMessage(`{"Mode": "TypeAnnotation"}`), Outcome: capture.OutcomeClean},
	})
	examples := picked["r"]
	if examples.Firing != nil || examples.Clean != nil {
		test.Errorf("a case run with options was shown: %+v", examples)
	}
	if strings.Join(examples.MessageIds(), ",") != "only" || withOptions != 2 {
		test.Errorf("ids %v, %d with options", examples.MessageIds(), withOptions)
	}
}

// TestPickExamplesCountsACaseOnce: one case asserted twice, as findings and then as its fixed source, or
// as findings by two tests, is counted once per outcome, and the fixed record is the one shown.
func TestPickExamplesCountsACaseOnce(test *testing.T) {
	test.Parallel()
	findings := capture.Record{Rule: "r", File: "a.ts", Source: "x", Options: json.RawMessage(`{"a":1}`), Outcome: capture.OutcomeFindings}
	_, withOptions := PickExamples([]capture.Record{findings, findings})
	if withOptions != 1 {
		test.Errorf("one case asserted twice counted %d times", withOptions)
	}

	asserted := capture.Record{Rule: "r", File: "a.ts", Source: "debugger;", Outcome: capture.OutcomeFindings,
		Findings: []capture.Finding{{MessageId: "debugger", Fix: true}}}
	fixed := asserted
	fixed.Outcome, fixed.FixedSource = capture.OutcomeFixed, "\n"
	picked, _ := PickExamples([]capture.Record{asserted, fixed, asserted})
	if picked["r"].Firing == nil || picked["r"].Firing.FixedSource != "\n" {
		test.Errorf("the fixed record should be the one shown: %+v", picked["r"].Firing)
	}
}

// TestBenchmarksHoldEveryRecordNewestFirst: benchmarks.json is every committed record, as recorded, newest
// first, with a later record placed ahead of the committed ones.
func TestBenchmarksHoldEveryRecordNewestFirst(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	committed, readError := benchresults.Read(filepath.Join(moduleRoot, benchresults.Directory))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(committed) == 0 {
		test.Fatal("bench/results holds no record, so this test would compare nothing")
	}
	later := committed[0]
	later.RecordedAt = "2999-01-01T00:00:00Z"
	inputs.Benchmarks = append(slices.Clone(committed), later)

	var benchmarks Benchmarks
	if decodeError := json.Unmarshal(built(test, inputs)[BenchmarksPath], &benchmarks); decodeError != nil {
		test.Fatal(decodeError)
	}
	if len(benchmarks.Records) != len(committed)+1 {
		test.Fatalf("%d records published for %d given", len(benchmarks.Records), len(committed)+1)
	}
	if benchmarks.Records[0].RecordedAt != later.RecordedAt {
		test.Errorf("the newest record is not first: %s", benchmarks.Records[0].RecordedAt)
	}
	for index := 1; index < len(benchmarks.Records); index++ {
		if benchmarks.Records[index-1].RecordedAt < benchmarks.Records[index].RecordedAt {
			test.Errorf("records %d and %d are oldest first", index-1, index)
		}
	}
	if benchmarks.SourceNote == "" {
		test.Error("benchmarks.json says nothing about where its numbers come from")
	}
}

// TestAnUnearnedQuietNumberRefusesTheBuild: a record whose mode claims a quiet summary its runs did not earn
// is refused by Build, not published.
func TestAnUnearnedQuietNumberRefusesTheBuild(test *testing.T) {
	test.Parallel()
	inputs := sourceInputs(test)
	committed, readError := benchresults.Read(filepath.Join(moduleRoot, benchresults.Directory))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(committed) == 0 {
		test.Fatal("bench/results holds no record, so this test would plant nothing")
	}
	tampered := committed[0]
	tampered.Modes = map[benchresults.Mode]benchresults.ModeSummary{}
	for mode, summary := range committed[0].Modes {
		tampered.Modes[mode] = summary
	}
	cold := tampered.Modes[benchresults.ModeCold]
	cold.Quiet = &benchresults.QuietSummary{Count: 1, Best: 0.1, Median: 0.1, Worst: 0.1}
	tampered.Modes[benchresults.ModeCold] = cold
	inputs.Benchmarks = []benchresults.Record{tampered}
	if _, buildError := Build(inputs); buildError == nil {
		test.Error("a quiet number the runs did not earn was published")
	}
}
