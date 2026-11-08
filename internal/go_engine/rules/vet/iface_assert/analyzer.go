// Package iface_assert is vet's ifaceassert pass, an assertion from one interface to another that can never succeed:
// `value.(Target)`, or a case of a type switch on value, where value's static type is an interface holding a method
// of the same name as one of Target's with a different signature, so no type can implement both. Where either
// interface holds a free type parameter, in a method or in its type set, the pass says nothing, since it cannot know
// the signatures it would compare.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/ifaceassert/ifaceassert.go, whose
// logic it keeps and whose messages it reports word for word, at the same position. The search for a free type
// parameter is x/tools' internal typeparams.Free, and the type set of a constraint it reads is typeparams'
// InterfaceTermSet, both translated here (free_type_parameters.go, term_set.go, term_list.go, term.go), the second
// with its term operations. The search remembers its answers across the whole package, and an answer given for a
// type met inside its own computation stands, so the port walks the assertions and switches in one preorder, as the
// original does, and asks about the same types in the same order. The registry runs the port in the original's
// place, and the original stays the oracle: the parity tests beside this file run both over the same packages. The
// two agree everywhere, so there is no differences fixture.
package iface_assert

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "ifaceassert",
	Doc: "detect impossible interface-to-interface type assertions\n\n" +
		"This checker flags type assertions v.(T) and corresponding type-switch cases\n" +
		"in which the static type V of v is an interface that cannot possibly implement\n" +
		"the target interface T. This occurs when V and T contain methods with the same\n" +
		"name but different signatures. Example:\n\n" +
		"\tvar v interface {\n" +
		"\t\tRead()\n" +
		"\t}\n" +
		"\t_ = v.(io.Reader)\n\n" +
		"The Read method in v has a different signature than the Read method in\n" +
		"io.Reader, so this assertion cannot succeed.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/ifaceassert",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("iface_assert: inspect's result is missing")
	}
	// One search for the whole package, as the original keeps one, since its remembered answers depend on the order
	// it is asked in.
	var free freeTypeParameters
	// One preorder over both kinds of node, as the original's Preorder with its two-type filter, rather than an
	// inspector.All walk per kind, which would ask the search in another order.
	for node := range inspected.PreorderSeq((*ast.TypeAssertExpr)(nil), (*ast.TypeSwitchStmt)(nil)) {
		// assertion is the `value.(Target)` judged, and targets the interfaces it asserts to: its own type, or each
		// case's of a type switch.
		var assertion *ast.TypeAssertExpr
		var targets []ast.Expr
		switch visited := node.(type) {
		case *ast.TypeAssertExpr:
			// A switch's `value.(type)` has no type of its own, and is judged with its switch.
			if visited.Type == nil {
				continue
			}
			assertion = visited
			targets = append(targets, visited.Type)
		case *ast.TypeSwitchStmt:
			assertion = switchedAssertion(visited)
			for _, clause := range visited.Body.List {
				caseClause, isCaseClause := clause.(*ast.CaseClause)
				if !isCaseClause {
					// Invariant: go/parser fills a type switch's body with case clauses alone.
					panic("iface_assert: a type switch whose body holds something other than a case clause")
				}
				targets = append(targets, caseClause.List...)
			}
		}
		asserted := pass.TypesInfo.TypeOf(assertion.X)
		for _, target := range targets {
			targetType := pass.TypesInfo.TypeOf(target)
			if conflicting := assertableTo(&free, asserted, targetType); conflicting != nil {
				pass.Reportf(
					target.Pos(),
					"impossible type assertion: no type can implement both %v and %v (conflicting types for %v method)",
					asserted, targetType, conflicting.Name(),
				)
			}
		}
	}
	return nil, nil
}

// switchedAssertion is the `value.(type)` a type switch switches on, written alone or assigned, `name := value.(type)`.
func switchedAssertion(typeSwitch *ast.TypeSwitchStmt) *ast.TypeAssertExpr {
	switch guard := typeSwitch.Assign.(type) {
	case *ast.ExprStmt:
		if assertion, isAssertion := guard.X.(*ast.TypeAssertExpr); isAssertion {
			return assertion
		}
	case *ast.AssignStmt:
		if assertion, isAssertion := guard.Rhs[0].(*ast.TypeAssertExpr); isAssertion {
			return assertion
		}
	}
	// Invariant: go/parser makes a type switch only of a guard that is one of the two forms above.
	panic("iface_assert: a type switch whose guard is not a type assertion")
}

// assertableTo is nil when asserted can be asserted to target, and otherwise the first method the two declare with
// conflicting types. A pair that are not both interfaces, or where either holds a free type parameter, is nil: the
// first is the type checker's to judge, and the second is a mitigation the original keeps until
// https://github.com/golang/go/issues/50658 is resolved.
func assertableTo(free *freeTypeParameters, asserted types.Type, target types.Type) *types.Func {
	if target == nil || asserted == nil {
		return nil
	}
	assertedInterface, isAssertedInterface := asserted.Underlying().(*types.Interface)
	targetInterface, isTargetInterface := target.Underlying().(*types.Interface)
	if !isAssertedInterface || !isTargetInterface {
		return nil
	}
	if free.has(assertedInterface) || free.has(targetInterface) {
		return nil
	}
	if method, wrongType := types.MissingMethod(assertedInterface, targetInterface, false); wrongType {
		return method
	}
	return nil
}
