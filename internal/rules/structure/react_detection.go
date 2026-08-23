package structure

import (
	"strings"
	"unicode"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// React detection: does this function hold JSX or call a hook?
//
// The answer decides whether a PascalCase function is a component, and every rule about components
// rests on it. It is also where the tailwind port went wrong: JSX element names are plain
// identifiers in typescript-go's AST, and reading them as bindings produced 3,081 spurious findings
// (commit 02c720b). The lesson generalizes past that rule. An exemption the original gets free from
// its AST has to be written down explicitly in ours, because the two ASTs disagree about what a JSX
// name even is.

// IsHookName reports the shape React's own linting recognizes as a hook: "use" followed by an
// uppercase letter. A function named "used" or "user" is not a hook.
func IsHookName(name string) bool {
	if !strings.HasPrefix(name, "use") || len(name) == 3 {
		return false
	}
	return unicode.IsUpper([]rune(name[3:])[0])
}

// IsLikelyComponentName reports a name starting with a capital, which is the whole test the rule
// applies. JSX decides between a component and an intrinsic element by that letter, so the capital
// is the author's claim that this is a component.
func IsLikelyComponentName(name string) bool {
	runes := []rune(name)
	return len(runes) > 0 && unicode.IsUpper(runes[0])
}

// jsxWalkDepthLimit bounds the search, matching the original's limit of 20.
//
// The bound is behavior rather than a safety valve, which is why it is reproduced rather than
// dropped. A deeply nested component whose JSX sits past the limit reads as "not a component" to the
// gate verify replaces, and a Go walk with no limit would find it and report a finding the gate
// does not. Parity is the acceptance criterion, so the limit ports with the rule.
const jsxWalkDepthLimit = 20

// HasJsxOrReactHookCalls reports whether a subtree holds JSX or calls a React hook.
//
// The original walks a fixed list of eleven property names rather than every child, and reproducing
// that bound matters: an exhaustive walk finds JSX in places the original never descends into, and
// every such find is a finding the gate does not have.
//
// Naming the node kinds that carry those properties was the wrong way to reproduce it, and measuring
// is what caught it. The list looked complete and missed if, switch, for and try, because those
// carry their branches on exactly the properties the original does walk: an IfStatement's consequent
// and alternate, a loop's and a try's body. One component out of 128 was lost to that gap, and it
// was the one whose JSX all sat inside if statements.
//
// So this excludes rather than enumerates. Everything is walked except the few places the original's
// property list genuinely cannot reach, which is the same set stated as a boundary instead of as an
// inventory, and it fails toward the original rather than away from it.
func HasJsxOrReactHookCalls(node *ast.Node) bool {
	return searchForJsxOrHook(node, 0)
}

func searchForJsxOrHook(node *ast.Node, depth int) bool {
	if node == nil || depth > jsxWalkDepthLimit {
		return false
	}

	switch node.Kind {
	case ast.KindJsxElement, ast.KindJsxSelfClosingElement, ast.KindJsxFragment:
		return true
	case ast.KindCallExpression:
		if isHookCall(node.AsCallExpression()) {
			return true
		}
	}

	found := false
	node.ForEachChild(func(child *ast.Node) bool {
		if !descendsForJsxSearch(child) {
			return false
		}
		if searchForJsxOrHook(child, depth+1) {
			found = true
		}
		return found
	})
	return found
}

// descendsForJsxSearch reports whether the walk enters a child.
//
// One exclusion, and it earns its place by measurement rather than by sounding prudent.
//
// An earlier version of this also skipped type nodes, parameters, decorators, heritage clauses and
// import declarations, on the reasoning that JSX under a type is not a component body. Every one of
// those was speculative: removing them changed nothing in the fixtures and nothing across 3,400
// files, because the shapes they guard against do not occur. Code that cannot be shown to do
// anything is code that can only diverge from the original later, so it is gone. The original has no
// type-position check at all, and parity is the acceptance criterion.
func descendsForJsxSearch(node *ast.Node) bool {
	if node == nil {
		return false
	}

	switch node.Kind {

	// A switch statement's arms are genuinely unreachable to the original, and this is the one
	// exclusion that is a fact about its list rather than a judgment. ESTree puts the arms on
	// SwitchStatement.cases, and "cases" is not among the eleven names it walks. So a component that
	// returns JSX only from switch arms does not read as a component to the gate.
	//
	// Measured, not assumed: walking into them found two real components the gate does not report,
	// and both are switch-dispatch components. The rule is parity, so the blind spot ports with it.
	// SwitchCase.consequent would be reachable if anything reached the case, and nothing does.
	case ast.KindSwitchStatement:
		return false
	}
	return true
}

// isHookCall reports a direct useFoo(...) or a namespaced React.useFoo(...).
//
// The namespaced form is checked against the React object specifically, matching the original: a
// call to somethingElse.useState is not a React hook, and reading every namespaced use* name as one
// would count things the gate does not.
func isHookCall(call *ast.CallExpression) bool {
	if call == nil || call.Expression == nil {
		return false
	}

	switch call.Expression.Kind {
	case ast.KindIdentifier:
		return IsHookName(call.Expression.Text())

	case ast.KindPropertyAccessExpression:
		access := call.Expression.AsPropertyAccessExpression()
		if access.Expression == nil || access.Expression.Kind != ast.KindIdentifier {
			return false
		}
		if access.Expression.Text() != "React" {
			return false
		}
		name := access.Name()
		return name != nil && IsHookName(name.Text())
	}
	return false
}
