package program_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"github.com/system-inc/cohere/internal/types/program"
)

// Readiness measures every file against the set whatever the chain enables (#drbrp8c). Three set rules
// and one other, over two files:
//
//   - test-set-enabled is on in the chain: its findings report as ever, and count, suppressed ones too,
//     since Adamic reads no disable comment;
//   - test-set-off is off in the chain: it runs measure-only, so it counts and reports nothing, and its
//     fix never reaches anything that could apply it;
//   - test-set-skips skips b.ts for a missing precondition, which leaves b.ts unmeasured, not ready;
//   - test-other is no set rule: it reports and is never counted.
func TestReadinessCountsEverySetRuleAndReportsOnlyTheChainsOwn(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json":       minimalConfiguration,
		"CohereSettings.json": `{"rules": {"test-set-enabled": "error", "test-set-off": "off", "test-set-skips": "error", "test-other": "error"}}`,
		"a.ts":                "// cohere-disable-next-line test-set-enabled -- suppressed, still counted\nexport const first = 1;\nexport const second = 2;\n",
		"b.ts":                "export const third = 3;\n",
	})
	graph, buildError := program.Build(
		program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory, SingleThreaded: true},
	)
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	var loadError error
	graph.LintConfiguration, loadError = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if loadError != nil {
		test.Fatalf("loading the config: %v", loadError)
	}
	graph.Readiness = &program.Readiness{Options: map[string][]json.RawMessage{
		"test-set-enabled": nil, "test-set-off": nil, "test-set-skips": nil,
	}}

	eachConstant := func(id string, fix bool) func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners {
		return func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
				message := rule.Message{Id: id, Description: id}
				if fix {
					ruleContext.ReportNodeWithFixes(node, message, ruleContext.RemoveNode(node))
					return
				}
				ruleContext.ReportNode(node, message)
			}}
		}
	}
	rules := []rule.Rule[rule.NoOptions]{
		{Name: "test-other", Run: eachConstant("other", false)},
		{Name: "test-set-enabled", Run: eachConstant("enabled", false)},
		{Name: "test-set-off", Run: eachConstant("off", true)},
		{Name: "test-set-skips", Run: func(ruleContext rule.Context, _ rule.Given[rule.NoOptions]) rule.Listeners {
			if filepath.Base(ruleContext.SourceFile.FileName().AsString()) == "b.ts" {
				ruleContext.Skip("b.ts lacks what this rule needs")
				return nil
			}
			return nil
		}, NoListener: rule.NoListenerAnswersInRun},
	}
	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, rules...))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}

	reported := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		reported[diagnostic.RuleName]++
		if diagnostic.RuleName == "test-set-off" {
			test.Errorf(
				"a measure-only rule reported %q, which a reader would see and a fix phase could apply",
				diagnostic.Message.Id,
			)
		}
	}
	// a.ts's first constant is suppressed for test-set-enabled, so the chain sees one of a.ts's two and b.ts's one.
	if want := map[string]int{"test-other": 3, "test-set-enabled": 2}; !reflect.DeepEqual(reported, want) {
		test.Errorf("reported %v, want %v", reported, want)
	}

	records := map[string]*program.AdamicRecord{}
	for fileName, record := range result.Adamic {
		records[filepath.Base(fileName)] = record
	}
	want := map[string]*program.AdamicRecord{
		"a.ts": {Counts: []program.AdamicCount{
			{Rule: "test-set-enabled", Findings: 2},
			{Rule: "test-set-off", Findings: 2},
			{Rule: "test-set-skips", Findings: 0},
		}},
		"b.ts": {Counts: []program.AdamicCount{
			{Rule: "test-set-enabled", Findings: 1},
			{Rule: "test-set-off", Findings: 1},
			{Rule: "test-set-skips", Findings: 0},
		},
			Skipped: []string{"test-set-skips"}},
	}
	if !reflect.DeepEqual(records, want) {
		// Ignored: the records are plain structs of strings, numbers and slices, which always marshal.
		got, _ := json.Marshal(records)
		// Ignored: the records are plain structs of strings, numbers and slices, which always marshal.
		expected, _ := json.Marshal(want)
		test.Errorf("readiness records\n got %s\nwant %s", got, expected)
	}
	if records["b.ts"].Measured() || !records["a.ts"].Measured() {
		test.Error("a file a set rule skipped must read unmeasured, and one every set rule ran on measured")
	}
	if records["a.ts"].Findings() != 4 {
		test.Errorf("a.ts totals %d findings, want 4", records["a.ts"].Findings())
	}
	// The measure-only rule's notes stay out of the run's, and its skip-free note set is empty.
	for fileName, notes := range result.Notes {
		if _, present := notes["test-set-off"]; present {
			test.Errorf("%s carries a measure-only rule's notes into the run's", fileName)
		}
	}
}

// TestReadinessOffMeasuresNothing: without Readiness the walk records no measurement and runs no rule
// the chain leaves off, which is every run before readiness existed.
func TestReadinessOffMeasuresNothing(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json":       minimalConfiguration,
		"CohereSettings.json": `{"rules": {"test-set-off": "off", "test-other": "error"}}`,
		"a.ts":                "export const first = 1;\n",
	})
	graph, buildError := program.Build(
		program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory, SingleThreaded: true},
	)
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	var loadError error
	graph.LintConfiguration, loadError = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if loadError != nil {
		test.Fatalf("loading the config: %v", loadError)
	}
	ran := false
	rules := []rule.Rule[rule.NoOptions]{
		{
			Name:       "test-other",
			Run:        func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners { return nil },
			NoListener: rule.NoListenerAnswersInRun,
		},
		{
			Name: "test-set-off",
			Run: func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners {
				ran = true
				return nil
			},
			NoListener: rule.NoListenerAnswersInRun,
		},
	}
	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, rules...))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}
	if result.Adamic != nil || ran {
		test.Errorf("with readiness off the walk recorded %v and ran the off rule: %t", result.Adamic, ran)
	}
}

// TestReadinessRunsATierEnabledSetRuleOncePerFile: a set rule the chain already runs is counted as it
// reports, never run a second time to measure, so readiness adds nothing to a rule the tier has on (#drbrp8c).
func TestReadinessRunsATierEnabledSetRuleOncePerFile(test *testing.T) {
	test.Parallel()
	directory := writeProject(test, map[string]string{
		"tsconfig.json":       minimalConfiguration,
		"CohereSettings.json": `{"rules": {"test-set-enabled": "error"}}`,
		"a.ts":                "export const first = 1;\n",
		"b.ts":                "export const second = 2;\n",
	})
	graph, buildError := program.Build(
		program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory, SingleThreaded: true},
	)
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	var loadError error
	graph.LintConfiguration, loadError = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if loadError != nil {
		test.Fatalf("loading the config: %v", loadError)
	}
	graph.Readiness = &program.Readiness{Options: map[string][]json.RawMessage{"test-set-enabled": nil}}

	var runs atomic.Int32
	rules := []rule.Rule[rule.NoOptions]{{Name: "test-set-enabled", Run: func(
		ruleContext rule.Context,
		_ rule.Given[rule.NoOptions],
	) rule.Listeners {
		runs.Add(1)
		return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
			ruleContext.ReportNode(node, rule.Message{Id: "enabled", Description: "enabled"})
		}}
	}}}
	result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rule_testing.EntriesOf(test, rules...))
	if walkError != nil {
		test.Fatalf("walking: %v", walkError)
	}
	if got := runs.Load(); got != 2 {
		test.Errorf("the tier-enabled set rule ran %d times over 2 files, want once per file", got)
	}
	if len(result.Diagnostics) != 2 {
		test.Errorf("reported %d findings, want each file's one", len(result.Diagnostics))
	}
	for fileName, record := range result.Adamic {
		if want := []program.AdamicCount{
			{Rule: "test-set-enabled", Findings: 1},
		}; !reflect.DeepEqual(record.Counts, want) {
			test.Errorf(
				"%s counts %v, want %v: the one run's finding, counted",
				filepath.Base(fileName),
				record.Counts,
				want,
			)
		}
	}
}

// TestAWarmReplayRunsNoRuleForReadiness: readiness is a file's property and rides the findings cache, so a
// walk that replays every file runs no rule to measure, the measure-only and type-aware ones included, and
// reads the records the walk that recorded them took (#drbrp8c).
//
// c.ts carries a directive that silences one of its findings. Replayed whole, it is dispatched with no rule to
// walk so its directive is tallied, and its record must still be the recorded one: an empty walked part read as
// unmeasured would leave every directive file out of readiness (#kdee854).
func TestAWarmReplayRunsNoRuleForReadiness(test *testing.T) {
	test.Parallel()
	typeAware := typeAwareRule(test)
	root := writeProject(test, map[string]string{
		"tsconfig.json": minimalConfiguration,
		"CohereSettings.json": `{"rules": {"test-set-enabled": "error", "test-set-off": "off", "` +
			typeAware.Name +
			`": "off"}}`,
		"a.ts": "export const first = 1;\nexport const negated = -first;\n",
		"b.ts": "export const second = 2;\n",
		"c.ts": "// cohere-disable-next-line test-set-enabled -- the directive file\nexport const third = 3;\n",
	})

	var runs atomic.Int32
	// Counts the runs of a rule's Unconfigured, which is what this walk runs: the config gives no rule options.
	counted := func(subject rule.Entry) rule.Entry {
		run := subject.Unconfigured.Run
		subject.Unconfigured.Run = func(ruleContext rule.Context) rule.Listeners {
			runs.Add(1)
			return run(ruleContext)
		}
		return subject
	}
	eachConstant := func(id string) func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		return func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
				ruleContext.ReportNode(node, rule.Message{Id: id, Description: id})
			}}
		}
	}
	rules := []rule.Entry{
		counted(rule_testing.EntryOf(test, rule.Rule[rule.NoOptions]{
			Name: "test-set-enabled",
			Run:  eachConstant("enabled"),
		})),
		counted(rule_testing.EntryOf(test, rule.Rule[rule.NoOptions]{Name: "test-set-off", Run: eachConstant("off")})),
		counted(typeAware),
	}

	walk := func(previous *program.LintCache) (program.Result, *program.LintCache) {
		graph, buildError := program.Build(program.Options{ConfigFileName: filepath.Join(root, "tsconfig.json")})
		if buildError != nil {
			test.Fatalf("building: %v", buildError)
		}
		var loadError error
		graph.LintConfiguration, loadError = configuration.Load(filepath.Join(root, "CohereSettings.json"))
		if loadError != nil {
			test.Fatalf("loading the config: %v", loadError)
		}
		graph.Readiness = &program.Readiness{Options: map[string][]json.RawMessage{
			"test-set-enabled": nil, "test-set-off": nil, typeAware.Name: nil,
		}}
		reuse := program.NewFindingsReuse(program.HashRuleSet([]string{"fixture"}), previous, program.PathAnchor{})
		reuse.MeasureReadiness()
		graph.FindingsReuse = reuse
		result, walkError := graph.Walk(context.Background(), graph.ProjectFiles(), rules)
		if walkError != nil {
			test.Fatalf("walking: %v", walkError)
		}
		return result, reuse.Recorded()
	}
	recordsOf := func(result program.Result) map[string]*program.AdamicRecord {
		records := map[string]*program.AdamicRecord{}
		for fileName, record := range result.Adamic {
			records[filepath.Base(fileName)] = record
		}
		return records
	}

	cold, recorded := walk(nil)
	if runs.Load() == 0 {
		test.Fatal("the recording walk ran no rule, so a warm walk running none proves nothing")
	}
	measureOnly := false
	for _, record := range cold.Adamic {
		for _, count := range record.Counts {
			measureOnly = measureOnly || count.Rule == "test-set-off" && count.Findings > 0
		}
	}
	if !measureOnly {
		test.Fatal("the measure-only rule counted nothing on the recording walk, so its replay is untested here")
	}

	runs.Store(0)
	warm, _ := walk(recorded)
	if got := runs.Load(); got != 0 {
		test.Errorf("the warm walk ran rules %d times; readiness rides the findings cache and should run none", got)
	}
	if warm.FilesReplayed != 3 {
		test.Errorf(
			"the warm walk replayed %d of 3 files; every file's record should serve a run that measures",
			warm.FilesReplayed,
		)
	}
	if warm.Coverage.Suppressed != cold.Coverage.Suppressed || cold.Coverage.Suppressed == 0 {
		test.Errorf("c.ts's directive withheld %d findings warm and %d cold; it should withhold one either way",
			warm.Coverage.Suppressed, cold.Coverage.Suppressed)
	}
	if !reflect.DeepEqual(recordsOf(cold), recordsOf(warm)) {
		// Ignored: the records are plain structs of strings, numbers and slices, which always marshal.
		got, _ := json.Marshal(recordsOf(warm))
		// Ignored: the records are plain structs of strings, numbers and slices, which always marshal.
		want, _ := json.Marshal(recordsOf(cold))
		test.Errorf("the warm walk's records\n got %s\nwant %s", got, want)
	}
}
