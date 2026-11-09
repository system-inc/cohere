// Package sort_helper_call is staticcheck's S1032, a call of sort.Sort on a conversion to one of package sort's
// helper slices, `sort.Sort(sort.IntSlice(x))`, which `sort.Ints(x)` says plainer, ported as a check on types and
// syntax that reports through the kit every port of staticcheck shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 simple/s1032/s1032.go, whose logic it keeps and whose
// messages it reports word for word, at the same range, the whole call. The original matches no pattern: it walks the
// body of every function declaration and function literal, nested ones included, for calls of sort.Sort, and judges
// each one's argument by its spelling, a selector that code.SelectorName names sort.IntSlice, sort.Float64Slice or
// sort.StringSlice called on one argument. A function any of whose sort.Sort calls passes anything else sorts a type
// of its own, and the original reports none of that function's calls, so a reader is not told to write sort.Ints
// beside a sort.Sort that has to stay. A call in a function literal is judged once for each function around it, and
// reported once. The port keeps all of that, and gathers its findings across the package, as the original does, in
// one Findings, the rule's state over the package, reported once the dispatcher's walk has covered it.
//
// It differs in one place, adjudicated: the port judges the argument by the type it converts to, not by how the
// conversion is spelled. The original's selector misses a conversion to sort.IntSlice written through a dot import,
// `Sort(IntSlice(x))`, through parentheses, `sort.Sort((sort.IntSlice(x)))` or `sort.Sort((sort.IntSlice)(x))`, or
// through an alias of the helper type, and takes each for a type of the code's own: it reports nothing for that call
// and, holding the function permissible, nothing for any other call in it. The port reports each, so it finds a
// superset of the original's findings and never fewer. The registry runs the port in the original's place, under its
// name, and the original stays only as the oracle: the parity tests beside this file run both over the same packages,
// and every finding only the port makes is recorded in the differences fixture.
package sort_helper_call

import (
	"go/ast"
	"go/types"

	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Listener is what the port hears of each package from the dispatcher: every function declaration and function
// literal, in preorder, each judged and what it finds gathered in the rule's state over the package, and all of it
// reported once the walk has covered the package.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.FuncLit)(nil), (*ast.FuncDecl)(nil)},
	Node: func(context *rule.Context, node ast.Node, _ []ast.Node) {
		rule.State[Findings](context).record(Check(context, node))
	},
	PackageExit: func(context *rule.Context) {
		rule.State[Findings](context).report(context)
	},
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener under the original's
// name, so the rule, its settings and every `//cohere:ignore` naming it read the same once the port takes the
// original's place.
var Analyzer = dispatch.RuleAnalyzer(
	rule.Namespace+"S1032",
	"S1032",
	"Flags sort.Sort called on a conversion to sort.IntSlice, sort.Float64Slice or sort.StringSlice, in place "+
		"of sort.Ints, sort.Float64s or sort.Strings: staticcheck's S1032, ported.",
	&Listener,
)

// helperMessages is the original's message for each of package sort's helper slices, keyed by the type's name.
var helperMessages = map[string]string{
	"IntSlice":     "should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))",
	"Float64Slice": "should use sort.Float64s(...) instead of sort.Sort(sort.Float64Slice(...))",
	"StringSlice":  "should use sort.Strings(...) instead of sort.Sort(sort.StringSlice(...))",
}

// Check judges one function declaration or function literal, and returns the calls of sort.Sort on a helper slice
// that its body holds, nested function literals included, for the caller to record, or none when any call of
// sort.Sort in the body passes anything else. A declaration with no body holds no call.
func Check(context *rule.Context, node ast.Node) []Finding {
	var body *ast.BlockStmt
	switch function := node.(type) {
	case *ast.FuncLit:
		body = function.Body
	case *ast.FuncDecl:
		body = function.Body
	default:
		// Invariant: the dispatcher hands Check only the function literals and declarations Listener lists, as the
		// original's lint.ExhaustiveTypeSwitch holds.
		panic("sort_helper_call: the dispatcher handed over a node neither a function literal nor a declaration")
	}
	if body == nil {
		return nil
	}
	found := []Finding{}
	permissible := false
	ast.Inspect(body, func(inspected ast.Node) bool {
		// The original stops walking at the first permissible call, whose function reports nothing whatever follows.
		if permissible {
			return false
		}
		call, isCall := inspected.(*ast.CallExpr)
		// code.IsCallTo's "sort.Sort", the callee named through its object, so a renamed or dot import, or
		// parentheses around it, still call sort.Sort, and a variable holding it does not.
		if !isCall || staticcheck_code.CallIndex(context, call, "sort.Sort") < 0 {
			return true
		}
		message, isHelper := helperConversion(context, call.Args[knowledge.Arg("sort.Sort.data")])
		if !isHelper {
			permissible = true
			return false
		}
		found = append(found, Finding{call: call, message: message})
		return true
	})
	if permissible {
		return nil
	}
	return found
}

// helperConversion is the original's message for argument when it converts to one of package sort's helper slices,
// and false when it is anything else. The original asks code.SelectorName of the conversion's function, so it knows
// the helper only spelled as a package's selector; the port asks the type the conversion yields, so the same
// conversion through a dot import, parentheses or an alias is the helper too. A value that already has a helper's
// type, a variable or a function's result, is no conversion, as in the original.
func helperConversion(context *rule.Context, argument ast.Expr) (string, bool) {
	conversion, isCall := ast.Unparen(argument).(*ast.CallExpr)
	if !isCall || !context.TypesInfo.Types[conversion.Fun].IsType() {
		return "", false
	}
	named, isNamed := types.Unalias(context.TypesInfo.TypeOf(conversion)).(*types.Named)
	// A named type of the universe, error or comparable, has no package.
	if !isNamed || named.Obj().Pkg() == nil || named.Obj().Pkg().Path() != "sort" {
		return "", false
	}
	message, isHelper := helperMessages[named.Obj().Name()]
	return message, isHelper
}
