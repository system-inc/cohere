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
// 50 to 100% between neighbours. Run rules sequentially with generous headroom
// (100x the slowest healthy row), and never retry a failure.
// Calibration on 2026-10-20: 495 registered rules, seven files, 5,305
// nodes. The slowest healthy row was 3.633ms of thread CPU; 600ms is 165x.
// Keep the corpus small: increasing its union width is deliberately nonlinear.
const ruleCpuBudget = 600 * time.Millisecond

func enumBudgetSource(members, comparisons int, numeric bool) string {
	var builder strings.Builder
	builder.WriteString("export enum Kind {\n")
	for index := range members {
		if numeric {
			fmt.Fprintf(&builder, "M%d = %d,\n", index, index)
		} else {
			fmt.Fprintf(&builder, "M%d = 'm%d',\n", index, index)
		}
	}
	builder.WriteString("}\n")
	for index := range members {
		fmt.Fprintf(&builder, "export interface N%d { type: Kind.M%d; parent: Node; value%d: number }\n",
			index, index, index)
	}
	builder.WriteString("export type Node = ")
	for index := range members {
		if index > 0 {
			builder.WriteString(" | ")
		}
		fmt.Fprintf(&builder, "N%d", index)
	}
	builder.WriteString(";\nexport function visit(node: Node): number { let count = 0;\n")
	for index := range comparisons {
		fmt.Fprintf(&builder, "if (node.parent.parent.type === Kind.M%d) { count += 1; }\n", index%members)
	}
	builder.WriteString("return count; }\n")
	return builder.String()
}

// Generated here, never dependent on an external checkout or installed declarations.
// The recursive receiver union is essential: flattening parent to a small type
// removes the combinatorial expansion in 6911526c's declared-property recovery.
func ruleBudgetCorpus() map[string]string {
	files := map[string]string{
		"enum-string.ts": enumBudgetSource(64, 8, false),
		"enum-number.ts": enumBudgetSource(64, 8, true),
	}
	var builder strings.Builder
	builder.WriteString("interface Link { next: Link; value: number }\nexport function deep(node: Link) { return node")
	for range 32 {
		builder.WriteString(".next")
	}
	builder.WriteString(".value; }\n")
	files["chain.ts"] = builder.String()
	builder.Reset()
	for index := range 60 {
		fmt.Fprintf(&builder, "export function f%d(x: number) { const y = x + 1; return y * y; }\n", index)
	}
	files["functions.ts"] = builder.String()
	builder.Reset()
	builder.WriteString("export function nested(x: number) {\n")
	for range 24 {
		builder.WriteString("if (x > 0) {\n")
	}
	builder.WriteString("x = ")
	for range 24 {
		builder.WriteString("(")
	}
	builder.WriteString("x + 1")
	for range 24 {
		builder.WriteString(")")
	}
	builder.WriteString(";\n")
	for range 24 {
		builder.WriteString("}\n")
	}
	builder.WriteString("return x; }\n")
	files["nested.ts"] = builder.String()
	builder.Reset()
	builder.WriteString("export const wide = {\n")
	for index := range 120 {
		fmt.Fprintf(&builder, "key%d: %d,\n", index, index)
	}
	builder.WriteString("};\n")
	files["object.ts"] = builder.String()
	builder.Reset()
	builder.WriteString("export type Tag = ")
	for index := range 200 {
		if index > 0 {
			builder.WriteString(" | ")
		}
		fmt.Fprintf(&builder, "'tag%d'", index)
	}
	builder.WriteString(";\nexport function tag(value: Tag) { return value === 'tag199'; }\n")
	files["literals.ts"] = builder.String()
	return files
}

func buildBudgetGraph(test *testing.T, files map[string]string) *program.Graph {
	test.Helper()
	directory := test.TempDir()
	for name, source := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if writeError := os.WriteFile(
		filepath.Join(directory, "tsconfig.json"),
		[]byte(typedCorpusConfig),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(directory, "tsconfig.json"),
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
	}
	if len(graph.ProjectFiles()) != len(files) {
		test.Fatalf("built %d files, want %d", len(graph.ProjectFiles()), len(files))
	}
	if diagnostics := graph.Program.GetSyntacticDiagnostics(context.Background(), nil); len(diagnostics) != 0 {
		test.Fatalf("generated corpus has %d syntax errors", len(diagnostics))
	}
	graph.CollectTimings = true
	return graph
}

// measureOne runs just one rule at a time. A deadline also bounds rules that
// never return from a listener; context cancellation alone cannot interrupt them.
// The caller must return immediately on a deadline, ending the dedicated test
// process. The deadline is a separate safety bound, never reported as CPU time.
func measureOne(graph *program.Graph, subject rule.Entry, deadline time.Duration) (program.Result, error) {
	type answer struct {
		result program.Result
		err    error
	}
	done := make(chan answer, 1)
	cancelContext, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		result, walkError := graph.Walk(cancelContext, graph.ProjectFiles(), []rule.Entry{subject})
		done <- answer{result, walkError}
	}()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case got := <-done:
		return got.result, got.err
	case <-timer.C:
		return program.Result{}, fmt.Errorf(
			"rule CPU budget exceeded: %s reached deadline %.3fms (CPU budget %.3fms; CPU measurement unfinished)",
			subject.Name,
			float64(deadline)/float64(time.Millisecond),
			float64(deadline/3)/float64(time.Millisecond),
		)
	}
}

func checkRuleBudget(
	test *testing.T,
	graph *program.Graph,
	rules []rule.Entry,
	budget time.Duration,
) ([]program.RuleTiming, error) {
	test.Helper()
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
		test.Logf(
			"rule CPU coverage: rules run=%d registered=%d corpus nodes=%d files=%d wall=%s",
			len(rows),
			len(rules),
			nodes,
			len(graph.ProjectFiles()),
			time.Since(started),
		)
	}()
	for _, subject := range rules {
		result, measureOneError := measureOne(graph, subject, budget*3)
		if measureOneError != nil {
			return rows, measureOneError
		}
		if result.Timings == nil || result.Timings.Account.Unavailable != "" {
			return rows, fmt.Errorf("%s: no thread CPU measurement", subject.Name)
		}
		if len(result.Coverage.RulesCrashed) != 0 || len(result.Coverage.FilesCrashed) != 0 {
			return rows, fmt.Errorf(
				"%s: incomplete walk: rule crashes=%v file crashes=%v",
				subject.Name,
				result.Coverage.RulesCrashed,
				result.Coverage.FilesCrashed,
			)
		}
		if result.Coverage.RulesOffered[subject.Name] != len(graph.ProjectFiles()) {
			return rows, fmt.Errorf(
				"%s offered %d files, want %d",
				subject.Name,
				result.Coverage.RulesOffered[subject.Name],
				len(graph.ProjectFiles()),
			)
		}
		row := result.Timings.Sorted()[0]
		rows = append(rows, row)
		if row.TotalCpu() > budget {
			return rows, fmt.Errorf(
				"rule CPU budget exceeded: %s %.3fms > %.3fms",
				row.Name,
				float64(row.TotalCpu())/float64(time.Millisecond),
				float64(budget)/float64(time.Millisecond),
			)
		}
	}
	return rows, nil
}

// Invoked separately by cohere-dev land with -count=1: a cached test verdict must
// never answer a performance question on a busy landing machine.
func TestRegisteredRuleCPUBudget(test *testing.T) {
	test.Parallel()
	started := time.Now()
	rules := All()
	if len(rules) == 0 {
		test.Fatal("empty registry")
	}
	graph := buildBudgetGraph(test, ruleBudgetCorpus())
	rows, checkRuleBudgetError := checkRuleBudget(test, graph, rules, ruleCpuBudget)
	if checkRuleBudgetError != nil {
		test.Fatal(checkRuleBudgetError)
	}
	sort.Slice(rows, func(left, right int) bool { return rows[left].TotalCpu() > rows[right].TotalCpu() })
	for index, row := range rows {
		if index == 10 {
			break
		}
		test.Logf("rule CPU: %s %.3fms", row.Name, float64(row.TotalCpu())/float64(time.Millisecond))
	}
	test.Logf("rule CPU budget check elapsed: %s", time.Since(started))
}
