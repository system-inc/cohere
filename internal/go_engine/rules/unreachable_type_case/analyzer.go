// Package unreachable_type_case is staticcheck's SA4020, a case clause of a type switch no value can reach, since an
// interface an earlier clause names is implemented by every type the clause names, and clauses are tried in the order
// they are written, ported as a check on syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa4020/sa4020.go, whose logic it keeps and
// whose message it reports word for word, over the unreachable clause's case and colon, where staticcheck's report
// puts it with its short range. A type parameter named in a case is never taken as the interface that subsumes a
// later type, as the original asks golang.org/x/exp/typeparams, whose IsTypeParam is that one type assertion, here
// written out. Like the original, it reports a clause once for each earlier clause that subsumes it, naming the first
// pair of types in each, and judges a `nil` in a case list as reachable always. The registry still runs the original,
// which stays the oracle: the parity tests beside this file run both over the same modules, and the two differ
// nowhere they hold.
package unreachable_type_case

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Listener is what the port hears of each package from the dispatcher: every type switch, in preorder.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.TypeSwitchStmt)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		typeSwitch, isTypeSwitch := node.(*ast.TypeSwitchStmt)
		if !isTypeSwitch {
			// Invariant: the dispatcher hands Node only the type switches it was asked for.
			panic("unreachable_type_case: the dispatcher handed over a node that is not a type switch")
		}
		Check(context, typeSwitch)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"SA4020",
	"SA4020",
	"Flags a case clause of a type switch that an earlier clause always matches first: staticcheck's SA4020, "+
		"ported as a check on syntax.",
	&Listener,
)

// typedClause is a case clause of a type switch and the types its case list names, in order, a `nil` left out.
type typedClause struct {
	clause *ast.CaseClause
	types  []types.Type
}

// Check judges one type switch: each case clause after another is reported once for every earlier clause naming an
// interface that a type of its own implements. The default clause names no type and is left out, wherever it stands.
func Check(context *rule.Context, typeSwitch *ast.TypeSwitchStmt) {
	clauses := make([]typedClause, 0, len(typeSwitch.Body.List))
	for _, statement := range typeSwitch.Body.List {
		clause, isClause := statement.(*ast.CaseClause)
		// A type switch's body holds case clauses alone; the original asserts it without a check.
		if !isClause || len(clause.List) == 0 {
			continue
		}
		named := make([]types.Type, 0, len(clause.List))
		for _, expression := range clause.List {
			// A `nil` matches only a nil interface, which no type matches first, so it is always reachable. A clause
			// naming nothing but `nil` still counts as a clause, with no type that subsumes or is subsumed.
			if caseType := context.TypesInfo.TypeOf(expression); caseType != types.Typ[types.UntypedNil] {
				named = append(named, caseType)
			}
		}
		clauses = append(clauses, typedClause{clause: clause, types: named})
	}
	if len(clauses) <= 1 {
		return
	}
	for index, earlier := range clauses[:len(clauses)-1] {
		for _, later := range clauses[index+1:] {
			subsuming, subsumed, isSubsumed := firstSubsumed(earlier.types, later.types)
			if !isSubsumed {
				continue
			}
			staticcheck_report.Report(context, later.clause, fmt.Sprintf(
				"unreachable case clause: %s will always match before %s", subsuming.String(), subsumed.String()),
				staticcheck_report.ShortRange())
		}
	}
}

// firstSubsumed is the first pair, in the order of earlier and then of later, of a type of earlier that subsumes a
// type of later.
func firstSubsumed(earlier []types.Type, later []types.Type) (types.Type, types.Type, bool) {
	for _, subsuming := range earlier {
		for _, subsumed := range later {
			if subsumes(subsuming, subsumed) {
				return subsuming, subsumed, true
			}
		}
	}
	return nil, nil, false
}

// subsumes is whether every value of type subsumed matches subsuming first: subsuming is an interface, and subsumed
// implements it, its method set holding subsuming's. A type parameter is not judged an interface, though its
// underlying type is its constraint, since what it stands for is chosen at instantiation.
func subsumes(subsuming types.Type, subsumed types.Type) bool {
	if _, isTypeParameter := subsuming.(*types.TypeParam); isTypeParameter {
		return false
	}
	subsumingInterface, isInterface := subsuming.Underlying().(*types.Interface)
	if !isInterface {
		return false
	}
	return types.Implements(subsumed, subsumingInterface)
}
