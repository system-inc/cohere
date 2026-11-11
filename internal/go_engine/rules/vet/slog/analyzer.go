// Package slog is vet's slog pass, a call to a log/slog function or method taking alternating key-value pairs whose
// arguments do not pair: an argument in a key position that is neither a string nor a slog.Attr, or a final key with no
// value after it. It reads only the static types of the arguments, so an argument of an interface type may be either,
// and the pass judges what follows it before it calls the call wrong. It sits on the engine's syntax dispatcher,
// hearing each call on the walk the dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/slog/slog.go, whose logic and
// table of slog's functions it keeps entry for entry, and whose messages it reports word for word, over the same
// range. The registry runs the port in the original's place, and the original stays the oracle: the parity tests
// beside this file run both over the same packages.
//
// The two differ only where adjudicated, each case in testdata/src/differences and held by
// TestPortDiffersFromOriginalOnlyWhereItIsRight, and in each the original is wrong. A key whose type is an alias of
// string is a string, `type Name = string` denotes string itself, and slog takes it as a key; the original compares
// the alias the checker records with the predeclared string, so it calls such a key neither a string nor an Attr, and
// an unknown argument before one a key that cannot be. The port looks through the alias (isString). A key whose type
// is a type parameter every term of whose constraint is exactly string, `[Key string]`, is a string whatever Key is
// instantiated to; the original calls any type parameter neither, since no string is assignable to one. The port
// takes such a key as a string (isString), and only such a key: a term `~string` admits a named string type, which
// slog logs as `!BADKEY` as the original reports it, so a tilde, a union with another type or a method keeps the
// original's finding. And a
// parenthesized method expression, `((*slog.Logger).Debug)(logger, "message", "key", 1)`, still passes its receiver
// first; the original looks for the method expression without looking through the parentheses, so it reads the pairs
// one argument early, reports pairs that are right and misses ones that are wrong. The port looks through them
// (isMethodExpression).
package slog

import (
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "slog"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words. It
// keeps the original's missing space in "For example,it".
const passDoc = "check for invalid structured logging calls\n\n" +
	"The slog checker looks for calls to functions from the log/slog\n" +
	"package that take alternating key-value pairs. It reports calls\n" +
	"where an argument in a key position is neither a string nor a\n" +
	"slog.Attr, and where a final key is missing its value.\n" +
	"For example,it would report\n\n" +
	"\tslog.Warn(\"message\", 11, \"k\") // slog.Warn arg \"11\" should be a string or a slog.Attr\n\n" +
	"and\n\n" +
	"\tslog.Info(\"message\", \"k1\", v1, \"k2\") // call to slog.Info missing a final value"

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/slog"

// Listener is what the port hears of each package from the dispatcher: every call, in preorder, as the original's
// Preorder visits them, of which it judges those of log/slog's functions taking key-value pairs.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node:      checkCall,
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

// stringType is the predeclared string, which a key's static type must be (isString): slog reads a key with a type
// switch whose case is string, which a named string type's value does not match, so it logs such a key as `!BADKEY`.
var stringType = types.Universe.Lookup("string").Type()

// keyValueFunctions is the original's kvFuncs, entry for entry: the functions and methods of log/slog that take
// `...any` for key-value pairs, keyed by the receiver's type name with its pointer taken off, or "" for a package-level
// function, then by name, each mapped to the count of arguments before the pairs begin.
var keyValueFunctions = map[string]map[string]int{
	"": {
		"Debug":        1,
		"Info":         1,
		"Warn":         1,
		"Error":        1,
		"DebugContext": 2,
		"InfoContext":  2,
		"WarnContext":  2,
		"ErrorContext": 2,
		"Log":          3,
		"Group":        1,
	},
	"Logger": {
		"Debug":        1,
		"Info":         1,
		"Warn":         1,
		"Error":        1,
		"DebugContext": 2,
		"InfoContext":  2,
		"WarnContext":  2,
		"ErrorContext": 2,
		"Log":          3,
		"With":         0,
	},
	"Record": {
		"Add": 0,
	},
}

// checkCall judges node when it is a static call of a function or method keyValueFunctions names, reporting the first
// argument in a key position that cannot be a key, and otherwise a final key with no value. It reports at most one
// finding a call, since after one misplaced argument every position after it is a guess.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	function := typeutil.StaticCallee(context.TypesInfo, call)
	if function == nil {
		// A call of a function value or an interface method names no function the table could hold.
		return
	}
	if call.Ellipsis != token.NoPos {
		// The pairs of a spread slice are not in the syntax.
		return
	}
	skipped, takesPairs := keyValueArgumentsStart(function)
	if !takesPairs {
		return
	}
	// firstPair is where the pairs begin among call's arguments. A method expression's call passes the receiver as
	// its first argument, `(*slog.Logger).Debug(logger, "message", ...)`, so its pairs begin one argument later.
	firstPair := skipped
	if isMethodExpression(context.TypesInfo, call) {
		firstPair++
	}
	if len(call.Args) <= firstPair {
		// No pairs at all. Kept for fidelity: the loop below would judge nothing and report nothing, so no mutant of
		// this guard can go red, and a call with fewer arguments than the table skips does not type-check.
		return
	}
	// Every function the table names is log/slog's, so its package's scope holds Attr. The original looks it up once
	// a pass, at the first such call, and the port once a call; both find the same object. The look is recorded in the
	// context's reads (#b2t9xmf).
	attrType := context.LookupIn(function.Pkg(), "Attr").Type()
	// The first argument after those skipped should be a key or an Attr.
	expected := expectKey
	// unknownArgument is nil, or the last argument at a position whose expectation was unknown.
	var unknownArgument ast.Expr
	for _, argument := range call.Args[firstPair:] {
		argumentType := context.TypesInfo.Types[argument].Type
		switch expected {
		case expectKey:
			switch {
			case isString(argumentType):
				expected = expectValue
			case isAttr(argumentType):
				// An Attr is a whole pair, so a key follows it.
				expected = expectKey
			// The pass follows no data, so an argument of an interface type may hold anything its type admits.
			case types.IsInterface(argumentType) && types.AssignableTo(stringType, argumentType):
				// A string fits, so the type is empty and an Attr fits as well: what follows is unknown.
				expected = expectUnknown
			case types.IsInterface(argumentType) && types.AssignableTo(attrType, argumentType):
				// An Attr fits and a string does not, fmt.Stringer for one, so it is taken for an Attr.
				expected = expectKey
			default:
				// Neither a string nor an Attr, nor an interface either could fill.
				reportMisplaced(context, function, argument, unknownArgument)
				return
			}
		case expectValue:
			// Anything can be a value, and a key follows it.
			expected = expectKey
		case expectUnknown:
			// From here on, a problem later or at the end may be a missing final value or a non-key in a key's place,
			// and the messages say they cannot tell which.
			unknownArgument = argument
			if !isString(argumentType) && !isAttr(argumentType) && !types.IsInterface(argumentType) {
				// This argument cannot be a key, so the unknown one before it was one, this is its value, and a key
				// follows.
				expected = expectKey
			}
		}
	}
	if expected == expectValue {
		if unknownArgument == nil {
			context.ReportRangef(call, "call to %s missing a final value", shortName(function))
		} else {
			context.ReportRangef(call, "call to %s has a missing or misplaced value", shortName(function))
		}
	}
}

// reportMisplaced reports argument, in a key position, as neither a string nor an Attr, naming unknownArgument when an
// argument before it left its position unknown, since then the mistake may be that argument's.
func reportMisplaced(context *rule.Context, function *types.Func, argument ast.Expr, unknownArgument ast.Expr) {
	if unknownArgument == nil {
		context.ReportRangef(argument, "%s arg %q should be a string or a slog.Attr (possible missing key or value)",
			shortName(function), formatted(context.FileSet, argument))
		return
	}
	context.ReportRangef(
		argument,
		"%s arg %q should probably be a string or a slog.Attr (previous arg %q cannot be a key)",
		shortName(function), formatted(context.FileSet, argument), formatted(context.FileSet, unknownArgument),
	)
}

// keyValueArgumentsStart is the count of function's arguments before its key-value pairs, and true, when function is
// one keyValueFunctions names, and 0 and false otherwise. x/tools' kvFuncSkipArgs.
func keyValueArgumentsStart(function *types.Func) (int, bool) {
	if function.Pkg() == nil || function.Pkg().Path() != "log/slog" {
		return 0, false
	}
	// A package-level function's entries are under "".
	receiverName := ""
	if receiver := function.Signature().Recv(); receiver != nil {
		named := receiverNamed(receiver)
		if named == nil {
			// A method of an unnamed struct or interface type, which the table cannot name.
			return 0, false
		}
		receiverName = named.Obj().Name()
	}
	start, found := keyValueFunctions[receiverName][function.Name()]
	return start, found
}

// shortName is how a message names function, shorter than its full name: "slog.Info" for "log/slog.Info", and
// "slog.Logger.With" for "(*log/slog.Logger).With". x/tools' slog shortName.
func shortName(function *types.Func) string {
	receiverPrefix := ""
	if receiver := function.Signature().Recv(); receiver != nil {
		if named := receiverNamed(receiver); named != nil {
			receiverPrefix = named.Obj().Name() + "."
		} else {
			// Kept for fidelity, and unreachable: keyValueArgumentsStart refuses a method of an unnamed type before
			// any message names it.
			receiverPrefix = receiver.Type().String() + "."
		}
	}
	return fmt.Sprintf("%s.%s%s", function.Pkg().Name(), receiverPrefix, function.Name())
}

// receiverNamed is the named type of receiver, a pointer to it taken off and aliases looked through, and nil when the
// receiver's type is unnamed. x/tools' typesinternal.ReceiverNamed, less the pointer report the pass never reads.
func receiverNamed(receiver *types.Var) *types.Named {
	receiverType := types.Unalias(receiver.Type())
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiverType.(*types.Named)
	if !isNamed {
		return nil
	}
	return named
}

// isString reports whether every type valueType can stand for is the predeclared string, so a value of it is a string
// to slog's type switch: string itself, through any alias, which the original misses since it compares valueType with
// string and the checker records a value of an alias's type as the alias; a type parameter whose constraint is
// string-only, which the original misses since no string is assignable to a type parameter; and, as such a
// constraint, an interface every embedded element of which is string-only and which declares no method, or a union
// every term of which is exactly string. A tilde admits named string types and is never string-only. An interface a
// value can have embeds no type term, so it bottoms out empty or at a method, and is never string-only either.
func isString(valueType types.Type) bool {
	switch typed := types.Unalias(valueType).(type) {
	case *types.Basic:
		return typed == stringType
	case *types.TypeParam:
		return isString(typed.Constraint())
	case *types.Union:
		// A union of exact terms all string would repeat a term, which does not type-check, so only a lone tilde
		// term, `~string`, reaches here in practice, and is refused.
		for index := range typed.Len() {
			term := typed.Term(index)
			if term.Tilde() || !isString(term.Type()) {
				return false
			}
		}
		return true
	}
	constraint, isInterface := valueType.Underlying().(*types.Interface)
	if !isInterface || constraint.NumExplicitMethods() > 0 || constraint.NumEmbeddeds() == 0 {
		// Not a constraint of type terms, or one with a method no string has, or `any`, whose type set is every
		// type.
		return false
	}
	for index := range constraint.NumEmbeddeds() {
		if !isString(constraint.EmbeddedType(index)) {
			return false
		}
	}
	return true
}

// isAttr reports whether valueType is log/slog's Attr, or an alias of it. x/tools' typesinternal.IsTypeNamed, for the
// one type the pass asks about.
func isAttr(valueType types.Type) bool {
	named, isNamed := types.Unalias(valueType).(*types.Named)
	if !isNamed {
		return false
	}
	object := named.Obj()
	// x/tools' typesinternal.IsPackageLevel. Kept for fidelity, and no fixture can reach it: only a type declared in a
	// function of log/slog itself would pass the path and name below and fail this.
	packageLevel := object != nil && object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
	return packageLevel && object.Pkg().Path() == "log/slog" && object.Name() == "Attr"
}

// isMethodExpression reports whether call calls a method expression, `(*slog.Logger).Debug(logger, ...)`, whose
// receiver is its first argument. x/tools' slog isMethodExpr, which looks at call's function as written, so that a
// parenthesized method expression, `((*slog.Logger).Debug)(logger, ...)`, is not one to it, and its pairs are read
// with the receiver among them. The port looks through the parentheses, as typeutil.StaticCallee does to find the
// method at all.
func isMethodExpression(information *types.Info, call *ast.CallExpr) bool {
	selector, isSelector := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	selection := information.Selections[selector]
	return selection != nil && selection.Kind() == types.MethodExpr
}

// formatted is node printed as gofmt prints it, as a message quotes an argument. x/tools' astutil.Format.
func formatted(files *token.FileSet, node ast.Node) string {
	var builder strings.Builder
	// Ignored: a strings.Builder never fails a write, and an expression the checker typed always prints; the original
	// drops the error too.
	_ = printer.Fprint(&builder, files, node)
	return builder.String()
}
