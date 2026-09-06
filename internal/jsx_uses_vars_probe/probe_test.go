// Package jsx_uses_vars_probe is the runnable evidence that `react/jsx-uses-vars` was DECLINED
// after measurement rather than skipped.
//
// It lives outside `internal/lint/rules/` deliberately: the guards and package test runs scan that
// tree, so a probe inside it turns a package red for every other agent, naming a file they have
// never opened. Here it costs nobody anything.
//
// # Why this exists rather than only a paragraph in the audit
//
// The ruling is written up at `internal/lint/rules/react/jsx_uses_vars.md`. That prose is a claim;
// this is the thing that can fail. Upstream's rule contains zero `context.report` calls and exists
// only to call `markVariableAsUsed` so that `no-unused-vars` does not flag a binding referenced only
// from a JSX tag name. The question that actually decides whether to port it is therefore not "what
// does it report" but "does OUR `no-unused-vars` already get this right", and that is a measurement
// about a different rule, which no fixture in the react package would ever make.
//
// If `no-unused-vars` ever changes such that a JSX-only binding starts being reported, the decline
// stops being correct and this file goes red. That is the whole point of keeping it.
package jsx_uses_vars_probe

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rules/core"
	rule_testing "github.com/system-inc/cohere/internal/lint/testing"
)

// probeFile is where the cases pretend to live.
//
// A `.tsx` name because every case is JSX. Not load-bearing to the measurement; `no-unused-vars`
// has no file gate.
const probeFile = "/repository/source/Probe.tsx"

// cleanCases are upstream's own `valid` list for `jsx-uses-vars`, taken from
// `/tmp/lint-sources-fresh/eslint-plugin-react/tests/lib/rules/jsx-uses-vars.js`.
//
// The `/* eslint react/jsx-uses-vars: 1 */` pragma line is dropped from each, because it is how
// upstream turns the rule on and here there is no rule to turn on -- which is the finding.
//
// Note what upstream runs these against: its tester invokes `no-unused-vars` as the rule under
// test, with `jsx-uses-vars` merely enabled alongside. So does this. Every one must come back clean.
var cleanCases = []struct {
	name   string
	source string
}{
	{"valid-1-var-in-function", "function foo() {\n  var App;\n  var bar = React.render(<App/>);\n  return bar;\n};\nfoo()\n"},
	{"valid-2-module-scope", "var App;\nReact.render(<App/>);\n"},
	{"valid-3-attribute-expression", "var a = 1;\nReact.render(<img src={a} />);\n"},
	{"valid-4-returned-from-function", "var App;\nfunction f() {\n  return <App />;\n}\nf();\n"},
	{"valid-5-member-tag", "var App;\n<App.Hello />\n"},
	{"valid-6-class-declaration", "class HelloMessage {};\n<HelloMessage />\n"},
	{"valid-7-shadowed-inner", "class HelloMessage {\n  render() {\n    var HelloMessage = <div>Hello</div>;\n    return HelloMessage;\n  }\n};\n<HelloMessage />\n"},
	{"valid-8-two-deep-member", "function foo() {\n  var App = { Foo: { Bar: {} } };\n  var bar = React.render(<App.Foo.Bar/>);\n  return bar;\n};\nfoo()\n"},
	{"valid-9-three-deep-member", "function foo() {\n  var App = { Foo: { Bar: { Baz: {} } } };\n  var bar = React.render(<App.Foo.Bar.Baz/>);\n  return bar;\n};\nfoo()\n"},
	{"valid-10-lowercase-object-uppercase-tag", "var object;\nReact.render(<object.Tag />);\n"},
	{"valid-11-lowercase-object-lowercase-tag", "var object;\nReact.render(<object.tag />);\n"},
}

// TestJsxOnlyBindingsAreNotFlagged is the measurement.
//
// Upstream needs `jsx-uses-vars` because `eslint-scope` does not connect a JSX element name to its
// declaration. Ours does: `GetSymbolAtLocation` on a JSX tag name resolves to the declaration. So a
// binding referenced only from JSX is already counted as read here, and a faithful port of that rule
// would report nothing and change nothing.
//
// This test is only half the instrument. On its own a rule that had stopped judging anything at all
// would pass every case below, which is the confident zero this repository keeps finding. See
// TestControlsFire.
func TestJsxOnlyBindingsAreNotFlagged(t *testing.T) {
	t.Parallel()
	for _, testCase := range cleanCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunTyped(t, core.NoUnusedVars, probeFile, testCase.source)
			for _, diagnostic := range result.Diagnostics {
				t.Errorf("expected clean, got %q at %q",
					diagnostic.Message.Id,
					testCase.source[diagnostic.Range.Pos():diagnostic.Range.End()])
			}
		})
	}
}

// controlCases are upstream's `invalid` list: bindings that are genuinely unread even with
// `jsx-uses-vars` enabled.
//
// These are what make the clean column above a measurement rather than a broken instrument. Without
// them, a `no-unused-vars` that reported nothing at all would look like a `no-unused-vars` that
// resolves JSX perfectly.
var controlCases = []struct {
	name     string
	source   string
	expected int
}{
	{"invalid-1-bare-var", "var App;\n", 1},
	{"invalid-2-unused-beside-used", "var App;\nvar unused;\nReact.render(<App unused=\"\"/>);\n", 1},
	{"invalid-4-member-object-only", "var Button;\nvar Input;\nReact.render(<Button.Input unused=\"\"/>);\n", 1},
	{"invalid-5-unused-class", "class unused {}\n", 1},
	{"invalid-6-class-never-rendered", "class HelloMessage {\n  render() {\n    var HelloMessage = <div>Hello</div>;\n    return HelloMessage;\n  }\n}\n", 1},
	{"invalid-8-lowercase-tag-shadow", "var lowercase;\nReact.render(<lowercase />);\n", 1},
	{"invalid-9-parameter-named-div", "function Greetings(div) {\n  return <div />;\n}\nGreetings();\n", 1},
}

// TestControlsFire proves the harness above can report at all.
//
// Measured 2026-09-06: all 7 fire. The pair was also checked the other way, by editing one clean
// case so its binding really is unused; that case went red and went green again when restored.
// Baseline-green, mutant-red, restored-green.
func TestControlsFire(t *testing.T) {
	t.Parallel()
	for _, testCase := range controlCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			result := rule_testing.RunTyped(t, core.NoUnusedVars, probeFile, testCase.source)
			if len(result.Diagnostics) != testCase.expected {
				var got []string
				for _, diagnostic := range result.Diagnostics {
					got = append(got, testCase.source[diagnostic.Range.Pos():diagnostic.Range.End()])
				}
				t.Errorf("CONTROL DID NOT FIRE: want %d findings, got %d (%s). The clean cases in "+
					"TestJsxOnlyBindingsAreNotFlagged prove nothing while this is failing",
					testCase.expected, len(result.Diagnostics), strings.Join(got, ", "))
			}
		})
	}
}
