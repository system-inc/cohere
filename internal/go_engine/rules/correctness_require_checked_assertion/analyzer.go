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

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/correctness-require-checked-assertion"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "correctness_require_checked_assertion",
	Doc: "Flags a single-value type assertion, value.(Type), which panics when the value is not that type, so every " +
		"assertion is the comma-ok form or a type switch. An assertion to an interface the static type already " +
		"implements is flagged too, since a nil value fails every assertion. An assertion of a variable inside the " +
		"single-type case of a type switch on that variable cannot miss, and gets its own message asking for the " +
		"switch's bound value instead; an assignment to the variable between the switch and the assertion is not " +
		"looked for.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: true, Analyzer: Analyzer}

var uncheckedAssertionMessage = policy.MessageOf(Name, "uncheckedAssertion")

var assertionInMatchingCaseMessage = policy.MessageOf(Name, "assertionInMatchingCase")

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		// The nodes from the file down to the one being visited, so an assertion can find the type switch around
		// it. ast.Inspect calls back with nil as it leaves a node it walked into, which is where that node is
		// popped; the callback always returns true, so every push has its pop.
		var ancestors []ast.Node
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				ancestors = ancestors[:len(ancestors)-1]
				return true
			}
			ancestors = append(ancestors, node)
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
			// Past the comma-ok check, the recorded type is the assertion's target, spelled as this package's own
			// code would spell it.
			target := types.TypeString(recorded.Type, types.RelativeTo(pass.Pkg))
			// The ancestors end with the assertion itself, so its parents are everything before it.
			if operand := matchedOperand(pass, assertion, recorded.Type, ancestors[:len(ancestors)-1]); operand != nil {
				pass.Report(analysis.Diagnostic{
					Pos:      assertion.Pos(),
					End:      assertion.End(),
					Category: assertionInMatchingCaseMessage.Id,
					Message: assertionInMatchingCaseMessage.Render(map[string]string{
						"type":  target,
						"value": operand.Name,
					}),
				})
				return true
			}
			pass.Report(analysis.Diagnostic{
				Pos:      assertion.Pos(),
				End:      assertion.End(),
				Category: uncheckedAssertionMessage.Id,
				Message:  uncheckedAssertionMessage.Render(map[string]string{"type": target}),
			})
			return true
		})
	}
	return nil, nil
}

// matchedOperand returns the assertion's operand when a type switch around the assertion already matched it:
// the operand is an identifier naming the same variable as the switch's subject, the assertion sits in a case
// clause of that switch listing exactly one type, and that type is identical to the assertion's target. Such an
// assertion cannot miss, so it is told to use the switch's bound value rather than that it panics. Anything else
// returns nil and gets the ordinary message.
func matchedOperand(
	pass *analysis.Pass,
	assertion *ast.TypeAssertExpr,
	target types.Type,
	parents []ast.Node,
) *ast.Ident {
	operand, isIdentifier := ast.Unparen(assertion.X).(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	operandVariable, isVariable := pass.TypesInfo.Uses[operand].(*types.Var)
	if !isVariable {
		return nil
	}
	for index := len(parents) - 1; index >= 0; index-- {
		switch parent := parents[index].(type) {
		case *ast.FuncLit:
			// A closure can run after the switch has moved on, so a case around the closure says nothing about
			// the value when the closure reads it.
			return nil
		case *ast.CaseClause:
			// A case clause sits in its switch's body, which sits in the switch, so the switch is two up. A clause
			// listing several types, or none (default), binds the subject's own type and matches no one target.
			if index < 2 || len(parent.List) != 1 {
				continue
			}
			typeSwitch, isTypeSwitch := parents[index-2].(*ast.TypeSwitchStmt)
			if !isTypeSwitch || typeSwitchSubject(pass, typeSwitch) != operandVariable {
				continue
			}
			caseType := pass.TypesInfo.TypeOf(parent.List[0])
			if caseType != nil && types.Identical(caseType, target) {
				return operand
			}
		}
	}
	return nil
}

// typeSwitchSubject returns the variable a type switch switches on, `value` in `switch value.(type)` and in
// `switch typed := value.(type)`, or nil when the subject is anything but an identifier naming a variable.
func typeSwitchSubject(pass *analysis.Pass, typeSwitch *ast.TypeSwitchStmt) *types.Var {
	var guard ast.Expr
	switch statement := typeSwitch.Assign.(type) {
	case *ast.ExprStmt:
		guard = statement.X
	case *ast.AssignStmt:
		// A bound switch's guard is `typed := value.(type)`, which go/types accepts only with one name and one
		// value, so a well-typed package has exactly one right-hand side.
		if len(statement.Rhs) == 1 {
			guard = statement.Rhs[0]
		}
	}
	typeGuard, isTypeGuard := ast.Unparen(guard).(*ast.TypeAssertExpr)
	if !isTypeGuard {
		return nil
	}
	subject, isIdentifier := ast.Unparen(typeGuard.X).(*ast.Ident)
	if !isIdentifier {
		return nil
	}
	subjectVariable, _ := pass.TypesInfo.Uses[subject].(*types.Var)
	return subjectVariable
}
