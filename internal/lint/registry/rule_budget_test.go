package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// The budget guards catastrophic outliers, not small differences between rules.
// Shared checker work is charged to the first rule that asks; attribution can move
// 50–100% between neighbours. Run rules sequentially with generous headroom
// (100x the slowest healthy row), and never retry a failure.
// Calibration on 2026-10-20: 495 registered rules, seven files, 5,305
// nodes. The slowest healthy row was 3.633ms of thread CPU; 600ms is 165x.
// Keep the corpus small: increasing its union width is deliberately nonlinear.
const ruleCPUBudget = 600 * time.Millisecond

func enumBudgetSource(members, comparisons int, numeric bool) string {
	var s strings.Builder
	s.WriteString("export enum Kind {\n")
	for i := range members {
		if numeric {
			fmt.Fprintf(&s, "M%d = %d,\n", i, i)
		} else {
			fmt.Fprintf(&s, "M%d = 'm%d',\n", i, i)
		}
	}
	s.WriteString("}\n")
	for i := range members {
		fmt.Fprintf(&s, "export interface N%d { type: Kind.M%d; parent: Node; value%d: number }\n", i, i, i)
	}
	s.WriteString("export type Node = ")
	for i := range members {
		if i > 0 {
			s.WriteString(" | ")
		}
		fmt.Fprintf(&s, "N%d", i)
	}
	s.WriteString(";\nexport function visit(node: Node): number { let count = 0;\n")
	for i := range comparisons {
		fmt.Fprintf(&s, "if (node.parent.parent.type === Kind.M%d) { count += 1; }\n", i%members)
	}
	s.WriteString("return count; }\n")
	return s.String()
}

// Generated here, never dependent on an external checkout or installed declarations.
// The recursive receiver union is essential: flattening parent to a small type
// removes the combinatorial expansion in 6911526c's declared-property recovery.
func ruleBudgetCorpus() map[string]string {
	files := map[string]string{
		"enum-string.ts": enumBudgetSource(64, 8, false),
		"enum-number.ts": enumBudgetSource(64, 8, true),
	}
	var s strings.Builder
	s.WriteString("interface Link { next: Link; value: number }\nexport function deep(node: Link) { return node")
	for range 32 {
		s.WriteString(".next")
	}
	s.WriteString(".value; }\n")
	files["chain.ts"] = s.String()
	s.Reset()
	for i := range 60 {
		fmt.Fprintf(&s, "export function f%d(x: number) { const y = x + 1; return y * y; }\n", i)
	}
	files["functions.ts"] = s.String()
	s.Reset()
	s.WriteString("export function nested(x: number) {\n")
	for range 24 {
		s.WriteString("if (x > 0) {\n")
	}
	s.WriteString("x = ")
	for range 24 {
		s.WriteString("(")
	}
	s.WriteString("x + 1")
	for range 24 {
		s.WriteString(")")
	}
	s.WriteString(";\n")
	for range 24 {
		s.WriteString("}\n")
	}
	s.WriteString("return x; }\n")
	files["nested.ts"] = s.String()
	s.Reset()
	s.WriteString("export const wide = {\n")
	for i := range 120 {
		fmt.Fprintf(&s, "key%d: %d,\n", i, i)
	}
	s.WriteString("};\n")
	files["object.ts"] = s.String()
	s.Reset()
	s.WriteString("export type Tag = ")
	for i := range 200 {
		if i > 0 {
			s.WriteString(" | ")
		}
		fmt.Fprintf(&s, "'tag%d'", i)
	}
	s.WriteString(";\nexport function tag(value: Tag) { return value === 'tag199'; }\n")
	files["literals.ts"] = s.String()
	return files
}

func buildBudgetGraph(t *testing.T, files map[string]string) *program.Graph {
	t.Helper()
	directory := t.TempDir()
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "tsconfig.json"), []byte(typedCorpusConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json"), CurrentDirectory: directory, SingleThreaded: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.ProjectFiles()) != len(files) {
		t.Fatalf("built %d files, want %d", len(graph.ProjectFiles()), len(files))
	}
	if diagnostics := graph.Program.GetSyntacticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		t.Fatalf("generated corpus has %d syntax errors", len(diagnostics))
	}
	graph.CollectTimings = true
	return graph
}

// measureOne runs just one rule at a time. A deadline also bounds rules that
// never return from a listener; context cancellation alone cannot interrupt them.
// The caller must return immediately on a deadline, ending the dedicated test
// process. The deadline is a separate safety bound, never reported as CPU time.
func measureOne(graph *program.Graph, subject rule.Rule, deadline time.Duration) (program.Result, error) {
	type answer struct {
		result program.Result
		err    error
	}
	done := make(chan answer, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		result, err := graph.Walk(ctx, graph.ProjectFiles(), []rule.Rule{subject})
		done <- answer{result, err}
	}()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case got := <-done:
		return got.result, got.err
	case <-timer.C:
		return program.Result{}, fmt.Errorf("rule CPU budget exceeded: %s reached deadline %.3fms (CPU budget %.3fms; CPU measurement unfinished)", subject.Name, float64(deadline)/float64(time.Millisecond), float64(deadline/3)/float64(time.Millisecond))
	}
}

func checkRuleBudget(t *testing.T, graph *program.Graph, rules []rule.Rule, budget time.Duration) ([]program.RuleTiming, error) {
	t.Helper()
	started := time.Now()
	var rows []program.RuleTiming
	nodes := 0
	var countNodes func(*ast.Node)
	countNodes = func(node *ast.Node) {
		nodes++
		node.ForEachChild(func(child *ast.Node) bool { countNodes(child); return false })
	}
	for _, file := range graph.ProjectFiles() {
		countNodes(file.AsNode())
	}
	// Coverage precedes every verdict, including a deadline or an incomplete walk.
	defer func() {
		t.Logf("rule CPU coverage: rules run=%d registered=%d corpus nodes=%d files=%d wall=%s", len(rows), len(rules), nodes, len(graph.ProjectFiles()), time.Since(started))
	}()
	for _, subject := range rules {
		result, err := measureOne(graph, subject, budget*3)
		if err != nil {
			return rows, err
		}
		if result.Timings == nil || result.Timings.Account.Unavailable != "" {
			return rows, fmt.Errorf("%s: no thread CPU measurement", subject.Name)
		}
		if len(result.Coverage.RulesCrashed) != 0 || len(result.Coverage.FilesCrashed) != 0 {
			return rows, fmt.Errorf("%s: incomplete walk: rule crashes=%v file crashes=%v", subject.Name, result.Coverage.RulesCrashed, result.Coverage.FilesCrashed)
		}
		if result.Coverage.RulesOffered[subject.Name] != len(graph.ProjectFiles()) {
			return rows, fmt.Errorf("%s offered %d files, want %d", subject.Name, result.Coverage.RulesOffered[subject.Name], len(graph.ProjectFiles()))
		}
		row := result.Timings.Sorted()[0]
		rows = append(rows, row)
		if row.TotalCPU() > budget {
			return rows, fmt.Errorf("rule CPU budget exceeded: %s %.3fms > %.3fms", row.Name, float64(row.TotalCPU())/float64(time.Millisecond), float64(budget)/float64(time.Millisecond))
		}
	}
	return rows, nil
}

// Invoked separately by cohere-dev land with -count=1: a cached test verdict must
// never answer a performance question on a busy landing machine.
func TestRegisteredRuleCPUBudget(t *testing.T) {
	t.Parallel()
	started := time.Now()
	rules := All()
	if len(rules) == 0 {
		t.Fatal("empty registry")
	}
	graph := buildBudgetGraph(t, ruleBudgetCorpus())
	rows, err := checkRuleBudget(t, graph, rules, ruleCPUBudget)
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TotalCPU() > rows[j].TotalCPU() })
	for i, row := range rows {
		if i == 10 {
			break
		}
		t.Logf("rule CPU: %s %.3fms", row.Name, float64(row.TotalCPU())/float64(time.Millisecond))
	}
	t.Logf("rule CPU budget check elapsed: %s", time.Since(started))
}
