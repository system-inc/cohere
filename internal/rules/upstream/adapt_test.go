package upstream

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/system-inc/verify/internal/program"
	"github.com/system-inc/verify/internal/rule"
	upstreamrule "github.com/system-inc/verify/internal/upstream/tsgolint/rule"
	"github.com/system-inc/verify/internal/upstream/tsgolint/rules/await_thenable"
)

// TestAdaptedRuleAgreesOnBothDirections is the deliverable, and compilation is not.
//
// An adapter that wraps a rule and never lets it fire compiles perfectly and reports zero findings,
// which on a clean tree is indistinguishable from an adapter that works. So the proof has to be a
// planted violation each way: source that must produce a finding, and source that must not.
//
// await-thenable is the right rule to prove it with, because it is genuinely type-aware. It asks
// the checker whether the awaited expression is a Thenable, so a wrapper that failed to pass the
// checker through would fail here rather than pass silently.
func TestAdaptedRuleAgreesOnBothDirections(t *testing.T) {
	adapted, err := Adapt(await_thenable.AwaitThenableRule)
	if err != nil {
		t.Fatalf("adapting await-thenable: %v", err)
	}
	if adapted.Name != "await-thenable" {
		t.Fatalf("adapted rule lost its name: %q", adapted.Name)
	}

	t.Run("fires on awaiting a non-thenable", func(t *testing.T) {
		findings := runAgainstProgram(t, `
			async function main() {
				const value = 3;
				return await value;
			}
		`, adapted)

		if len(findings) != 1 {
			t.Fatalf("expected 1 finding for `await 3`, got %d: %v", len(findings), describe(findings))
		}
		if findings[0].RuleName != "await-thenable" {
			t.Fatalf("finding reported under the wrong rule name: %q", findings[0].RuleName)
		}
		// A finding that reported under a different name would be unsuppressable by the comment its
		// author actually wrote, which is a worse defect than not firing.
		if findings[0].Message.Id == "" {
			t.Fatal("finding carries no message id, so it cannot be suppressed or tracked")
		}
	})

	t.Run("stays silent on awaiting a real promise", func(t *testing.T) {
		findings := runAgainstProgram(t, `
			async function main() {
				const value = Promise.resolve(3);
				return await value;
			}
		`, adapted)

		if len(findings) != 0 {
			t.Fatalf("expected no findings for `await Promise`, got %d: %v", len(findings), describe(findings))
		}
	})
}

// TestAdaptRefusesRatherThanRunsBlind is the guard that matters most about this adapter.
//
// tsgolint encodes exit visits as pseudo-kinds (`ListenerOnExit(kind)` is `kind + 1000`). verify's
// walk has no exit visit, so such a listener would be looked up against real node kinds, never
// match, and never fire — a rule that runs and reports nothing, which looks exactly like a rule
// that found nothing.
func TestAdaptRefusesRatherThanRunsBlind(t *testing.T) {
	exitOnly := upstreamrule.RuleListeners{
		upstreamrule.ListenerOnExit(ast.KindCallExpression): func(node *ast.Node) {},
	}
	if !UsesExitListeners(exitOnly) {
		t.Fatal("an exit listener was not recognized as one")
	}

	plain := upstreamrule.RuleListeners{
		ast.KindCallExpression: func(node *ast.Node) {},
	}
	if UsesExitListeners(plain) {
		t.Fatal("a plain kind listener was misread as an exit listener")
	}
}

func TestAdaptRejectsAnUnusableRule(t *testing.T) {
	if _, err := Adapt(upstreamrule.Rule{Name: "", Run: nil}); err == nil {
		t.Fatal("a nameless rule with no Run should not adapt")
	}
	if _, err := Adapt(upstreamrule.Rule{Name: "named", Run: nil}); err == nil {
		t.Fatal("a rule with no Run should not adapt")
	}
}

// runAgainstProgram builds a real type graph over one file and walks it with one rule.
//
// A real program is required rather than a bare parse: await-thenable answers by asking the checker
// what a type is, and a parse-only harness would hand it a nil checker and prove nothing.
func runAgainstProgram(t *testing.T, source string, subject rule.Rule) []rule.Diagnostic {
	t.Helper()

	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "Subject.ts")
	if err := os.WriteFile(sourcePath, []byte(strings.TrimSpace(source)+"\n"), 0o644); err != nil {
		t.Fatalf("writing the fixture: %v", err)
	}

	configPath := filepath.Join(directory, "tsconfig.json")
	config := `{"compilerOptions":{"strict":true,"target":"ES2022","lib":["ES2022"],"moduleDetection":"force","types":[]},"include":["*.ts"]}`
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("writing the tsconfig: %v", err)
	}

	graph, err := program.Build(program.Options{
		ConfigFileName:   configPath,
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if err != nil {
		t.Fatalf("building the type graph: %v", err)
	}

	files := graph.ProjectFiles()
	if len(files) == 0 {
		// An empty file set would make every assertion below pass vacuously, which is the exact
		// failure this project keeps finding. Fail loudly instead.
		t.Fatal("the program contains no project files, so this test would prove nothing")
	}

	result, err := graph.Walk(context.Background(), files, []rule.Rule{subject})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	return result.Diagnostics
}

func describe(findings []rule.Diagnostic) []string {
	described := make([]string, 0, len(findings))
	for _, finding := range findings {
		described = append(described, finding.RuleName+"/"+finding.Message.Id)
	}
	return described
}
