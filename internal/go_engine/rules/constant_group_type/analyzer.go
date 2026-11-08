// Package constant_group_type is staticcheck's SA9004, a group of constants in which only the first has an explicit
// type, where each later one given only a literal takes the literal's default type and not the first's, ported as a
// check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa9004/sa9004.go, whose logic it keeps and
// whose message it reports word for word, at the group's first spec, through staticcheck_report.Report, the port of the
// report.Report the original calls, proven against it beside it. Groups are the runs of specs on consecutive lines, as
// staticcheck's go/ast/astutil.GroupSpecs splits them, a public utility the port calls itself. Its suggested fix gives
// each later spec the first's type, each spec printed again without its trailing comment, which stays where it is; it
// is built with staticcheck's analysis/edit, edit.Fix and edit.ReplaceWithNode, borrowed in this wave as report.Render
// is, and passed through staticcheck_report.Fixes; wave 5 ports edit. The group is judged by types and not by values,
// so a literal whose type converts to the first's but whose value does not fit it, `large = 1000` after an int8 or
// `signed = -1` after a uint, is reported all the same, and the fix writes a declaration that does not compile, as the
// original's does; testdata/src/differences holds both. The original keeps no state from one declaration
// to the next. The registry still runs the original, which stays the oracle: the parity tests beside this file run
// both over the same packages, fixes included, and the two differ nowhere they hold.
package constant_group_type

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/edit"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/ast/astutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// generated pass, which the context's judgment of generated files reads, though this check filters none.
var Analyzer = &analysis.Analyzer{
	Name: "SA9004",
	Doc: "Flags a group of constants in which only the first has an explicit type, so the others take their " +
		"literals' default types: staticcheck's SA9004, ported as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, generated.Analyzer},
	Run:      run,
}

// run builds the pass's context and judges every general declaration of the package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("constant_group_type: the inspect pass's result is missing")
	}
	inspected.Preorder([]ast.Node{(*ast.GenDecl)(nil)}, func(node ast.Node) {
		declaration, isDeclaration := node.(*ast.GenDecl)
		if !isDeclaration {
			// Invariant: Preorder hands this function only the general declarations it was asked for.
			panic("constant_group_type: the inspector handed over a node that is not a general declaration")
		}
		Check(context, declaration)
	})
	return nil, nil
}

// Check judges one declaration: in a parenthesized constant declaration, each group of two or more specs on
// consecutive lines whose first alone has a type, each spec naming one constant given one literal its type could
// convert to, is reported, with the fix that gives every later spec the first's type.
func Check(context *rule.Context, declaration *ast.GenDecl) {
	if !declaration.Lparen.IsValid() || declaration.Tok != token.CONST {
		return
	}
	for _, group := range astutil.GroupSpecs(context.Fset, declaration.Specs) {
		specs := valueSpecs(group)
		if len(specs) < 2 || specs[0].Type == nil || !onlyFirstTyped(context, specs) {
			continue
		}
		edits := []analysis.TextEdit{}
		for _, spec := range specs[1:] {
			typed := *spec
			typed.Type = specs[0].Type
			// A spec's range leaves out its trailing comment and its printing would not, so the comment is left out of
			// the printing and stays where it is, said once.
			typed.Comment = nil
			edits = append(edits, edit.ReplaceWithNode(context.Fset, spec, &typed))
		}
		staticcheck_report.Report(context, specs[0], "only the first constant in this group has an explicit type",
			staticcheck_report.Fixes(edit.Fix("Add type to all constants in group", edits...)))
	}
}

// valueSpecs is group's specs as the value specs a constant declaration holds.
func valueSpecs(group []ast.Spec) []*ast.ValueSpec {
	specs := []*ast.ValueSpec{}
	for _, spec := range group {
		valueSpec, isValueSpec := spec.(*ast.ValueSpec)
		if !isValueSpec {
			// Invariant: the parser puts only value specs in a constant declaration.
			panic("constant_group_type: a constant declaration holds a spec that is not a value spec")
		}
		specs = append(specs, valueSpec)
	}
	return specs
}

// onlyFirstTyped is whether every spec of a group whose first is typed names one constant given one value, a literal
// or a unary operator on one, that converts to the first's type, and no spec after the first has a type of its own. A
// value that is not a literal may carry a type of its own, as `1000 * Nanosecond` does, so it leaves the group alone.
func onlyFirstTyped(context *rule.Context, specs []*ast.ValueSpec) bool {
	firstType := context.TypesInfo.TypeOf(specs[0].Values[0])
	for index, spec := range specs {
		if index > 0 && spec.Type != nil {
			return false
		}
		if len(spec.Names) != 1 || len(spec.Values) != 1 {
			return false
		}
		if !types.ConvertibleTo(context.TypesInfo.TypeOf(spec.Values[0]), firstType) {
			return false
		}
		switch value := spec.Values[0].(type) {
		case *ast.BasicLit:
		case *ast.UnaryExpr:
			if _, isLiteral := value.X.(*ast.BasicLit); !isLiteral {
				return false
			}
		default:
			return false
		}
	}
	return true
}
