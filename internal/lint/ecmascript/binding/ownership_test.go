package binding_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/binding"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// answersFor drives IsForeignName over every occurrence of `target` in a source and returns the
// answers in source order, so every assertion runs against real parsed nodes.
func answersFor(test *testing.T, source string, target string) []bool {
	test.Helper()
	var answers []bool
	rule_testing.Run(test, rule.Rule{
		Name: "foreign-probe",
		Run: func(ruleContext rule.Context, options any) rule.Listeners {
			return rule.Listeners{
				ast.KindIdentifier: func(node *ast.Node) {
					if node.Text() == target {
						answers = append(answers, binding.IsForeignName(node))
					}
				},
			}
		},
	}, "p.tsx", source)
	return answers
}

func expectFirst(test *testing.T, source string, want bool) {
	test.Helper()
	got := answersFor(test, source, "name")
	if len(got) == 0 {
		test.Fatalf("%q: no occurrence of `name` reached the probe", source)
	}
	if got[0] != want {
		test.Errorf("%q: IsForeignName = %v, want %v", source, got[0], want)
	}
}

// TestForeignPositions covers every position a name can occupy that this file did not choose.
func TestForeignPositions(test *testing.T) {
	test.Parallel()
	for _, source := range []string{
		"declare const thing: any; thing.name;",
		"({ name: 1 });",
		"import { name } from './m';",
		"import * as name from './m';",
		"import name from './m';",
		"interface I { name: string }",
	} {
		expectFirst(test, source, true)
	}

	// The two type positions declare the name first, so the USE is the occurrence under test. The
	// declaration is this file's own and correctly answers owned, which is what the index picks
	// apart here rather than asserting on whichever occurrence came first.
	for _, testCase := range []struct {
		source string
		use    int
	}{
		{"declare namespace A { type name = string }\ndeclare const v: A.name;", 1},
		{"type name = string;\ndeclare const v: name;", 1},
	} {
		got := answersFor(test, testCase.source, "name")
		if len(got) <= testCase.use {
			test.Fatalf("%q: expected at least %d occurrences, got %d", testCase.source, testCase.use+1, len(got))
		}
		if got[0] {
			test.Errorf("%q: the declaration answered foreign, want owned", testCase.source)
		}
		if !got[testCase.use] {
			test.Errorf("%q: the use answered owned, want foreign", testCase.source)
		}
	}
}

// TestOwnedPositions covers the positions this file does choose, which must stay judged.
func TestOwnedPositions(test *testing.T) {
	test.Parallel()
	for _, source := range []string{
		"const name = 1;",
		"declare function f(name: string): void;",
		"declare const other: any; name.other;",
		"declare const name: any; f(name);",
		// A shorthand value is a real reference to a binding, not a key.
		"declare const name: any; ({ name });",
	} {
		expectFirst(test, source, false)
	}
}

// TestPropertyReadOffThisIsOwned is the one place the two lifted implementations genuinely
// disagreed, and it is the reason this function exists rather than either of them.
//
// The property belongs to the class being read, which this file owns, and its declaration is a
// property declaration that no member check protects. A class once declared `maximumBackoff` while
// reading `this.maximumBackoff`, which is the measured defect this answer prevents.
func TestPropertyReadOffThisIsOwned(test *testing.T) {
	test.Parallel()
	got := answersFor(test, "class C { name = 1; m() { return this.name; } }", "name")
	if len(got) != 2 {
		test.Fatalf("expected the declaration and the read, got %d occurrences", len(got))
	}
	if got[1] {
		test.Error("a property read off `this` answered foreign; the class owns that property")
	}
	// A read off anything else is foreign, which is what makes the `this` carve-out narrow.
	other := answersFor(test, "declare const thing: any; thing.name;", "name")
	if len(other) != 1 || !other[0] {
		test.Errorf("a property read off another object answered %v, want [true]", other)
	}
}

// TestJsxNamesAreForeign covers the exemption the ESTree originals never needed.
//
// Their parser gives a JSX name its own node type so it never reaches an identifier visitor. Here a
// tag and an attribute are plain identifiers, and reproducing only the written-down exemptions cost
// 3,081 false findings on a real tree.
func TestJsxNamesAreForeign(test *testing.T) {
	test.Parallel()
	for _, source := range []string{
		"const x = <name />;",
		"const x = <name></name>;",
		"declare const Thing: any; const x = <Thing name=\"v\" />;",
	} {
		got := answersFor(test, source, "name")
		if len(got) == 0 {
			test.Fatalf("%q: no occurrence reached the probe", source)
		}
		for index, answer := range got {
			if !answer {
				test.Errorf("%q: occurrence %d answered owned, want foreign", source, index)
			}
		}
	}
}

// TestNilIsNotForeign covers the guard a shared function needs and a rule-local one did not.
func TestNilIsNotForeign(test *testing.T) {
	test.Parallel()
	if binding.IsForeignName(nil) {
		test.Error("IsForeignName(nil) = true, want false")
	}
}
