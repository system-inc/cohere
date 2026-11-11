// Package printf is vet's printf pass, ported as ours: a call of a formatting function such as fmt.Printf, or of any
// function found to wrap one, whose format string does not parse or whose verbs do not fit the arguments in number
// or type; a call of a print function such as fmt.Println that looks meant for its printf counterpart; a printf call
// with a non-constant format and no other arguments; a printf call whose %w wraps a pointer to an error; and a
// wrapper that forwards its arguments without `...`.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/printf/printf.go and types.go, with
// the parser of golang.org/x/tools@v0.50.0 internal/fmtstr/parse.go, whose logic it keeps and whose messages and
// suggested fix it reports word for word, at the same positions. The x/tools internal helpers it calls are translated
// beside it, each naming the helper it translates. Its match of an argument's type against a verb, the argMatcher of
// types.go with the normalization of a type parameter's type set from internal/typeparams, is translated into
// type_predicates/verbs, which the restore from the host shares (#b2t9xmf). It exports its own fact type, isWrapper,
// with the original's meaning and the original's words for it, so a wrapper declared in a dependency is found as the
// original finds it, and it takes the original's -funcs flag. The registry runs the port in the original's place, and
// the original stays the oracle: the parity tests beside this file run both over the same packages.
//
// Two things differ, neither in what it finds. The original's analyzer has a result, which reports to a client
// whether a function behaves like fmt.Print or fmt.Printf; nothing in cohere requires it, so the port has none. And
// where the original asserts a type the type checker always gives, and would panic were it missing, the port checks
// the assertion and passes the node by.
//
// It is not on the engine's syntax dispatcher, and stays a plain analyzer: it exports and imports a fact, isWrapper,
// through which a wrapper declared in a dependency is found, and a listener has no facts to export or import.
package printf

import (
	"go/ast"
	"go/types"
	"go/version"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/types/typeutil"
)

// knownPrintFunctions are the print functions known by name, each a printf when its name ends in f, to which the
// -funcs flag adds: x/tools' printf isPrint. A name is a function's or a method's (*types.Func).FullName, or a name
// without a dot, matched against any function's name in any case. The standard library's functions are listed
// though the pass could find most of them as wrappers, since a driver that runs no analyzer over the standard
// library, as `go vet` with gccgo does not, would then know none.
var knownPrintFunctions = stringSet{
	"fmt.Appendf":  true,
	"fmt.Append":   true,
	"fmt.Appendln": true,
	"fmt.Errorf":   true,
	"fmt.Fprint":   true,
	"fmt.Fprintf":  true,
	"fmt.Fprintln": true,
	"fmt.Print":    true,
	"fmt.Printf":   true,
	"fmt.Println":  true,
	"fmt.Sprint":   true,
	"fmt.Sprintf":  true,
	"fmt.Sprintln": true,

	"runtime/trace.Logf": true,

	"log.Print":             true,
	"log.Printf":            true,
	"log.Println":           true,
	"log.Fatal":             true,
	"log.Fatalf":            true,
	"log.Fatalln":           true,
	"log.Panic":             true,
	"log.Panicf":            true,
	"log.Panicln":           true,
	"(*log.Logger).Fatal":   true,
	"(*log.Logger).Fatalf":  true,
	"(*log.Logger).Fatalln": true,
	"(*log.Logger).Panic":   true,
	"(*log.Logger).Panicf":  true,
	"(*log.Logger).Panicln": true,
	"(*log.Logger).Print":   true,
	"(*log.Logger).Printf":  true,
	"(*log.Logger).Println": true,

	"(*testing.common).Error":  true,
	"(*testing.common).Errorf": true,
	"(*testing.common).Fatal":  true,
	"(*testing.common).Fatalf": true,
	"(*testing.common).Log":    true,
	"(*testing.common).Logf":   true,
	"(*testing.common).Skip":   true,
	"(*testing.common).Skipf":  true,
	"(testing.TB).Error":       true,
	"(testing.TB).Errorf":      true,
	"(testing.TB).Fatal":       true,
	"(testing.TB).Fatalf":      true,
	"(testing.TB).Log":         true,
	"(testing.TB).Logf":        true,
	"(testing.TB).Skip":        true,
	"(testing.TB).Skipf":       true,
}

// Analyzer is the port, under the original's name, with its documentation and its link word for word, so the rule,
// its settings and every `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = newAnalyzer()

// newAnalyzer is the port's analyzer with the original's -funcs flag, which adds to knownPrintFunctions.
func newAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: "printf",
		Doc: "check consistency of Printf format strings and arguments\n\n" +
			"The check applies to calls of the formatting functions such as\n" +
			"[fmt.Printf] and [fmt.Sprintf], as well as any detected wrappers of\n" +
			"those functions such as [log.Printf]. It reports a variety of\n" +
			"mistakes such as syntax errors in the format string and mismatches\n" +
			"(of number and type) between the verbs and their arguments.\n\n" +
			"See the documentation of the fmt package for the complete set of\n" +
			"format operators and their operand types.",
		URL:       "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/printf",
		Requires:  []*analysis.Analyzer{inspect.Analyzer},
		Run:       run,
		FactTypes: []analysis.Fact{new(isWrapper)},
	}
	analyzer.Flags.Var(knownPrintFunctions, "funcs", "comma-separated list of print function names to check")
	return analyzer
}

// run finds the package's wrappers, then checks every call of a print function.
func run(pass *analysis.Pass) (any, error) {
	// What each function the pass has met is, found or looked up, x/tools' printf Result.
	kinds := wrapperKinds{}
	findPrintLike(pass, kinds)
	checkCalls(pass, kinds)
	return nil, nil
}

// checkCalls checks each call of a print or printf function, as kinds knows it, with the Go version of the file
// it is in, which gates some checks.
func checkCalls(pass *analysis.Pass, kinds wrapperKinds) {
	walker, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		return
	}
	// The file's Go language version, "" when unknown.
	fileVersion := ""
	walker.Preorder([]ast.Node{(*ast.File)(nil), (*ast.CallExpr)(nil)}, func(node ast.Node) {
		switch visited := node.(type) {
		case *ast.File:
			fileVersion = version.Lang(fileVersionOf(pass.TypesInfo, visited))
		case *ast.CallExpr:
			callee := typeutil.Callee(pass.TypesInfo, visited)
			if callee == nil {
				return
			}
			switch kinds.of(pass, callee) {
			case kindPrintf:
				checkPrintf(pass, fileVersion, kindPrintf, visited, fullName(callee))
			case kindErrorf:
				checkPrintf(pass, fileVersion, kindErrorf, visited, fullName(callee))
			case kindPrint:
				checkPrint(pass, visited, fullName(callee))
			case kindNone:
			}
		}
	})
}

// fullName is a function's full name, such as "(*log.Logger).Printf", and a variable's name.
func fullName(object types.Object) string {
	if function, isFunction := object.(*types.Func); isFunction {
		return function.FullName()
	}
	return object.Name()
}

// origin is the generic declaration of a function or variable, which facts are about.
func origin(object types.Object) types.Object {
	switch declared := object.(type) {
	case *types.Func:
		return declared.Origin()
	case *types.Var:
		return declared.Origin()
	}
	return object
}
