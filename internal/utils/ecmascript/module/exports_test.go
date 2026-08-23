package module

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

// firstStatement parses source text and returns its first statement.
func firstStatement(t *testing.T, sourceText string) *ast.Node {
	t.Helper()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/repository/source/Thing.tsx",
		Path:     tspath.Path("/repository/source/Thing.tsx"),
	}, sourceText, core.ScriptKindTSX)
	if file == nil || file.Statements == nil || len(file.Statements.Nodes) == 0 {
		t.Fatalf("no statement in %q", sourceText)
	}
	return file.Statements.Nodes[0]
}

// Both modifiers are required rather than either, which is the whole decision this makes.
func TestIsDefaultExported(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		want       bool
	}{
		{"an exported default function", "export default function run() {}\n", true},
		{"an exported default class", "export default class Thing {}\n", true},
		{"an anonymous default function", "export default function () {}\n", true},
		{"exported without default", "export function run() {}\n", false},
		{"neither modifier", "function run() {}\n", false},
		{"a plain class", "class Thing {}\n", false},
		// `default` as a switch label is the same word in a different place, and a scan for the
		// keyword rather than the modifier would accept it.
		{"a default switch label", "switch (value) {\n    default:\n        break;\n}\n", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsDefaultExported(firstStatement(t, testCase.sourceText)); got != testCase.want {
				t.Fatalf("want %v, got %v", testCase.want, got)
			}
		})
	}
}

// A nil node and a nil modifier list both answer rather than panic. A panic in a shared package
// takes the whole run down instead of one rule's finding.
func TestSurvivesNilInput(t *testing.T) {
	if IsDefaultExported(nil) {
		t.Fatal("want nil to answer false")
	}
	if HasDefaultModifier(nil) {
		t.Fatal("want a nil modifier list to answer false")
	}
}

// The two spellings must agree, since they exist only because two call shapes did. A divergence
// here would recreate the drift the lift removed.
func TestBothSpellingsAgree(t *testing.T) {
	for _, sourceText := range []string{
		"export default function run() {}\n",
		"export function run() {}\n",
		"function run() {}\n",
	} {
		statement := firstStatement(t, sourceText)
		if IsDefaultExported(statement) != HasDefaultModifier(statement.Modifiers()) {
			t.Fatalf("the two spellings disagree on %q", sourceText)
		}
	}
}
