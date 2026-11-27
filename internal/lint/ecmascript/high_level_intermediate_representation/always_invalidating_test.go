package high_level_intermediate_representation

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// TestIsAlwaysInvalidatingTypeSeparatesTheFourShapes is the whole claim of this predicate.
//
// Upstream answers from a shape registry it maintains because Babel has no checker. This asks the
// resident checker instead, and the only thing that makes that substitution legitimate is that the
// checker separates the same set. So the set is asserted directly, by name, in both directions --
// every invalidating shape true and every cheap-to-compare one false.
//
// A test that only checked the true cases would pass on a predicate that returns true for
// everything, which is the failure this file is most likely to have.
func TestIsAlwaysInvalidatingTypeSeparatesTheFourShapes(test *testing.T) {
	test.Parallel()

	// The callable forms are here because the predicate is one flag check rather than upstream's
	// two arms. A separate call-signature arm was written, then removed when a mutation sweep
	// showed deleting it changed no answer: every callable form TypeScript has carries the Object
	// flag. These five hold that reduction in place -- if a callable form is ever found without the
	// flag, one of them fails and the arm comes back.
	answers := invalidatingAnswers(test, `
		interface CallableValue { (x: number): number; }
		type AliasedFunction = (x: number) => number;
		function f(flag, aliasedValue: AliasedFunction, callableValue: CallableValue,
				overloadedValue: {(): void; (x: number): void}) {
			const arrayValue = [1, 2, 3];
			const objectValue = {a: 1};
			const arrowValue = () => 1;
			function declaredValue() { return 1; }
			const numberValue = 42;
			const stringValue = "s";
			const booleanValue = true;
			const nullValue = null;
			return [arrayValue, objectValue, arrowValue, declaredValue, numberValue,
				stringValue, booleanValue, nullValue, flag, aliasedValue, callableValue,
				overloadedValue];
		}
	`)

	for _, testCase := range []struct {
		name string
		want bool
		why  string
	}{
		{name: "arrayValue", want: true, why: "an array literal allocates on every evaluation"},
		{name: "objectValue", want: true, why: "an object literal allocates on every evaluation"},
		{name: "arrowValue", want: true, why: "a closure allocates on every evaluation"},
		{name: "declaredValue", want: true, why: "a function declaration is still a function type"},
		{name: "aliasedValue", want: true, why: "a type-alias callback is callable and allocates"},
		{name: "callableValue", want: true, why: "a callable interface carries the Object flag"},
		{name: "overloadedValue", want: true, why: "an overloaded signature is still one object type"},
		{name: "numberValue", want: false, why: "a number is comparable with Object.is"},
		{name: "stringValue", want: false, why: "a string is comparable with Object.is"},
		{name: "booleanValue", want: false, why: "a boolean is comparable with Object.is"},
		{name: "nullValue", want: false, why: "null is comparable with Object.is"},
	} {
		got, found := answers[testCase.name]
		if !found {
			test.Errorf("%q never reached the predicate; the lowering did not give it an identifier "+
				"with a node, so this case asserts nothing", testCase.name)
			continue
		}
		if got != testCase.want {
			test.Errorf("%s: got %t, want %t (%s)", testCase.name, got, testCase.want, testCase.why)
		}
	}
}

// TestIsAlwaysInvalidatingTypeDeclinesWithoutAChecker pins the conservative direction.
//
// Both upstream call sites use a true answer to permit a merge, so a missing type must answer false
// rather than true: declining to merge on missing information is safe, merging on it is not. A
// predicate that defaulted the other way would still pass the separation test above.
func TestIsAlwaysInvalidatingTypeDeclinesWithoutAChecker(test *testing.T) {
	test.Parallel()

	function, _ := rangesFor(test, `function f() { const a = [1]; return a; }`)
	if function == nil {
		test.Fatal("the source did not lower")
	}

	reached := 0
	for _, identifier := range function.Identifiers {
		if identifier == nil {
			continue
		}
		reached++
		if IsAlwaysInvalidatingType(function, identifier.Id, nil) {
			test.Errorf("identifier %q answered true with a nil checker; a missing type must decline",
				identifier.Name)
		}
	}
	if reached == 0 {
		test.Fatal("the function holds no identifiers, so the loop asserted nothing")
	}

	if IsAlwaysInvalidatingType(nil, 0, nil) {
		test.Error("a nil function answered true")
	}
}

// invalidatingAnswers runs the predicate over a source and returns its answer per named identifier.
func invalidatingAnswers(test *testing.T, source string) map[string]bool {
	test.Helper()

	answers := map[string]bool{}
	probe := rule.Rule[rule.NoOptions]{
		Name:             "always-invalidating-harness",
		NeedsTypeChecker: true,
		Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
			return rule.Listeners{
				ast.KindSourceFile: func(node *ast.Node) {
					if ruleContext.TypeChecker == nil {
						test.Fatal("the typed harness handed this probe a nil checker, so every " +
							"answer below would be the nil-checker default rather than a type")
					}
					forEachFunctionLike(node, func(functionNode *ast.Node) {
						function := Lower(functionNode, ruleContext.TypeChecker)
						if function == nil {
							return
						}
						Construct(function)
						for _, identifier := range function.Identifiers {
							if identifier == nil || identifier.Name == "" {
								continue
							}
							answers[identifier.Name] = IsAlwaysInvalidatingType(
								function, identifier.Id, ruleContext.TypeChecker)
						}
					})
				},
			}
		},
	}
	rule_testing.RunTypedFiles(test, probe, map[string]string{"/invalidating.ts": source}, "/invalidating.ts")
	return answers
}
