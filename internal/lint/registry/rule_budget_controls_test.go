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
	"golang.org/x/sys/unix"
)

func budgetThreadCPU() time.Duration {
	var spec unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &spec); err != nil {
		panic(err)
	}
	return time.Duration(spec.Nano())
}

// The sleeper waits longer than the CPU budget but less than the safety deadline.
// The spinner supplies the positive control using the same thread clock as --timing.
func TestRuleBudgetRejectsCPUWorkAndDoesNotBillWaiting(t *testing.T) {
	t.Parallel()
	graph := buildBudgetGraph(t, map[string]string{"file.ts": "export const value = 1;\n"})
	sleeper := rule.Rule{Name: "planted-waiter", Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) { time.Sleep(350 * time.Millisecond) }}
	}}
	rows, err := checkRuleBudget(t, graph, []rule.Rule{sleeper}, 250*time.Millisecond)
	if err != nil || len(rows) != 1 || rows[0].NodesOffered != 1 || rows[0].TotalCPU() > 25*time.Millisecond {
		t.Fatalf("waiter was billed CPU or not run: %v %+v", err, rows)
	}
	spinner := rule.Rule{Name: "planted-spinner", Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) {
			start := budgetThreadCPU()
			for budgetThreadCPU()-start < 350*time.Millisecond {
			}
		}}
	}}
	rows, err = checkRuleBudget(t, graph, []rule.Rule{spinner}, 250*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "rule CPU budget exceeded: planted-spinner") {
		t.Fatalf("spinner was not refused: %v %+v", err, rows)
	}
	t.Logf("waiter passed; spinner refused: %v", err)
}

// A rule that never returns must not hang the gate. Its dedicated subprocess is
// also how the real land run bounds an abandoned walk: the process ends at once.
func TestRuleBudgetBoundsANonReturningListener(t *testing.T) {
	t.Parallel()
	const helper = "COHERE_BUDGET_DEADLINE_CONTROL"
	if os.Getenv(helper) == "1" {
		graph := buildBudgetGraph(t, map[string]string{"file.ts": "export const value = 1;\n"})
		stuck := rule.Rule{Name: "planted-nonreturning", Run: func(ctx rule.Context, options any) rule.Listeners {
			return rule.Listeners{ast.KindSourceFile: func(node *ast.Node) { select {} }}
		}}
		_, err := checkRuleBudget(t, graph, []rule.Rule{stuck}, 20*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "-test.run=^TestRuleBudgetBoundsANonReturningListener$", "-test.v", "-test.timeout=5s")
	command.Env = append(os.Environ(), helper+"=1")
	output, err := command.CombinedOutput()
	text := string(output)
	if err == nil || !strings.Contains(text, "rule CPU budget exceeded: planted-nonreturning reached deadline 60.000ms") ||
		!strings.Contains(text, "rules run=0 registered=1 corpus nodes=8 files=1") {
		t.Fatalf("unbounded or unnamed refusal (%v):\n%s", err, text)
	}
	t.Logf("bounded listener refused:\n%s", text)
}

// Preserve the supplied 6911526c implementation and replay it through Go's source
// overlay, never by editing a working rule. Older public main lacks its registration
// and message: the overlay adds those too, without removing any existing rule.
// When the fixed rule is present only its implementation moves.
func TestRuleBudgetRefuses6911526cThroughGoOverlay(t *testing.T) {
	t.Parallel()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	replacements := map[string]string{}
	add := func(target string, source []byte) {
		t.Helper()
		path := filepath.Join(directory, fmt.Sprintf("overlay-%d%s", len(replacements), filepath.Ext(target)))
		if err := os.WriteFile(path, source, 0o644); err != nil {
			t.Fatal(err)
		}
		replacements[filepath.Join(root, target)] = path
	}
	source, err := os.ReadFile("testdata/rule-budget/6911526c.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	target := "internal/lint/rules/adamic/no_cross_enum_narrowing.go"
	if _, err := os.Stat(filepath.Join(root, target)); os.IsNotExist(err) {
		add("internal/lint/rules/adamic/no_cross_enum_narrowing_register.go", []byte(`package adamic
import "github.com/system-inc/cohere/internal/lint/rule"
func init() { rule.Register(rule.Registration{Rule: NoCrossEnumNarrowing}) }
`))
		add("policy/messages/no-cross-enum-narrowing.json", []byte(`{"rules":{"TypeScript":"adamic/no-cross-enum-narrowing"},"messages":{"crossEnumNarrowing":{"text":"{{other}} collides with {{member}} at {{value}}"}}}`))
	} else if err != nil {
		t.Fatal(err)
	}
	add(target, source)
	overlay, err := json.Marshal(map[string]any{"Replace": replacements})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-overlay="+overlayPath, "./internal/lint/registry", "-count=1", "-timeout=30s", "-run=^TestRegisteredRuleCPUBudget$", "-v")
	command.Dir = root
	output, err := command.CombinedOutput()
	text := string(output)
	if err == nil || !strings.Contains(text, "rule CPU budget exceeded: adamic/no-cross-enum-narrowing") || !strings.Contains(text, "rule CPU coverage:") {
		t.Fatalf("historical rule was not refused by name (%v):\n%s", err, text)
	}
	t.Logf("6911526c refused:\n%s", text)
}
