//go:build darwin || linux

package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
	"golang.org/x/sys/unix"
)

func budgetThreadCpu() time.Duration {
	var spec unix.Timespec
	if clockGettimeError := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &spec); clockGettimeError != nil {
		panic(clockGettimeError)
	}
	return time.Duration(spec.Nano())
}

// The sleeper waits longer than the CPU budget but less than the safety deadline.
// The spinner supplies the positive control using the same thread clock as --timing.
func TestRuleBudgetRejectsCpuWorkAndDoesNotBillWaiting(test *testing.T) {
	test.Parallel()
	graph := buildBudgetGraph(test, map[string]string{"file.ts": "export const value = 1;\n"})
	sleeper := rule.Rule[rule.NoOptions]{Name: "planted-waiter", Run: func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners {
		return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) { time.Sleep(350 * time.Millisecond) }}
	}}
	rows, err := checkRuleBudget(test, graph, rule_testing.EntriesOf(test, sleeper), 250*time.Millisecond)
	if err != nil || len(rows) != 1 || rows[0].NodesOffered != 1 || rows[0].TotalCPU() > 25*time.Millisecond {
		test.Fatalf("waiter was billed CPU or not run: %v %+v", err, rows)
	}
	spinner := rule.Rule[rule.NoOptions]{Name: "planted-spinner", Run: func(rule.Context, rule.Given[rule.NoOptions]) rule.Listeners {
		return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
			start := budgetThreadCpu()
			for budgetThreadCpu()-start < 350*time.Millisecond {
			}
		}}
	}}
	rows, err = checkRuleBudget(test, graph, rule_testing.EntriesOf(test, spinner), 250*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "rule CPU budget exceeded: planted-spinner") {
		test.Fatalf("spinner was not refused: %v %+v", err, rows)
	}
	test.Logf("waiter passed; spinner refused: %v", err)
}

// A rule that never returns must not hang the gate. Its dedicated subprocess is
// also how the real land run bounds an abandoned walk: the process ends at once.
func TestRuleBudgetBoundsANonReturningListener(test *testing.T) {
	test.Parallel()
	const helper = "COHERE_BUDGET_DEADLINE_CONTROL"
	if os.Getenv(helper) == "1" {
		graph := buildBudgetGraph(test, map[string]string{"file.ts": "export const value = 1;\n"})
		stuck := rule.Rule[rule.NoOptions]{
			Name: "planted-nonreturning",
			Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
				return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) { select {} }}
			},
		}
		_, checkRuleBudgetError := checkRuleBudget(test, graph, rule_testing.EntriesOf(test, stuck), 20*time.Millisecond)
		if checkRuleBudgetError != nil {
			test.Fatal(checkRuleBudgetError)
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		test.Fatal(err)
	}
	command := exec.Command(
		binary,
		"-test.run=^TestRuleBudgetBoundsANonReturningListener$",
		"-test.v",
		"-test.timeout=5s",
	)
	command.Env = append(os.Environ(), helper+"=1")
	output, err := command.CombinedOutput()
	text := string(output)
	if err == nil ||
		!strings.Contains(text, "rule CPU budget exceeded: planted-nonreturning reached deadline 60.000ms") ||
		!strings.Contains(text, "rules run=0 registered=1 corpus nodes=8 files=1") {
		test.Fatalf("unbounded or unnamed refusal (%v):\n%s", err, text)
	}
	test.Logf("bounded listener refused:\n%s", text)
}

// Preserve the supplied 6911526c implementation and replay it through Go's source
// overlay, never by editing a working rule. Older public main lacks its registration
// and message: the overlay adds those too, without removing any existing rule.
// When the fixed rule is present only its implementation moves.
func TestRuleBudgetRefuses6911526cThroughGoOverlay(test *testing.T) {
	test.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		test.Fatal(err)
	}
	directory := test.TempDir()
	replacements := map[string]string{}
	add := func(target string, source []byte) {
		test.Helper()
		path := filepath.Join(directory, fmt.Sprintf("overlay-%d%s", len(replacements), filepath.Ext(target)))
		if writeError := os.WriteFile(path, source, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		replacements[filepath.Join(root, target)] = path
	}
	source, err := os.ReadFile("testdata/rule-budget/6911526c.go.txt")
	if err != nil {
		test.Fatal(err)
	}
	target := "internal/lint/rules/adamic/no_cross_enum_narrowing.go"
	if _, statError := os.Stat(filepath.Join(root, target)); os.IsNotExist(statError) {
		add("internal/lint/rules/adamic/no_cross_enum_narrowing_register.go", []byte(`package adamic
import "github.com/system-inc/cohere/internal/lint/rule"
func init() { rule.Register(rule.Registration[rule.NoOptions]{Rule: NoCrossEnumNarrowing}) }
`))
		add("policy/messages/no-cross-enum-narrowing.json", []byte(
			`{"rules":{"TypeScript":"adamic/no-cross-enum-narrowing"},"messages":{"crossEnumNarrowing":{"text":"{{other}} collides with {{member}} at {{value}}"}}}`,
		))
	} else if statError != nil {
		test.Fatal(statError)
	}
	add(target, source)
	overlay, err := json.Marshal(map[string]any{"Replace": replacements})
	if err != nil {
		test.Fatal(err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if writeError := os.WriteFile(overlayPath, overlay, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	// Uncached: the child run measures a rule's CPU time, so it has to run again each time rather than replay a
	// cached result from a run that happened to land under the budget.
	command := exec.Command(
		"go",
		"test",
		"-overlay="+overlayPath,
		"./internal/lint/registry",
		"-count=1",
		"-timeout=30s",
		"-run=^TestRegisteredRuleCPUBudget$",
		"-v",
	)
	command.Dir = root
	output, err := command.CombinedOutput()
	text := string(output)
	if err == nil ||
		!strings.Contains(text, "rule CPU budget exceeded: adamic/no-cross-enum-narrowing") ||
		!strings.Contains(text, "rule CPU coverage:") {
		test.Fatalf("historical rule was not refused by name (%v):\n%s", err, text)
	}
	test.Logf("6911526c refused:\n%s", text)
}
