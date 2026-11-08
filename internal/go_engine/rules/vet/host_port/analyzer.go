// Package host_port is vet's hostport pass, an address built for net.Dial by fmt.Sprintf with the format "%s:%d" or
// "%s:%s", which joins a host and a port with a bare colon and so is wrong for an IPv6 host, whose own colons must be
// bracketed. It judges the address argument of each call to net.Dial, net.DialTimeout and (*net.Dialer).Dial: the
// Sprintf call itself, written as the argument, or the sole value that declares the variable passed there. Each
// finding offers the fix the original offers, the same edits to the same spans: net.JoinHostPort in Sprintf's place,
// the format deleted, and a numeric port turned into a string.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/hostport/hostport.go, whose logic
// it keeps and whose messages and fixes it reports word for word, at the same position. The registry runs the port in
// the original's place, and the original stays the oracle: the parity tests beside this file run both over the same
// packages. The two agree everywhere, so there is no differences fixture.
//
// The original finds the dial calls, and the declaration of a variable passed to one, through x/tools' internal type
// index, which cohere cannot import. The port sits on the engine's syntax dispatcher instead, hearing each call,
// assignment and value spec on the walk the dispatcher makes once for every syntax rule, and gathers both over the
// package: each call whose callee typeutil.Callee names, gathered per callee and judged once the package is walked, in
// the original's order of callees, and the declarations, read for the package's variables only once a variable is
// passed. Within one callee the port judges calls in the preorder of
// the calls, and the original in the preorder of the callee's name, which differ only for a dial call nested inside
// another's receiver; that changes the order of the reports and nothing reported.
package host_port

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "hostport"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = `check format of addresses passed to net.Dial

This analyzer flags code that produce network address strings using
fmt.Sprintf, as in this example:

    addr := fmt.Sprintf("%s:%d", host, 12345) // "will not work with IPv6"
    ...
    conn, err := net.Dial("tcp", addr)       // "when passed to dial here"

The analyzer suggests a fix to use the correct approach, a call to
net.JoinHostPort:

    addr := net.JoinHostPort(host, "12345")
    ...
    conn, err := net.Dial("tcp", addr)

A similar diagnostic and fix are produced for a format string of "%s:%s".
`

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/hostport"

// Listener is what the port hears of each package from the dispatcher: every call, assignment and value spec, in
// preorder, gathered as it is heard, and the package judged once it has all been heard.
var Listener = rule.Listener{
	Nodes:       []ast.Node{(*ast.CallExpr)(nil), (*ast.AssignStmt)(nil), (*ast.ValueSpec)(nil)},
	Node:        gatherNode,
	PackageExit: judgePackage,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// gathered is what the port gathers over one package before judging it: whether the package is judged at all, read
// once on the first node heard, the dial calls, one list per callee, and the assignments and value specs that may
// declare a variable passed as an address, each list in the preorder the walk heard it.
type gathered struct {
	decided        bool
	judged         bool
	dials          []*ast.CallExpr
	timeoutDials   []*ast.CallExpr
	dialerDials    []*ast.CallExpr
	assignments    []*ast.AssignStmt
	specifications []*ast.ValueSpec
}

// gatherNode gathers node, a call, an assignment or a value spec, into the package's state, once the package is known
// to be judged.
func gatherNode(context *rule.Context, node ast.Node, _ []ast.Node) {
	state := rule.State[gathered](context)
	if !state.decided {
		state.decided, state.judged = true, isJudged(context.Package)
	}
	if !state.judged {
		return
	}
	switch heard := node.(type) {
	case *ast.CallExpr:
		callee := typeutil.Callee(context.TypesInfo, heard)
		switch {
		case isFunctionNamed(callee, "net", "Dial"):
			state.dials = append(state.dials, heard)
		case isFunctionNamed(callee, "net", "DialTimeout"):
			state.timeoutDials = append(state.timeoutDials, heard)
		case isMethodNamed(callee, "net", "Dialer", "Dial"):
			state.dialerDials = append(state.dialerDials, heard)
		}
	case *ast.AssignStmt:
		state.assignments = append(state.assignments, heard)
	case *ast.ValueSpec:
		state.specifications = append(state.specifications, heard)
	}
}

// isJudged reports whether the original would judge the package at all.
func isJudged(judgedPackage *types.Package) bool {
	// The original's index holds the objects of the packages a package refers to, never the package's own, so it
	// never finds net's dial functions inside net itself, nor Sprintf inside fmt, and judges neither package. fmt
	// imports no fmt, so the fast path below already skips it; net is skipped here. The fixtures cannot reach this
	// branch, since a fixture laid out as net is shadowed by the standard library's.
	if judgedPackage.Path() == "net" {
		return false
	}
	// The original's fast path: no call can name fmt.Sprintf in a package that does not import fmt.
	return slices.ContainsFunc(judgedPackage.Imports(), func(imported *types.Package) bool {
		return imported.Path() == "fmt"
	})
}

// judgePackage judges the dial calls gathered over the package, the calls of each callee in preorder, in the
// original's order of callees.
func judgePackage(context *rule.Context) {
	state := rule.State[gathered](context)
	// declared holds each variable's sole declaring value, built on the first variable passed as an address, since
	// most packages pass none.
	var declared map[*types.Var]ast.Expr
	for _, call := range slices.Concat(state.dials, state.timeoutDials, state.dialerDials) {
		if len(call.Args) < 2 {
			// A multi-valued call may supply the complete argument list.
			continue
		}
		switch address := call.Args[1].(type) {
		case *ast.CallExpr:
			// net.Dial("tcp", fmt.Sprintf("%s:%d", ...)). A third argument, as DialTimeout's or a spread call's, leaves
			// the call alone, as the original does.
			if len(call.Args) == 2 {
				reportAddress(context, address, nil)
			}
		case *ast.Ident:
			// addr := fmt.Sprintf("%s:%d", ...), and later net.Dial("tcp", addr). Like the original, the declaration
			// may be anywhere in the package, and a later assignment to the variable is not looked for.
			variable, isVariable := context.TypesInfo.Uses[address].(*types.Var)
			if !isVariable {
				continue
			}
			if declared == nil {
				declared = soleDeclaredValues(context, state)
			}
			if value, found := declared[variable]; found {
				reportAddress(context, value, call)
			}
		}
	}
}

// reportAddress reports the address expression is, with the fix, when it is a call fmt.Sprintf("%s:%d", host, port) or
// fmt.Sprintf("%s:%s", host, port) with a constant format. dialCall is the dial the address was passed to when it was
// passed through a variable, named in the message by its line, and nil when the call is the dial's own argument.
// x/tools' hostport's checkAddr.
func reportAddress(context *rule.Context, expression ast.Expr, dialCall *ast.CallExpr) {
	call, isCall := expression.(*ast.CallExpr)
	if !isCall || len(call.Args) != 3 || !isFunctionNamed(typeutil.Callee(context.TypesInfo, call), "fmt", "Sprintf") {
		return
	}
	formatArgument := call.Args[0]
	formatValue := context.TypesInfo.Types[formatArgument].Value
	if formatValue == nil {
		return
	}
	// Sprintf's format is a string, so a constant one is a string constant, which StringVal reads without panicking.
	format := constant.StringVal(formatValue)
	if format != "%s:%d" && format != "%s:%s" {
		return
	}
	// Granular edits keep the call's own formatting: net.JoinHostPort in Sprintf's place, and the format deleted up to
	// the host.
	edits := []analysis.TextEdit{
		{Pos: call.Fun.Pos(), End: call.Fun.End(), NewText: []byte("net.JoinHostPort")},
		{Pos: formatArgument.Pos(), End: call.Args[1].Pos()},
	}
	if format == "%s:%d" {
		edits = append(edits, portAsString(call.Args[2])...)
	}
	suffix := ""
	if dialCall != nil {
		suffix = fmt.Sprintf(" (passed to net.Dial at L%d)", context.Fset.Position(dialCall.Pos()).Line)
	}
	context.Report(analysis.Diagnostic{
		Pos:     formatArgument.Pos(),
		End:     formatArgument.End(),
		Message: fmt.Sprintf("address format %q does not work with IPv6%s", format, suffix),
		SuggestedFixes: []analysis.SuggestedFix{{
			Message:   "Replace fmt.Sprintf with net.JoinHostPort",
			TextEdits: edits,
		}},
	})
}

// portAsString is the edits that turn a numeric port into the string net.JoinHostPort takes: an integer literal
// rewritten as its decimal string, `0x7B` to `"123"`, and anything else wrapped in fmt.Sprintf("%d", port). Only a
// literal is rewritten, since rewriting a named constant would lose the name.
func portAsString(port ast.Expr) []analysis.TextEdit {
	if literal, isLiteral := port.(*ast.BasicLit); isLiteral && literal.Kind == token.INT {
		// A literal past int64's range fails to parse and is wrapped, as in the original. A literal is never negative,
		// so every value that parses is rewritten; the original's test that it is not negative cannot fail.
		if value, parseError := strconv.ParseInt(literal.Value, 0, 64); parseError == nil {
			return []analysis.TextEdit{{Pos: port.Pos(), End: port.End(), NewText: fmt.Appendf(nil, `"%d"`, value)}}
		}
	}
	return []analysis.TextEdit{
		{Pos: port.Pos(), End: port.Pos(), NewText: []byte(`fmt.Sprintf("%d", `)},
		{Pos: port.End(), End: port.End(), NewText: []byte(`)`)},
	}
}

// soleDeclaredValues maps each variable the package declares, in `name := value` or `var name = value`, to that value
// when the declaration holds just one, whatever the number of names. It is what the original reads through its index:
// the parent of the identifier Defs records for the variable, an assignment or a value spec, and its one value. The
// assignments are read before the value specs, each in preorder, as two walks of the package read them.
func soleDeclaredValues(context *rule.Context, state *gathered) map[*types.Var]ast.Expr {
	declared := map[*types.Var]ast.Expr{}
	for _, assignment := range state.assignments {
		if len(assignment.Rhs) != 1 {
			continue
		}
		// Only `:=` defines, and Defs holds only the names it declares anew, never one it redeclares, which Uses
		// holds, nor a type switch's symbolic variable, whose entry is nil.
		for _, name := range assignment.Lhs {
			if identifier, isIdentifier := name.(*ast.Ident); isIdentifier {
				if variable, isVariable := context.TypesInfo.Defs[identifier].(*types.Var); isVariable {
					declared[variable] = assignment.Rhs[0]
				}
			}
		}
	}
	for _, specification := range state.specifications {
		if len(specification.Values) != 1 {
			continue
		}
		for _, name := range specification.Names {
			if variable, isVariable := context.TypesInfo.Defs[name].(*types.Var); isVariable {
				declared[variable] = specification.Values[0]
			}
		}
	}
	return declared
}

// isFunctionNamed reports whether object is the package-level function name of the package at path. x/tools'
// typesinternal.IsFunctionNamed, for one name.
func isFunctionNamed(object types.Object, path string, name string) bool {
	function, isFunction := object.(*types.Func)
	return isFunction &&
		function.Pkg() != nil &&
		function.Parent() == function.Pkg().Scope() &&
		function.Pkg().Path() == path &&
		function.Signature().Recv() == nil &&
		function.Name() == name
}

// isMethodNamed reports whether object is the method name of the type typeName declared in the package at path, the
// receiver a pointer or not. x/tools' typesinternal.IsMethodNamed, for one name.
func isMethodNamed(object types.Object, path string, typeName string, name string) bool {
	method, isMethod := object.(*types.Func)
	if !isMethod || method.Signature().Recv() == nil || method.Name() != name {
		return false
	}
	// x/tools' typesinternal.ReceiverNamed: the receiver's named type, through one pointer and any alias.
	receiver := types.Unalias(method.Signature().Recv().Type())
	if pointer, isPointer := receiver.(*types.Pointer); isPointer {
		receiver = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiver.(*types.Named)
	if !isNamed {
		return false
	}
	// x/tools' typesinternal.IsTypeNamed: a type declared at package level.
	typeObject := named.Obj()
	return typeObject.Pkg() != nil &&
		typeObject.Parent() == typeObject.Pkg().Scope() &&
		typeObject.Pkg().Path() == path &&
		typeObject.Name() == typeName
}
