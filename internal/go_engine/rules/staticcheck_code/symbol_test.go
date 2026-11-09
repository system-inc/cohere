package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/pattern"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// symbolNames are the names each expression is asked about: a symbol of every kind the symbols fixture uses, each
// spelled as pattern's Symbol compares it, and names it could be mistaken for.
var symbolNames = []string{
	"symbols.local", "strings.Repeat", "(symbols.record).method", "(*symbols.record).pointerMethod",
	"(*sync.WaitGroup).Add", "(*sync.WaitGroup).Wait", "(fmt.Stringer).String", "fmt.Println",
	"len", "append", "cap",
	"symbols.record", "symbols.alias", "symbols.twice", "sync.WaitGroup", "symbols.waitGroup", "int", "error", "any",
	"symbols.number", "symbols.slice", "[]int", "symbols.List[T any]", "symbols.pair[K comparable, V any]",
	"symbols.inner",
	"symbols.table", "symbols.lists", "symbols.limit", "math.Pi", "os.Args",
	"symbols.identity", "symbols.both",
	"local", "record", "symbols.parameter", "symbols.field", "missing",
	// Names a step from a symbol's, each failing one of the comparisons the port makes in place of building the name:
	// a method named as a function, a receiver without its parentheses or its dot, the wrong receiver, and a package
	// path or a name a letter off or a letter short.
	"symbols.method", "symbols.pointerMethod", "(symbols.record).pointerMethod", "(sync.WaitGroup).Add",
	"*sync.WaitGroup).Add", "(*sync.WaitGroup)Add", "(*sync.WaitGroup).Ad", "symbolz.local", "symbols.loca",
	"symbols/local", "fmt.Print", "Println", "strings.Repeatx", "math.P", "mathx.Pi", "os.Arg", "symbols.limits",
	"symbols_table", "symbols.List", "symbols.List[int]", "symbols.List[T any", "symbols.Lis[T any]", "symbols.recor",
	"symbols.records", "symbol.record", "sync.WaitGroups", "(symbols.record)", "integer", "in", "Pointer",
	"unsafe.Pointer", "unsafe.Pointe", "string", "rune", "int32",
	// A generic alias and an alias of one of its instances, asked by their printed forms and by those a step off: the
	// alias's name without its brackets, with an instance's in place of its parameters, or with them cut short.
	"symbols.genericAlias", "symbols.genericAlias[T any]", "symbols.genericAlias[int]", "symbols.genericAlias[T any",
	"symbols.genericAlia[T any]", "symbols.integers", "symbols.integers[int]",
}

// symbolComparison is what one package's side by side run of IsSymbol and SymbolIndex found.
type symbolComparison struct {
	// questions counts the expressions asked, each about every name of symbolNames, and of SymbolIndex about all of
	// them at once.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met counts each label the run met, among them those requiredSymbolLabels names.
	met map[string]int
}

// symbolAnalyzer asks both sides of every expression of a package, its result a symbolComparison. It requires
// staticcheck's generated pass, which PassContext does.
var symbolAnalyzer = &analysis.Analyzer{
	Name:       "comparesymbol",
	Doc:        "asks staticcheck's pattern (Symbol name) and the port of every expression, and compares them",
	Requires:   []*analysis.Analyzer{generated.Analyzer},
	Run:        compareSymbol,
	ResultType: reflect.TypeFor[*symbolComparison](),
}

// Both sides answer alike for every expression of the symbols fixture about each name of symbolNames, and SymbolIndex
// asked about all of them names the first that staticcheck matches, as an Or of them would.
// staticcheck's side is its pattern package matching a parsed `(Symbol "name")` through code.Match, as every pattern
// that names a symbol asks it of a call's function or an operand. The test fails unless every branch of Symbol was
// met finding a symbol and finding none, so a fixture that stops holding one cannot pass by asking nothing of it.
func TestIsSymbolMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"symbols"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading symbols: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{symbolAnalyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing symbols: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*symbolComparison)
		if !isComparison {
			test.Fatalf("running %s: its result is %T, where a comparison was expected", action, action.Result)
		}
		questions += compared.questions
		for _, mismatch := range compared.mismatches {
			test.Error(mismatch)
		}
		for label, count := range compared.met {
			met[label] += count
		}
	}
	test.Logf("%d questions asked of each side", questions)
	for _, label := range requiredSymbolLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// compareSymbol asks both sides of every expression of the pass's package about every name, recording each question
// they answer differently and the labels it met: the expression's kind with the answer, and for the identifier or
// selector the pattern resolves, the kind of object it names with the answer, and whether that object is an alias.
func compareSymbol(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	patterns := map[string]pattern.Pattern{}
	for _, name := range symbolNames {
		patterns[name] = pattern.MustParse(fmt.Sprintf("(Symbol %q)", name))
	}
	compared := &symbolComparison{met: map[string]int{}}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			kind := reflect.TypeOf(expression).Elem().Name()
			objectKind := resolvedKind(pass, expression)
			// The index of the first name staticcheck matches, or -1 when it matches none, which an Or of every name
			// matches and SymbolIndex must name.
			firstMatched := -1
			for index, name := range symbolNames {
				_, upstream := code.Match(pass, patterns[name], expression)
				if upstream && firstMatched < 0 {
					firstMatched = index
				}
				port := staticcheck_code.IsSymbol(context, expression, name)
				compared.questions++
				if upstream != port {
					compared.mismatches = append(compared.mismatches, fmt.Sprintf(
						"%s about %q at %s: staticcheck answered %t, the port %t",
						kind, name, pass.Fset.Position(expression.Pos()), upstream, port))
				}
				compared.met[fmt.Sprintf("%s, %t", kind, upstream)]++
				compared.met[fmt.Sprintf("%s, %t", objectKind, upstream)]++
			}
			indexed := staticcheck_code.SymbolIndex(context, expression, symbolNames...)
			compared.questions++
			if indexed != firstMatched {
				compared.mismatches = append(compared.mismatches, fmt.Sprintf(
					"%s about every name at %s: staticcheck matched first index %d, SymbolIndex answered %d",
					kind, pass.Fset.Position(expression.Pos()), firstMatched, indexed))
			}
			compared.met[fmt.Sprintf("SymbolIndex, %t", firstMatched >= 0)]++
			return true
		})
	}
	return compared, nil
}

// resolvedKind is the kind of object the identifier or selector inside expression names, through parentheses and an
// instantiation as Symbol reads it, marked when it is an alias, or "unresolved" when there is none.
func resolvedKind(pass *analysis.Pass, expression ast.Expr) string {
	named := ast.Unparen(expression)
	switch instantiated := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := named.(type) {
	case *ast.Ident:
		object = pass.TypesInfo.ObjectOf(named)
	case *ast.SelectorExpr:
		object = pass.TypesInfo.ObjectOf(named.Sel)
	}
	if object == nil {
		return "unresolved"
	}
	if typeName, isTypeName := object.(*types.TypeName); isTypeName && typeName.IsAlias() {
		return "alias"
	}
	return reflect.TypeOf(object).Elem().Name()
}

// requiredSymbolLabels are what the fixture must meet for the comparison to prove each branch: each expression kind
// Symbol reads through, each kind of object it names, and SymbolIndex, finding a symbol and finding none.
func requiredSymbolLabels() []string {
	return []string{
		"Ident, true", "Ident, false", "SelectorExpr, true", "SelectorExpr, false", "ParenExpr, true",
		"ParenExpr, false", "IndexExpr, true", "IndexExpr, false", "IndexListExpr, true", "IndexListExpr, false",
		"Func, true", "Func, false", "Builtin, true", "Builtin, false", "TypeName, true", "TypeName, false",
		"alias, true", "alias, false", "Var, true", "Var, false", "Const, true", "Const, false",
		"SymbolIndex, true", "SymbolIndex, false",
	}
}
