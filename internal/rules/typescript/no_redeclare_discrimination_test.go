package typescript

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// TestPartitionAndKindSeparatesAllTwentyThree runs the exact 23 cases from the earlier identity
// probe through the SHIPPED rule, and ASSERTS rather than logging.
//
// The earlier probe scored "distinct symbol identity" alone and disagreed with upstream on 7 of
// these 23: five false negatives where duplicate var, function and enum declarations share one
// symbol, and two false positives where the same name in two non-overlapping scopes gets two
// symbols. That probe only called t.Logf, so it passed while carrying every one of those
// disagreements, which is the same shape as a measurement that never ran.
//
// The shipped rule does not use symbol identity at all, which is why those 7 are not its problem.
// Grep it: no GetSymbolAtLocation, no symbol comparison, only prose in the doc comment. It groups
// declaration NODES by their enclosing scope and then reads the declaration KINDS in the group. The
// two false positives are answered by the partition, and the five false negatives are answered by
// the kind arithmetic, because a var beside a var is not a member of any merge set.
//
// The `want` column is what the installed @typescript-eslint 8.67.0 rule does, driven case by case
// through the ESLint Linter API with a control firing.
func TestPartitionAndKindSeparatesAllTwentyThree(t *testing.T) {
	cases := []struct {
		label      string
		sourceText string
		wantCount  int
		// note records why the case is here when it is one of the seven identity got wrong.
		note string
	}{
		// Identity AGREED on these. They are the merge family, and they stay correct.
		{"dup class", "class E {}\nclass E {}", 1, ""},
		{"dup type alias", "type G = string;\ntype G = number;", 1, ""},
		{"var and function", "var b = 1;\nfunction b() {}", 1, ""},
		{"var and class", "var c = 1;\nclass c {}", 1, ""},
		{"class and type alias", "class Q {}\ntype Q = string;", 1, ""},
		{"class and enum", "class R {}\nenum R {A}", 1, ""},
		{"interface and type alias", "interface S {}\ntype S = string;", 1, ""},
		{"class twice plus namespace", "class V {}\nclass V {}\nnamespace V {}", 1, ""},
		{"interface merge", "interface I {}\ninterface I {}", 0, ""},
		{"namespace merge", "namespace N {}\nnamespace N {}", 0, ""},
		{"class plus interface", "class C {}\ninterface C {}", 0, ""},
		{"class plus namespace", "class W {}\nnamespace W {}", 0, ""},
		{"function plus namespace", "function D() {}\nnamespace D {}", 0, ""},
		{"enum plus namespace", "enum J {A}\nnamespace J {}", 0, ""},
		{"overloads", "declare function K(a: string): void;\ndeclare function K(a: number): void;", 0, ""},
		{"class interface namespace", "class Z {}\ninterface Z {}\nnamespace Z {}", 0, ""},

		// The five identity got WRONG in the silent direction. Each is a duplicate that shares one
		// symbol, so identity saw one binding and predicted clean while upstream reports. Kind
		// arithmetic answers all five: none of these pairs is drawn from a merge set.
		{"dup enum", "enum H {A}\nenum H {B}", 1, "identity predicted clean; two enums are not a merge set"},
		{"dup function", "function F() {}\nfunction F() {}", 1, "identity predicted clean; two function bodies are not overloads"},
		{"dup var", "var a = 1;\nvar a = 2;", 1, "identity predicted clean; the single most common shape this rule must catch"},
		{"var and type alias", "var d = 1;\ntype d = string;", 1, "identity predicted clean; a type alias merges with nothing"},
		{"let and type alias", "let u = 1;\ntype u = string;", 1, "identity predicted clean; same, through a let"},

		// The two identity got WRONG in the reporting direction. Each is one name in two scopes that
		// do not overlap, so identity saw two bindings and predicted a finding while upstream is
		// silent. The scope partition answers both: two groups of one rather than one group of two.
		{"separate function scopes", "function f1() { var x = 1; }\nfunction f2() { var x = 2; }", 0, "identity predicted a finding; the partition keeps these apart"},
		{"let separate blocks", "{ let y = 1; }\n{ let y = 2; }", 0, "identity predicted a finding; block scopes are distinct containers"},
	}

	disagreements := 0
	for _, testCase := range cases {
		result := ruletest.RunTypedWithOptions(t, NoRedeclare, redeclareFile, testCase.sourceText, nil)
		got := len(result.Diagnostics)
		if got != testCase.wantCount {
			disagreements++
			t.Errorf("%-26s got %d findings, upstream reports %d  %s", testCase.label, got, testCase.wantCount, testCase.note)
		}
	}
	if disagreements != 0 {
		t.Errorf("partition-and-kind disagrees with upstream on %d of %d cases", disagreements, len(cases))
	}
	t.Logf("partition-and-kind: %d disagreements over %d cases (identity alone had 7)", disagreements, len(cases))
}
