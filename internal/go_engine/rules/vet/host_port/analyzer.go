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
// index, which cohere cannot import. The port requires inspect alone and walks for both: each call whose callee
// typeutil.Callee names, gathered per callee and judged in the original's order of callees, and, only once a variable
// is passed, the declarations of the package's variables. Within one callee the port judges calls in the preorder of
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
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "hostport",
	Doc: `check format of addresses passed to net.Dial

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
`,
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/hostport",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("host_port: inspect's result is missing")
	}
	// The original's index holds the objects of the packages a package refers to, never the package's own, so it
	// never finds net's dial functions inside net itself, nor Sprintf inside fmt, and judges neither package. fmt
	// imports no fmt, so the fast path below already skips it; net is skipped here. The fixtures cannot reach this
	// branch, since a fixture laid out as net is shadowed by the standard library's.
	if pass.Pkg.Path() == "net" {
		return nil, nil
	}
	// The original's fast path: no call can name fmt.Sprintf in a package that does not import fmt.
	if !slices.ContainsFunc(pass.Pkg.Imports(), func(imported *types.Package) bool {
		return imported.Path() == "fmt"
	}) {
		return nil, nil
	}
	// The dial calls, one list per callee, each in preorder, judged in the original's order of callees.
	dials := []*ast.CallExpr{}
	timeoutDials := []*ast.CallExpr{}
	dialerDials := []*ast.CallExpr{}
	for call := range inspector.All[*ast.CallExpr](inspected) {
		callee := typeutil.Callee(pass.TypesInfo, call)
		switch {
		case isFunctionNamed(callee, "net", "Dial"):
			dials = append(dials, call)
		case isFunctionNamed(callee, "net", "DialTimeout"):
			timeoutDials = append(timeoutDials, call)
		case isMethodNamed(callee, "net", "Dialer", "Dial"):
			dialerDials = append(dialerDials, call)
		}
	}
	// declared holds each variable's sole declaring value, built on the first variable passed as an address, since
	// most packages pass none.
	var declared map[*types.Var]ast.Expr
	for _, call := range slices.Concat(dials, timeoutDials, dialerDials) {
		if len(call.Args) < 2 {
			// A multi-valued call may supply the complete argument list.
			continue
		}
		switch address := call.Args[1].(type) {
		case *ast.CallExpr:
			// net.Dial("tcp", fmt.Sprintf("%s:%d", ...)). A third argument, as DialTimeout's or a spread call's, leaves
			// the call alone, as the original does.
			if len(call.Args) == 2 {
				reportAddress(pass, address, nil)
			}
		case *ast.Ident:
			// addr := fmt.Sprintf("%s:%d", ...), and later net.Dial("tcp", addr). Like the original, the declaration
			// may be anywhere in the package, and a later assignment to the variable is not looked for.
			variable, isVariable := pass.TypesInfo.Uses[address].(*types.Var)
			if !isVariable {
				continue
			}
			if declared == nil {
				declared = soleDeclaredValues(pass, inspected)
			}
			if value, found := declared[variable]; found {
				reportAddress(pass, value, call)
			}
		}
	}
	return nil, nil
}

// reportAddress reports the address expression is, with the fix, when it is a call fmt.Sprintf("%s:%d", host, port) or
// fmt.Sprintf("%s:%s", host, port) with a constant format. dialCall is the dial the address was passed to when it was
// passed through a variable, named in the message by its line, and nil when the call is the dial's own argument.
// x/tools' hostport's checkAddr.
func reportAddress(pass *analysis.Pass, expression ast.Expr, dialCall *ast.CallExpr) {
	call, isCall := expression.(*ast.CallExpr)
	if !isCall || len(call.Args) != 3 || !isFunctionNamed(typeutil.Callee(pass.TypesInfo, call), "fmt", "Sprintf") {
		return
	}
	formatArgument := call.Args[0]
	formatValue := pass.TypesInfo.Types[formatArgument].Value
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
		suffix = fmt.Sprintf(" (passed to net.Dial at L%d)", pass.Fset.Position(dialCall.Pos()).Line)
	}
	pass.Report(analysis.Diagnostic{
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
// the parent of the identifier Defs records for the variable, an assignment or a value spec, and its one value.
func soleDeclaredValues(pass *analysis.Pass, inspected *inspector.Inspector) map[*types.Var]ast.Expr {
	declared := map[*types.Var]ast.Expr{}
	for assignment := range inspector.All[*ast.AssignStmt](inspected) {
		if len(assignment.Rhs) != 1 {
			continue
		}
		// Only `:=` defines, and Defs holds only the names it declares anew, never one it redeclares, which Uses
		// holds, nor a type switch's symbolic variable, whose entry is nil.
		for _, name := range assignment.Lhs {
			if identifier, isIdentifier := name.(*ast.Ident); isIdentifier {
				if variable, isVariable := pass.TypesInfo.Defs[identifier].(*types.Var); isVariable {
					declared[variable] = assignment.Rhs[0]
				}
			}
		}
	}
	for specification := range inspector.All[*ast.ValueSpec](inspected) {
		if len(specification.Values) != 1 {
			continue
		}
		for _, name := range specification.Names {
			if variable, isVariable := pass.TypesInfo.Defs[name].(*types.Var); isVariable {
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
