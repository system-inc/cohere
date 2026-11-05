// Package correctness_require_checked_assertion is the Go engine's rule that a type assertion is checked: the
// comma-ok form or a type switch, never the single value that panics on a miss.
//
// A single-value assertion, value.(Type), is a claim the compiler cannot check, and when it is wrong the process
// stops with a message about interface conversion rather than a sentence about what went wrong. The comma-ok form
// gives the miss a place for code, so the next reader sees what happens when the value is something else.
package correctness_require_checked_assertion

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-checked-assertion"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_checked_assertion",
	Doc: "Flags a single-value type assertion, value.(Type), which panics when the value is not that type, so every " +
		"assertion is the comma-ok form or a type switch. An assertion to an interface the static type already " +
		"implements is flagged too, since a nil value fails every assertion.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var uncheckedAssertionMessage = policy.MessageOf(Name, "uncheckedAssertion")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			assertion, isAssertion := node.(*ast.TypeAssertExpr)
			// A type switch's value.(type) has no Type and cannot panic: a value no case matches runs the default
			// case or nothing. Its operand is still walked, since it may hold an assertion of its own.
			if !isAssertion || assertion.Type == nil {
				return true
			}
			recorded, isRecorded := pass.TypesInfo.Types[assertion]
			// A package that fails to type-check is never run, so every assertion is recorded. One that somehow is
			// not is left alone, since a finding guessed without types is worse than none.
			if !isRecorded {
				return true
			}
			// go/types records an assertion read as comma-ok, `typed, ok := value.(Type)` in any of its assignment
			// and declaration forms and through any parentheses, as the tuple (Type, bool). That is the one form
			// that cannot panic, so the type decides it rather than the shape of the statement around it.
			if _, isCommaOk := recorded.Type.(*types.Tuple); isCommaOk {
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:      assertion.Pos(),
				End:      assertion.End(),
				Category: uncheckedAssertionMessage.Id,
				Message: uncheckedAssertionMessage.Render(map[string]string{
					// Past the comma-ok check, the recorded type is the assertion's target, spelled as this
					// package's own code would spell it.
					"type": types.TypeString(recorded.Type, types.RelativeTo(pass.Pkg)),
				}),
			})
			return true
		})
	}
	return nil, nil
}
