package nexus

import (
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const arrowFile = "/repository/source/Thing.tsx"

func TestConsistencyNoMultilineArrowFunctionFires(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantIds    []string
	}{
		{
			"multi-line arrow with a block body",
			"const compute = (input) => {\n    return input * 2;\n};\n",
			[]string{"multilineArrow"},
		},
		{
			"react hook",
			"React.useEffect(() => {\n    run();\n}, []);\n",
			[]string{"reactHookArrow"},
		},
		{
			"react forwardRef",
			"const Thing = React.forwardRef((properties, reference) => {\n    return null;\n});\n",
			[]string{"reactHookArrow"},
		},
		{
			"addEventListener",
			"element.addEventListener('click', (event) => {\n    run(event);\n});\n",
			[]string{"addEventListenerArrow"},
		},
		{
			// A nested function binds its own `this`, so it says nothing about the outer arrow and
			// the outer arrow is still reportable.
			"this inside a nested function does not exempt the outer arrow",
			"const compute = () => {\n    return function () {\n        return this.value;\n    };\n};\n",
			[]string{"multilineArrow"},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ConsistencyNoMultilineArrowFunction, arrowFile, testCase.sourceText)
			rule_testing.ExpectFindings(t, result, testCase.wantIds...)
		})
	}
}

func TestConsistencyNoMultilineArrowFunctionStaysSilent(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
	}{
		{"single-line implicit return", "const add = (a, b) => a + b;\n"},
		// The case that separates "has a block body" from "spans lines". An arrow has no prototype
		// and a function expression does, so converting this one changes what the value can do.
		{"single-line block body", "const noop = () => {};\n"},
		{"single-line block with a statement", "const run = () => { start(); };\n"},
		// A function declaration would rebind `this`, so the conversion would change meaning.
		{"arrow using this", "const compute = () => {\n    return this.value;\n};\n"},
		// A nested arrow inherits the outer `this`, so a `this` inside it is the outer arrow's.
		{"this inside a nested arrow", "const compute = () => {\n    return items.map(() => this.value);\n};\n"},
		{"a function declaration is already the target shape", "function compute(input) {\n    return input * 2;\n}\n"},
		{"a function expression", "const compute = function (input) {\n    return input * 2;\n};\n"},
		{"multi-line implicit return is still an implicit return", "const compute = (input) =>\n    input * 2;\n"},

		// An arrow indented inside a multi-line literal. Pos() sits before the leading newline and
		// indentation, so measuring the span from it puts the start on the previous line and this
		// genuinely single-line arrow reads as multi-line. This exact shape was the rule's one
		// false finding on the ahra tree, in a fixture list of non-constructible values where
		// converting it to a function expression would have given it a prototype and flipped the
		// assertion it exists to make.
		{"single-line arrow inside a multi-line array", "const values = [\n    SimpleClass,\n    () => {},\n    42,\n];\n"},
		{"single-line arrow as a multi-line call argument", "register(\n    'name',\n    () => {},\n);\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ConsistencyNoMultilineArrowFunction, arrowFile, testCase.sourceText)
			rule_testing.ExpectClean(t, result)
		})
	}
}

// One arrow must never produce two findings. The general case has to skip what the call-expression
// case already claimed, and a fixture is the only thing that keeps that true.
func TestConsistencyNoMultilineArrowFunctionReportsEachArrowOnce(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		sourceText string
		wantId     string
	}{
		{"react hook", "React.useEffect(() => {\n    run();\n}, []);\n", "reactHookArrow"},
		{"addEventListener", "element.addEventListener('click', (event) => {\n    run(event);\n});\n", "addEventListenerArrow"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := rule_testing.Run(t, ConsistencyNoMultilineArrowFunction, arrowFile, testCase.sourceText)
			rule_testing.ExpectFindings(t, result, testCase.wantId)
		})
	}
}

// No fix is proposed. The TypeScript original rewrites the arrow to a function declaration, and that
// rewrite has already produced three separate parse bugs upstream: an arrow inside a parameter type
// truncating the signature, a nested generic default confusing the type-parameter boundary, and the
// prototype difference above. The conversion is worth doing and is not worth doing unattended.
func TestConsistencyNoMultilineArrowFunctionProposesNoFix(t *testing.T) {
	t.Parallel()

	result := rule_testing.Run(t, ConsistencyNoMultilineArrowFunction, arrowFile,
		"const compute = (input) => {\n    return input * 2;\n};\n")
	rule_testing.ExpectFindings(t, result, "multilineArrow")
	if len(result.Diagnostics[0].Fixes) != 0 {
		t.Fatalf("expected no fixes, got %d", len(result.Diagnostics[0].Fixes))
	}
}
