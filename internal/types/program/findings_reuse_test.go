package program_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/system-inc/cohere/internal/lint/registry"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// findingsReuseRules is a small rule set with both kinds in it: pure rules a cache may replay, and a
// type-aware one it must always run.
func findingsReuseRules(t *testing.T) []rule.Rule {
	t.Helper()
	wanted := map[string]bool{"no-self-compare": true, "no-duplicate-case": true, "no-empty": true, "no-debugger": true}
	var rules []rule.Rule
	var typeAware *rule.Rule
	for _, registered := range registry.All() {
		if wanted[registered.Name] {
			rules = append(rules, registered)
		}
		if typeAware == nil && registered.NeedsTypeChecker && !registered.ReadsProgram {
			subject := registered
			typeAware = &subject
		}
	}
	if len(rules) != len(wanted) || typeAware == nil {
		t.Fatalf("the fixture's rules are not all registered (%d of %d, type-aware %v), so this proves nothing",
			len(rules), len(wanted), typeAware != nil)
	}
	return append(rules, *typeAware)
}

// diagnosticKey is what a finding says, without the pointers that tie it to one graph.
func diagnosticKeys(diagnostics []rule.Diagnostic) []string {
	keys := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		keys[index] = fmt.Sprintf("%s|%s|%d-%d|%s|%s", filepath.Base(diagnostic.SourceFile.FileName()), diagnostic.RuleName,
			diagnostic.Range.Pos(), diagnostic.Range.End(), diagnostic.Message.Id, diagnostic.Message.Description)
	}
	return keys
}

// A walk that replays files from the findings cache reports exactly what a walk without one reports:
// the same findings, and the same coverage in every field. And it does replay, or the equality above
// would hold vacuously.
//
// The fixture is built so each path is exercised. a.ts has findings with no fix, so it is cached with
// those findings and replayed; b.ts is clean; c.ts carries a suppression directive and must never be
// cached; d.ts has a finding with a fix and must never be cached. Each premise is checked rather than
// assumed, since a fixture whose rules happened not to fire would pass everything here for nothing.
func TestAReplayingWalkReportsWhatAPlainWalkReports(t *testing.T) {
	root := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"a.ts":          "export function f(x: number): number {\n  if (x === x) {\n    return 1;\n  }\n  switch (x) {\n    case 1:\n      break;\n    case 1:\n      break;\n  }\n  return 0;\n}\n",
		"b.ts":          "export const b = 1;\n",
		"c.ts":          "export function g(y: number): boolean {\n  // cohere-disable-next-line no-self-compare\n  return y === y;\n}\n",
		"d.ts":          "export function h(): void {\n  debugger;\n}\n",
	})
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	files := graph.ProjectFiles()
	rules := findingsReuseRules(t)
	ctx := context.Background()
	key := program.HashRuleSet([]string{"fixture"})

	plain, err := graph.Walk(ctx, files, rules)
	if err != nil {
		t.Fatalf("plain walk: %v", err)
	}

	recording := program.NewFindingsReuse(key, nil)
	graph.FindingsReuse = recording
	if _, err := graph.Walk(ctx, files, rules); err != nil {
		t.Fatalf("recording walk: %v", err)
	}
	recorded := recording.Recorded()

	graph.FindingsReuse = program.NewFindingsReuse(key, recorded)
	replayed, err := graph.Walk(ctx, files, rules)
	if err != nil {
		t.Fatalf("replaying walk: %v", err)
	}

	byFile := map[string]program.LintCacheEntry{}
	for _, entry := range recorded.Entries {
		byFile[filepath.Base(entry.Path)] = entry
	}
	if len(byFile["a.ts"].Findings) == 0 {
		t.Fatal("a.ts was not recorded with findings, so findings replay is untested here")
	}
	if _, cached := byFile["c.ts"]; cached {
		t.Error("c.ts carries a suppression directive and was cached; a directive's accounting spans every rule in the file")
	}
	fixable := false
	for _, diagnostic := range plain.Diagnostics {
		if filepath.Base(diagnostic.SourceFile.FileName()) == "d.ts" && len(diagnostic.Fixes) > 0 {
			fixable = true
		}
	}
	if !fixable {
		t.Fatal("d.ts produced no fixable finding, so the fixable-file exclusion is untested here")
	}
	if _, cached := byFile["d.ts"]; cached {
		t.Error("d.ts has a fixable finding and was cached; a cached finding carries no fix for the fix phase")
	}

	if replayed.FilesReplayed == 0 {
		t.Fatal("nothing was replayed, so the equality below would hold for nothing")
	}
	if !reflect.DeepEqual(diagnosticKeys(plain.Diagnostics), diagnosticKeys(replayed.Diagnostics)) {
		t.Errorf("findings differ:\n plain    %v\n replayed %v", diagnosticKeys(plain.Diagnostics), diagnosticKeys(replayed.Diagnostics))
	}
	if !reflect.DeepEqual(plain.Coverage, replayed.Coverage) {
		t.Errorf("coverage differs:\n plain    %+v\n replayed %+v", plain.Coverage, replayed.Coverage)
	}
}

// An edited file is walked, not replayed, and reports what its new bytes say.
func TestAnEditedFileIsWalkedNotReplayed(t *testing.T) {
	root := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"a.ts":          "export function f(x: number): boolean {\n  return x === x;\n}\n",
		"b.ts":          "export const b = 1;\n",
	})
	build := func() *program.Graph {
		t.Helper()
		graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return graph
	}
	rules := findingsReuseRules(t)
	ctx := context.Background()
	key := program.HashRuleSet([]string{"fixture"})

	first := build()
	recording := program.NewFindingsReuse(key, nil)
	first.FindingsReuse = recording
	before, err := first.Walk(ctx, first.ProjectFiles(), rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Diagnostics) == 0 {
		t.Fatal("a.ts produced no finding before the edit, so its disappearance would prove nothing")
	}

	// The edit removes the finding. A cache keyed on path would replay it; keyed on bytes it cannot.
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function f(x: number): boolean {\n  return x === 1;\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second := build()
	second.FindingsReuse = program.NewFindingsReuse(key, recording.Recorded())
	after, err := second.Walk(ctx, second.ProjectFiles(), rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Diagnostics) != 0 {
		t.Errorf("the edited file's old finding was replayed: %v", diagnosticKeys(after.Diagnostics))
	}
	if after.FilesReplayed != 1 {
		t.Errorf("replayed %d files, want exactly the unchanged b.ts", after.FilesReplayed)
	}
}

// A cache recorded under another key serves nothing: a new binary, config or rule set invalidates
// every entry at once.
func TestAFindingsCacheUnderAnotherKeyServesNothing(t *testing.T) {
	root := writeProject(t, map[string]string{"tsconfig.json": minimalConfig, "b.ts": "export const b = 1;\n"})
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if err != nil {
		t.Fatal(err)
	}
	rules := findingsReuseRules(t)
	recording := program.NewFindingsReuse(program.HashRuleSet([]string{"old"}), nil)
	graph.FindingsReuse = recording
	if _, err := graph.Walk(context.Background(), graph.ProjectFiles(), rules); err != nil {
		t.Fatal(err)
	}
	if len(recording.Recorded().Entries) == 0 {
		t.Fatal("nothing was recorded, so a miss below would prove nothing")
	}
	graph.FindingsReuse = program.NewFindingsReuse(program.HashRuleSet([]string{"new"}), recording.Recorded())
	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), rules)
	if err != nil {
		t.Fatal(err)
	}
	if result.FilesReplayed != 0 {
		t.Errorf("replayed %d files from a cache recorded under another key", result.FilesReplayed)
	}
}

// With only pure rules, a replayed file has nothing left to walk at all, and its coverage still matches.
//
// This is the path the mixed rule set above never reaches: no uncacheable rule applies, so the walk is
// skipped and the file's node count comes from its entry. Counted as zero, the coverage line would
// change on every such file.
func TestAFileWithNothingLeftToWalkKeepsItsCoverage(t *testing.T) {
	root := writeProject(t, map[string]string{
		"tsconfig.json": minimalConfig,
		"a.ts":          "export function f(x: number): boolean {\n  return x === x;\n}\n",
		"b.ts":          "export const b = 1;\n",
	})
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
	if err != nil {
		t.Fatal(err)
	}
	var pure []rule.Rule
	for _, subject := range findingsReuseRules(t) {
		if !subject.NeedsTypeChecker && !subject.ReadsProgram {
			pure = append(pure, subject)
		}
	}
	ctx := context.Background()
	key := program.HashRuleSet([]string{"fixture"})
	files := graph.ProjectFiles()

	plain, err := graph.Walk(ctx, files, pure)
	if err != nil {
		t.Fatal(err)
	}
	recording := program.NewFindingsReuse(key, nil)
	graph.FindingsReuse = recording
	if _, err := graph.Walk(ctx, files, pure); err != nil {
		t.Fatal(err)
	}
	graph.FindingsReuse = program.NewFindingsReuse(key, recording.Recorded())
	replayed, err := graph.Walk(ctx, files, pure)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.FilesReplayed != len(files) {
		t.Fatalf("replayed %d of %d files, so the nothing-left-to-walk path is not what ran", replayed.FilesReplayed, len(files))
	}
	if plain.Coverage.NodesVisited == 0 {
		t.Fatal("the plain walk visited no nodes, so a zero below would match for nothing")
	}
	if !reflect.DeepEqual(plain.Coverage, replayed.Coverage) {
		t.Errorf("coverage differs: plain visited %d nodes, replayed %d", plain.Coverage.NodesVisited, replayed.Coverage.NodesVisited)
	}
	if !reflect.DeepEqual(diagnosticKeys(plain.Diagnostics), diagnosticKeys(replayed.Diagnostics)) {
		t.Errorf("findings differ:\n plain    %v\n replayed %v", diagnosticKeys(plain.Diagnostics), diagnosticKeys(replayed.Diagnostics))
	}
}
