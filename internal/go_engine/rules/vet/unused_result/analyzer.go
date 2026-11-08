// Package unused_result is vet's unusedresult pass, a call statement that drops the result of a function whose result
// is the whole point of calling it: fmt.Sprintf and errors.New have no effect but what they return, and
// context.WithCancel returns a cancel function that must be called. Two lists name what is judged, each a flag: funcs,
// the package-level functions by import path and name, and stringmethods, the names of methods of type
// `func() string`, Error and String by default. A call in the body of a `for b.Loop()` benchmark loop is left alone,
// since testing.B.Loop keeps its results alive by design. It sits on the engine's syntax dispatcher, hearing each
// expression statement on the walk the dispatcher makes once for every syntax rule.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/unusedresult/unusedresult.go,
// whose logic it keeps and whose messages it reports word for word, at the same position. The one difference is
// where the flags live: the original binds them to package variables, so every copy of its analyzer shares one
// setting, and the port reads them as its context's options, the flags of the analyzer the rule was built as, so a
// configured copy and the default can run side by side. The names, defaults and parsing of the two flags are the
// original's. The registry runs the port in the original's place, and the original stays the oracle: the parity
// tests beside this file run both over the same packages, under the default flags and under others. The two agree
// everywhere, so there is no differences fixture.
package unused_result

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "unusedresult"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for unused results of calls to some functions\n\n" +
	"Some functions like fmt.Errorf return a result and have no side\n" +
	"effects, so it is always a mistake to discard the result. Other\n" +
	"functions may return an error that must not be ignored, or a cleanup\n" +
	"operation that must be called. This analyzer reports calls to\n" +
	"functions like these when the result of the call is ignored.\n\n" +
	"The set of functions may be controlled using flags."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unusedresult"

// functionsOption names the flag listing the package-level functions whose results must be used, each as its
// package's import path, a dot and its name.
const functionsOption = "funcs"

// stringMethodsOption names the flag listing the names of the methods of type `func() string` whose results must be
// used.
const stringMethodsOption = "stringmethods"

// Listener is what the port hears of each package from the dispatcher: every expression statement, in preorder, as
// the original's Preorder visits them, with its ancestors, of which the benchmark loop's exemption reads the parent
// and grandparent. The two lists are read from the context's options once for the package, on the first statement,
// and kept in the context's state.
var Listener = rule.Listener{
	Nodes: []ast.Node{(*ast.ExprStmt)(nil)},
	Node:  checkStatement,
}

// Analyzer runs the port under the original's name, documentation, address and flags, reporting what the shared
// syntax dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// defaultFunctions is the original's list of the standard library's functions whose results must be used. Its
// context.With entries are effectively redundant with the lostcancel pass, as the original notes.
var defaultFunctions = nameSet{
	"context.WithCancel":      true,
	"context.WithDeadline":    true,
	"context.WithTimeout":     true,
	"context.WithValue":       true,
	"errors.New":              true,
	"fmt.Append":              true,
	"fmt.Appendf":             true,
	"fmt.Appendln":            true,
	"fmt.Errorf":              true,
	"fmt.Sprint":              true,
	"fmt.Sprintf":             true,
	"fmt.Sprintln":            true,
	"maps.All":                true,
	"maps.Clone":              true,
	"maps.Collect":            true,
	"maps.Equal":              true,
	"maps.EqualFunc":          true,
	"maps.Keys":               true,
	"maps.Values":             true,
	"slices.All":              true,
	"slices.AppendSeq":        true,
	"slices.Backward":         true,
	"slices.BinarySearch":     true,
	"slices.BinarySearchFunc": true,
	"slices.Chunk":            true,
	"slices.Clip":             true,
	"slices.Clone":            true,
	"slices.Collect":          true,
	"slices.Compact":          true,
	"slices.CompactFunc":      true,
	"slices.Compare":          true,
	"slices.CompareFunc":      true,
	"slices.Concat":           true,
	"slices.Contains":         true,
	"slices.ContainsFunc":     true,
	"slices.Delete":           true,
	"slices.DeleteFunc":       true,
	"slices.Equal":            true,
	"slices.EqualFunc":        true,
	"slices.Grow":             true,
	"slices.Index":            true,
	"slices.IndexFunc":        true,
	"slices.Insert":           true,
	"slices.IsSorted":         true,
	"slices.IsSortedFunc":     true,
	"slices.Max":              true,
	"slices.MaxFunc":          true,
	"slices.Min":              true,
	"slices.MinFunc":          true,
	"slices.Repeat":           true,
	"slices.Replace":          true,
	"slices.Sorted":           true,
	"slices.SortedFunc":       true,
	"slices.SortedStableFunc": true,
	"slices.Values":           true,
	"sort.Reverse":            true,
}

// stringResultSignature is `func() string`, the type a method must have for stringmethods to judge it. Identity of
// signatures ignores the receiver, so it matches a method of any type.
var stringResultSignature = types.NewSignatureType(
	nil, nil, nil, nil, types.NewTuple(types.NewParam(token.NoPos, nil, "", types.Typ[types.String])), false,
)

// qualifiedName is one entry of funcs split at its last dot, so a callee is looked up without building its text.
type qualifiedName struct {
	packagePath string
	name        string
}

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset, and the original's two flags, the options the listener's context reads, each holding a
// set of its own, filled with the original's default, so a copy configured in a test never changes the one the
// registry runs.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	// A set of its own, so nothing done to one analyzer's flag reaches another's or the defaults.
	functions := maps.Clone(defaultFunctions)
	analyzer.Flags.Var(&functions, functionsOption, "comma-separated list of functions whose results must be used")
	stringMethods := nameSet{"Error": true, "String": true}
	analyzer.Flags.Var(&stringMethods, stringMethodsOption,
		"comma-separated list of names of methods of type func() string whose results must be used")
	return analyzer
}

// packageState is what the port reads once for its package, in the context's state, as the original reads it before
// it walks anything: its two lists.
type packageState struct {
	// decided is whether the lists have been read yet.
	decided bool
	// functions is funcs, each entry split at its last dot.
	functions map[qualifiedName]bool
	// stringMethods is stringmethods as its flag holds it.
	stringMethods *nameSet
}

// checkStatement reports node when it is a call statement dropping the result of a function or method the lists
// name, outside the body of a benchmark loop.
func checkStatement(context *rule.Context, node ast.Node, stack []ast.Node) {
	statement, isStatement := node.(*ast.ExprStmt)
	if !isStatement {
		return
	}
	state := rule.State[packageState](context)
	if !state.decided {
		state.decided = true
		// Split once per package, as the original does, so each call is looked up by a key rather than by built text.
		// An entry with no dot, or only a leading one, names no package, and is skipped.
		state.functions = map[qualifiedName]bool{}
		for entry := range *optionOf(context, functionsOption) {
			if index := strings.LastIndexByte(entry, '.'); index > 0 {
				state.functions[qualifiedName{packagePath: entry[:index], name: entry[index+1:]}] = true
			}
		}
		state.stringMethods = optionOf(context, stringMethodsOption)
	}
	// `(fmt.Sprint(""))` is a call statement too: the specification lets one be parenthesized.
	call, isCall := ast.Unparen(statement.X).(*ast.CallExpr)
	if !isCall {
		return
	}
	if inBenchmarkLoop(context.TypesInfo, stack) {
		return
	}
	// A call of a function value, a builtin or a conversion has no *types.Func, and is never judged. Callee looks
	// through parentheses and type arguments, and resolves a method value or expression to its method.
	function, isFunction := typeutil.Callee(context.TypesInfo, call).(*types.Func)
	if !isFunction {
		return
	}
	if function.Signature().Recv() != nil {
		if types.Identical(function.Signature(), stringResultSignature) && (*state.stringMethods)[function.Name()] {
			context.Report(analysis.Diagnostic{
				Pos: call.Pos(),
				End: call.Lparen,
				Message: fmt.Sprintf("result of (%s).%s call not used",
					function.Signature().Recv().Type(), function.Name()),
			})
		}
		return
	}
	// Only a declared function has no receiver, and every declared function belongs to a package: the universe
	// declares none, so Pkg is never nil here.
	if state.functions[qualifiedName{packagePath: function.Pkg().Path(), name: function.Name()}] {
		context.Report(analysis.Diagnostic{
			Pos:     call.Pos(),
			End:     call.Lparen,
			Message: fmt.Sprintf("result of %s.%s call not used", function.Pkg().Path(), function.Name()),
		})
	}
}

// optionOf is the set the named option of the rule holds, the flag of the analyzer the rule was built as. It is read
// from the context rather than bound to a package variable, so a configured copy of the analyzer and the registry's
// can run side by side.
func optionOf(context *rule.Context, option string) *nameSet {
	// Option panics on an option the analyzer has no flag for, and newAnalyzer defines both.
	names, isNameSet := context.Option(option).(*nameSet)
	if !isNameSet {
		// Invariant: newAnalyzer defines both flags with a nameSet as their value.
		panic("unused_result: the " + option + " flag does not hold a set of names")
	}
	return names
}

// inBenchmarkLoop reports whether the expression statement stack leads to, its ancestors outermost first, is a
// statement of the body of a `for b.Loop()` loop, where b is a *testing.B, whose Loop keeps the results of the calls
// there alive, so they are unused on purpose. Only a statement directly in the body counts, as in the original: one
// nested in a block, an if or a label is judged.
func inBenchmarkLoop(information *types.Info, stack []ast.Node) bool {
	// The statement's parent, the last of its ancestors, is the loop's body, and its grandparent, the one before, the
	// loop. A statement always sits in a file and a declaration, so it has both; one with fewer is in no loop.
	if len(stack) < 2 {
		return false
	}
	// A for statement's init and post are simple statements, never a block, so a block whose parent is a for
	// statement is that loop's body.
	if _, inBlock := stack[len(stack)-1].(*ast.BlockStmt); !inBlock {
		return false
	}
	loop, isLoop := stack[len(stack)-2].(*ast.ForStmt)
	if !isLoop {
		return false
	}
	// A loop with no condition has a nil Cond, which is no call.
	condition, isCall := ast.Unparen(loop.Cond).(*ast.CallExpr)
	if !isCall {
		return false
	}
	return isMethodNamed(typeutil.Callee(information, condition), "testing", "B", "Loop")
}

// isMethodNamed reports whether object is the method name of typeName, a package-level type declared in packagePath,
// through a pointer receiver or not. x/tools' typesinternal.IsMethodNamed for one name, with its
// typesinternal.ReceiverNamed and typesinternal.IsTypeNamed folded in.
func isMethodNamed(object types.Object, packagePath string, typeName string, name string) bool {
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Signature().Recv() == nil {
		return false
	}
	// receiverType is the receiver's type with its pointer, if it has one, taken off.
	receiverType := types.Unalias(function.Signature().Recv().Type())
	if pointer, isPointer := receiverType.(*types.Pointer); isPointer {
		receiverType = types.Unalias(pointer.Elem())
	}
	named, isNamed := receiverType.(*types.Named)
	return isNamed &&
		named.Obj() != nil &&
		isPackageLevel(named.Obj()) &&
		named.Obj().Pkg().Path() == packagePath &&
		named.Obj().Name() == typeName &&
		function.Name() == name
}

// isPackageLevel reports whether object is declared at its package's top level. x/tools'
// typesinternal.IsPackageLevel.
func isPackageLevel(object types.Object) bool {
	return object.Pkg() != nil && object.Parent() == object.Pkg().Scope()
}
