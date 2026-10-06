package adamic

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// stableTextFiles are a file whose array literal makes the checker normalize `extra` and `note` first, and the
// case: its first column lacks `extra`, which only the second declares, so normalization adds `extra?: undefined`
// to it. TypeScript caches that member by name, per checker, with the declaration of whichever sibling it was first
// made from, and prints members in declaration order (#dq881as). Met first in Another.ts, which comes before the
// case in the program, the member prints first; met only in the case, it prints after the column's own members.
var stableTextFiles = map[string]string{
	"Another.ts": "export const others = [{ extra: 1 }, { note: 'other' }];\n",
	"Case.ts": `interface Column { id: string; extra?: number; note?: string; label?: string }
export const columns: () => Column[] = function() {
	return [{ id: 'a', note: 'first' }, { id: 'b', extra: 2 }];
};
`,
}

// relationMessages walks Case.ts with the two relation rules on a program checked by one checker, after asking it
// about Another.ts first when anotherFirst is set, and returns each rule's message there.
func relationMessages(t *testing.T, anotherFirst bool) map[string]string {
	t.Helper()
	directory := t.TempDir()
	writeAdamicSupport(t)(directory)
	for name, source := range stableTextFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json"), SingleThreaded: true})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	another := graph.Program.GetSourceFile(filepath.Join(directory, "Another.ts"))
	caseFile := graph.Program.GetSourceFile(filepath.Join(directory, "Case.ts"))
	if another == nil || caseFile == nil {
		t.Fatal("the fixture's files are not in the program")
	}
	if anotherFirst {
		graph.Program.GetSemanticDiagnostics(context.Background(), another)
	}
	result, err := graph.Walk(context.Background(), []*ast.SourceFile{caseFile}, []rule.Rule{InvariantMutable, NoOptionalWidening})
	if err != nil {
		t.Fatalf("walking: %v", err)
	}
	messages := map[string]string{}
	for _, diagnostic := range result.Diagnostics {
		messages[diagnostic.RuleName] = diagnostic.Message.Description
	}
	return messages
}

// TestTheRelationRulesPrintTheSameTextWhateverTheCheckerMetFirst: the two relation rules name the same types in
// the same words whether the checker normalized another file's literals first or not, which at GOMAXPROCS=16 is
// a matter of scheduling (#dq881as). TypeToString printed `extra?: undefined` first in one and last in the other.
// The fixture is held to that: each rule must report in both orders, or the comparison proves nothing.
func TestTheRelationRulesPrintTheSameTextWhateverTheCheckerMetFirst(t *testing.T) {
	t.Parallel()
	alone, afterAnother := relationMessages(t, false), relationMessages(t, true)
	for _, ruleName := range []string{InvariantMutable.Name, NoOptionalWidening.Name} {
		if alone[ruleName] == "" || afterAnother[ruleName] == "" {
			t.Fatalf("%s did not report in both orders (%q, %q), so this compares nothing", ruleName, alone[ruleName], afterAnother[ruleName])
		}
		if alone[ruleName] != afterAnother[ruleName] {
			t.Errorf("%s reads differently once the checker met Another.ts first:\n  alone:  %s\n  after:  %s", ruleName, alone[ruleName], afterAnother[ruleName])
		}
	}
}
