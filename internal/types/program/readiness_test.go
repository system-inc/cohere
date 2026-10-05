package program_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
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
func TestReadinessCountsEverySetRuleAndReportsOnlyTheChainsOwn(t *testing.T) {
	t.Parallel()
	directory := writeProject(t, map[string]string{
		"tsconfig.json":       minimalConfig,
		"CohereSettings.json": `{"rules": {"test-set-enabled": "error", "test-set-off": "off", "test-set-skips": "error", "test-other": "error"}}`,
		"a.ts":                "// cohere-disable-next-line test-set-enabled -- suppressed, still counted\nexport const first = 1;\nexport const second = 2;\n",
		"b.ts":                "export const third = 3;\n",
	})
	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory, SingleThreaded: true})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	graph.LintConfig, err = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	graph.Readiness = &program.Readiness{Options: map[string][]json.RawMessage{
		"test-set-enabled": nil, "test-set-off": nil, "test-set-skips": nil,
	}}

	eachConstant := func(id string, fix bool) func(ctx rule.Context, options any) rule.Listeners {
		return func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindVariableDeclaration: func(node *ast.Node) {
				message := rule.Message{Id: id, Description: id}
				if fix {
					ctx.ReportNodeWithFixes(node, message, ctx.RemoveNode(node))
					return
				}
				ctx.ReportNode(node, message)
			}}
		}
	}
	rules := []rule.Rule{
		{Name: "test-other", Run: eachConstant("other", false)},
		{Name: "test-set-enabled", Run: eachConstant("enabled", false)},
		{Name: "test-set-off", Run: eachConstant("off", true)},
		{Name: "test-set-skips", Run: func(ctx rule.Context, options any) rule.Listeners {
			if filepath.Base(ctx.SourceFile.FileName()) == "b.ts" {
				ctx.Skip("b.ts lacks what this rule needs")
				return nil
			}
			return nil
		}, NoListener: rule.NoListenerAnswersInRun},
	}
	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), rules)
	if err != nil {
		t.Fatalf("walking: %v", err)
	}

	reported := map[string]int{}
	for _, diagnostic := range result.Diagnostics {
		reported[diagnostic.RuleName]++
		if diagnostic.RuleName == "test-set-off" {
			t.Errorf("a measure-only rule reported %q, which a reader would see and a fix phase could apply", diagnostic.Message.Id)
		}
	}
	// a.ts's first constant is suppressed for test-set-enabled, so the chain sees one of a.ts's two and b.ts's one.
	if want := map[string]int{"test-other": 3, "test-set-enabled": 2}; !reflect.DeepEqual(reported, want) {
		t.Errorf("reported %v, want %v", reported, want)
	}

	records := map[string]*program.AdamicRecord{}
	for fileName, record := range result.Adamic {
		records[filepath.Base(fileName)] = record
	}
	want := map[string]*program.AdamicRecord{
		"a.ts": {Counts: []program.AdamicCount{{Rule: "test-set-enabled", Findings: 2}, {Rule: "test-set-off", Findings: 2}, {Rule: "test-set-skips", Findings: 0}}},
		"b.ts": {Counts: []program.AdamicCount{{Rule: "test-set-enabled", Findings: 1}, {Rule: "test-set-off", Findings: 1}, {Rule: "test-set-skips", Findings: 0}},
			Skipped: []string{"test-set-skips"}},
	}
	if !reflect.DeepEqual(records, want) {
		got, _ := json.Marshal(records)
		expected, _ := json.Marshal(want)
		t.Errorf("readiness records\n got %s\nwant %s", got, expected)
	}
	if records["b.ts"].Measured() || !records["a.ts"].Measured() {
		t.Error("a file a set rule skipped must read unmeasured, and one every set rule ran on measured")
	}
	if records["a.ts"].Findings() != 4 {
		t.Errorf("a.ts totals %d findings, want 4", records["a.ts"].Findings())
	}
	// The measure-only rule's notes stay out of the run's, and its skip-free note set is empty.
	for fileName, notes := range result.Notes {
		if _, present := notes["test-set-off"]; present {
			t.Errorf("%s carries a measure-only rule's notes into the run's", fileName)
		}
	}
}

// TestReadinessOffMeasuresNothing: without Readiness the walk records no measurement and runs no rule
// the chain leaves off, which is every run before readiness existed.
func TestReadinessOffMeasuresNothing(t *testing.T) {
	t.Parallel()
	directory := writeProject(t, map[string]string{
		"tsconfig.json":       minimalConfig,
		"CohereSettings.json": `{"rules": {"test-set-off": "off", "test-other": "error"}}`,
		"a.ts":                "export const first = 1;\n",
	})
	graph, err := program.Build(program.Options{ConfigFileName: "tsconfig.json", CurrentDirectory: directory, SingleThreaded: true})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	graph.LintConfig, err = configuration.Load(filepath.Join(directory, "CohereSettings.json"))
	if err != nil {
		t.Fatalf("loading the config: %v", err)
	}
	ran := false
	rules := []rule.Rule{
		{Name: "test-other", Run: func(ctx rule.Context, options any) rule.Listeners { return nil }, NoListener: rule.NoListenerAnswersInRun},
		{Name: "test-set-off", Run: func(ctx rule.Context, options any) rule.Listeners { ran = true; return nil }, NoListener: rule.NoListenerAnswersInRun},
	}
	result, err := graph.Walk(context.Background(), graph.ProjectFiles(), rules)
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	if result.Adamic != nil || ran {
		t.Errorf("with readiness off the walk recorded %v and ran the off rule: %t", result.Adamic, ran)
	}
}
