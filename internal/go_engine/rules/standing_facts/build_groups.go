package standing_facts

import (
	"cmp"
	"go/ast"
	"go/types"
	"slices"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/call_groups"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
)

// buildOrderReason is why a member of a cycle of calls in the build's own syntax was named: its answers in the build
// may hang on the order the build's walk enters the cycle in.
const buildOrderReason fact_reach.Reason = "build order"

// buildOrderNamed names each function of the pass's package that is a member of a group in the build, a cycle of
// calls among the package's functions, that touches a body the build lints: one with no standing body of the host's,
// a body only the build declares among them, or one not in standing, the host's set as the walk starts (#5vbs1np,
// must 3). Every member the host's identities name is named, in the order of their keys, and a member only the build
// declares has no user on the host to take out.
//
// The host's order bound (fact_reach.Host.OrderBound) names what hangs on the order of the host's own walks, and only
// what its walks saw: a cycle that forms in the build alone, as a function each platform declares calling back into
// its caller on one platform, is never in it, though a walk of the build's that enters the cycle at another member
// answers another member's question otherwise than the host did. A cycle whose members all stand has the same bodies
// in both builds, so it is the host's too, and its bound already covers it. A cycle of the host's the build does not
// form is order-free in the build, and the host's answers about it either the bound named or are order-free too, so
// comparing them is sound. So the groups to add are the build's that touch a body it lints.
//
// The build's walks run after this pass, so their callees cannot be read; its groups are read from its syntax
// instead: each function declared with a body, to every function of the package with a body that its body names or
// selects, an instantiation's or a method value's taken to its declaration, and a call inside a function literal to
// the declaration around it. Every static call a walk can make, through an instantiation or a method wrapper too,
// names its callee so, and a function only named, never called, is an edge too many, which can only name more.
func buildOrderNamed(
	pass *analysis.Pass,
	host fact_reach.Host,
	standing map[fact_reach.Place]bool,
	functions *functionFinder,
) []fact_reach.Named {
	// The place of each function the host declares with a body, by its key, and the key of each the build finds its own
	// of by the host's identity.
	places := map[fact_reach.Object]fact_reach.Place{}
	for _, place := range host.Places() {
		for _, key := range host.Declared(place) {
			places[key] = place
		}
	}
	keys := map[*types.Func]fact_reach.Object{}
	for _, function := range host.Functions() {
		if _, isDeclared := places[function.Object]; !isDeclared {
			continue
		}
		if own, found := functions.function(function.Identity); found {
			keys[own] = function.Object
		}
	}
	var declarations []*ast.FuncDecl
	bodied := map[*types.Func]bool{}
	for _, file := range pass.Files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil {
				if declared, isDeclared := pass.TypesInfo.Defs[function.Name].(*types.Func); isDeclared {
					declarations = append(declarations, function)
					bodied[declared] = true
				}
			}
		}
	}
	callees := map[*types.Func][]*types.Func{}
	for _, declaration := range declarations {
		declared, _ := pass.TypesInfo.Defs[declaration.Name].(*types.Func)
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			if name, isName := node.(*ast.Ident); isName {
				if used, isFunction := pass.TypesInfo.Uses[name].(*types.Func); isFunction && bodied[used.Origin()] {
					callees[declared] = append(callees[declared], used.Origin())
				}
			}
			return true
		})
	}
	var named []fact_reach.Named
	for _, group := range call_groups.Of(callees) {
		touches := slices.ContainsFunc(group, func(member *types.Func) bool {
			key, isKeyed := keys[member]
			return !isKeyed || !standing[places[key]]
		})
		if !touches {
			continue
		}
		for _, member := range group {
			if key, isKeyed := keys[member]; isKeyed {
				named = append(named, fact_reach.Named{Object: key, Reason: buildOrderReason})
			}
		}
	}
	slices.SortFunc(named, func(first fact_reach.Named, second fact_reach.Named) int {
		return cmp.Compare(first.Object, second.Object)
	})
	return named
}
