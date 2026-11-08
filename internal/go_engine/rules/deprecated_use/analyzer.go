// Package deprecated_use is staticcheck's SA1019, a use of a deprecated function, variable, constant, type, field or
// package of another package, ported as a check on syntax that reports through the kit every port of staticcheck
// shares.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1019/sa1019.go, whose logic it keeps and
// whose messages it reports word for word, where staticcheck's report puts them: over a selector, or over its name
// alone when it selects from a type, over a struct literal's key, and over an import's path. It reads what is
// deprecated from staticcheck's deprecated facts, which cross from every dependency, and which file a generator made
// from staticcheck's generated pass, keyed by the name a file was parsed from, both straight from the pass, as a check
// on the engine's dispatcher will read them from its ResultOf. It reads the standard library's deprecations, with the
// Go version each came in, from honnef.co/go/tools/knowledge, a table of data rather than framework, allowed in this
// wave and to be ported with the rest of knowledge later. It names a selector with staticcheck_code.SelectorName, asks
// staticcheck_report.StandardLibraryVersion where the original asks code.StdlibVersion, and reports through
// staticcheck_report.Report, each the port of the one the original calls, proven against it beside it.
//
// The original keeps state across its walk of every node of the package: the function declaration it is in, whose
// own deprecation lets it use deprecated objects, is cleared only at a file's start and replaced only at the next
// declaration, so a package-level variable after a deprecated function in its file is judged as if inside it. The port
// keeps the same state in the same walk, so that false negative is the original's and stays (parity's
// afterDeprecatedFunction shows it). Like the original, it misses dot-imported objects, and leaves alone a deprecated
// object of the standard library its knowledge does not date. The registry still runs the original, which stays the
// oracle: the parity tests beside this file run both over the same modules, and the two differ nowhere they hold.
package deprecated_use

import (
	"fmt"
	"go/ast"
	"go/types"
	"go/version"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
	"honnef.co/go/tools/analysis/facts/deprecated"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/knowledge"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place. It requires staticcheck's
// deprecated pass, whose facts say what is deprecated, and its generated pass, which says which generator made a file.
var Analyzer = &analysis.Analyzer{
	Name: "SA1019",
	Doc: "Flags a use of a deprecated function, variable, constant, field or package: staticcheck's SA1019, ported " +
		"as a check on syntax.",
	Requires: []*analysis.Analyzer{inspect.Analyzer, deprecated.Analyzer, generated.Analyzer},
	Run:      run,
}

// protobufPath is the deprecated protobuf package that code protoc-gen-go generated may import, which is left alone
// there, since the code cannot choose.
const protobufPath = "github.com/golang/protobuf/proto"

// run builds the pass's context, reads the deprecated facts and the generated files from the pass, and judges the
// package.
func run(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	deprecations, isDeprecations := pass.ResultOf[deprecated.Analyzer].(deprecated.Result)
	generators, isGenerators := pass.ResultOf[generated.Analyzer].(map[string]generated.Generator)
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isDeprecations || !isGenerators || !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("deprecated_use: a required pass's result is missing")
	}
	Check(context, deprecations, generators, inspected)
	return nil, nil
}

// Check judges every selector, struct literal and import of the package: each that reaches a deprecated object or
// package of another package, but the package's own tests, is reported, unless the function declaration it is in is
// itself deprecated, or the standard library's deprecation came after the version the code targets.
func Check(
	context *rule.Context,
	deprecations deprecated.Result,
	generators map[string]generated.Generator,
	inspected *inspector.Inspector,
) {
	// The function declaration the walk is in, or was last in, since the file began; nil before a file's first.
	var enclosing types.Object
	depth := 0
	inspected.Nodes(nil, func(node ast.Node, push bool) bool {
		if !push {
			depth--
			return false
		}
		depth++
		// The walk's root is each file, where the enclosing declaration is cleared.
		if depth == 1 {
			enclosing = nil
		}
		if declaration, isDeclaration := node.(*ast.FuncDecl); isDeclaration {
			enclosing = context.TypesInfo.ObjectOf(declaration.Name)
		}
		switch node := node.(type) {
		case *ast.SelectorExpr:
			checkSelector(context, deprecations, node, enclosing)
		case *ast.CompositeLit:
			checkStructKeys(context, deprecations, node, enclosing)
		}
		return true
	})
	inspected.Preorder([]ast.Node{(*ast.ImportSpec)(nil)}, func(node ast.Node) {
		specification, isImport := node.(*ast.ImportSpec)
		if !isImport {
			// Invariant: Preorder hands this function only the imports it was asked for.
			panic("deprecated_use: the inspector handed over a node that is not an import")
		}
		checkImport(context, deprecations, generators, specification)
	})
}

// checkSelector judges what selector selects: a deprecated object of another package, one that is not the package's
// own tests', is reported over the selector, or over its name alone when it selects from a type.
func checkSelector(context *rule.Context, deprecations deprecated.Result, selector *ast.SelectorExpr,
	enclosing types.Object) {
	object := context.TypesInfo.ObjectOf(selector.Sel)
	// A method of an instantiated generic type is deprecated as its generic origin is.
	if function, isFunction := object.(*types.Func); isFunction {
		object = function.Origin()
	}
	// A package may use its own deprecated objects, and the universe has none.
	if object.Pkg() == nil || object.Pkg() == context.Package {
		return
	}
	if isOwnTests(context.Package.Path(), object.Pkg().Path()) {
		return
	}
	reported := ast.Node(selector)
	if context.TypesInfo.Types[selector.X].IsType() {
		reported = selector.Sel
	}
	if deprecation, isDeprecated := deprecations.Objects[object]; isDeprecated {
		judge(context, deprecations, deprecation, reported, staticcheck_code.SelectorName(context, selector),
			object.Pkg().Path(), enclosing)
	}
}

// checkStructKeys judges each key of a struct literal as a selector of the literal's type, `T{Field: v}` as
// `T.Field`. A literal of any other type, a map's or a slice's, is left alone, and the keys stop at the first element
// that is not a key and value.
func checkStructKeys(context *rule.Context, deprecations deprecated.Result, literal *ast.CompositeLit,
	enclosing types.Object) {
	literalType := context.TypesInfo.Types[literal.Type]
	if !literalType.IsType() {
		return
	}
	if _, isStruct := literalType.Type.Underlying().(*types.Struct); !isStruct {
		return
	}
	for _, element := range literal.Elts {
		pair, isPair := element.(*ast.KeyValueExpr)
		if !isPair {
			return
		}
		key, isName := pair.Key.(*ast.Ident)
		if !isName {
			return
		}
		checkSelector(context, deprecations, &ast.SelectorExpr{X: literal.Type, Sel: key}, enclosing)
	}
}

// checkImport judges one import: a deprecated package is reported over its path, unless it is the package's own
// under test, or protobuf's deprecated package imported by code protoc-gen-go generated.
func checkImport(
	context *rule.Context,
	deprecations deprecated.Result,
	generators map[string]generated.Generator,
	specification *ast.ImportSpec,
) {
	var name types.Object
	if specification.Name != nil {
		name = context.TypesInfo.ObjectOf(specification.Name)
	} else {
		name = context.TypesInfo.Implicits[specification]
	}
	packageName, isPackageName := name.(*types.PkgName)
	if !isPackageName {
		// Invariant: the type checker records every import of a package that type-checks as a package name, and
		// staticcheck's assertion panics on one it did not.
		panic(fmt.Sprintf("deprecated_use: the import of %s has no package name", specification.Path.Value))
	}
	quoted := specification.Path.Value
	path := quoted[1 : len(quoted)-1]
	deprecation, isDeprecated := deprecations.Packages[packageName.Imported()]
	if !isDeprecated {
		return
	}
	if path == protobufPath {
		parsed := context.Fset.PositionFor(specification.Path.Pos(), false)
		// A file no generator made reads as Unknown, which is not protoc-gen-go.
		if generators[parsed.Filename] == generated.ProtocGenGo {
			return
		}
	}
	if isOwnTests(context.Package.Path(), path) {
		return
	}
	judge(context, deprecations, deprecation, specification.Path, path, path, nil)
}

// isOwnTests is whether the package at packagePath may use what the package at usedPath declares as its own: foo's
// external tests, foo_test, use foo, and foo.test, the main package `go test` generates, uses foo and foo_test.
func isOwnTests(packagePath string, usedPath string) bool {
	return strings.TrimSuffix(packagePath, "_test") == usedPath ||
		strings.TrimSuffix(packagePath, ".test") == usedPath ||
		strings.TrimSuffix(packagePath, ".test") == strings.TrimSuffix(usedPath, "_test")
}

// judge reports the use at node of name, deprecated as deprecation, of the package at packagePath, unless enclosing,
// the function declaration it is in, is deprecated itself, or the standard library deprecated it after the version
// the code targets, or deprecated it without knowledge to date it.
func judge(
	context *rule.Context,
	deprecations deprecated.Result,
	deprecation *deprecated.IsDeprecated,
	node ast.Node,
	name string,
	packagePath string,
	enclosing types.Object,
) {
	standard, isKnown := knowledge.StdlibDeprecations[name]
	// The standard library has no dot in its paths, the best guess there is, since nothing names its packages. An
	// undated deprecation there may have come after the version the code targets, so it is left alone.
	if !isKnown && !strings.Contains(packagePath, ".") {
		return
	}
	// Only the version that deprecated the object is compared, not when its alternative came, so a finding does not
	// hang on the Go the check runs under.
	if isKnown &&
		version.Compare(staticcheck_report.StandardLibraryVersion(context, node), standard.DeprecatedSince) == -1 {
		return
	}
	// A deprecated function may use deprecated objects.
	if enclosing != nil {
		if _, isDeprecated := deprecations.Objects[enclosing]; isDeprecated {
			return
		}
	}
	if !isKnown {
		staticcheck_report.Report(context, node, fmt.Sprintf("%s is deprecated: %s", name, deprecation.Msg))
		return
	}
	switch standard.AlternativeAvailableSince {
	case knowledge.DeprecatedNeverUse:
		staticcheck_report.Report(context, node, fmt.Sprintf(
			"%s has been deprecated since %s because it shouldn't be used: %s",
			name, goVersion(standard.DeprecatedSince), deprecation.Msg))
	case standard.DeprecatedSince, knowledge.DeprecatedUseNoLonger:
		staticcheck_report.Report(context, node, fmt.Sprintf("%s has been deprecated since %s: %s",
			name, goVersion(standard.DeprecatedSince), deprecation.Msg))
	default:
		staticcheck_report.Report(context, node, fmt.Sprintf(
			"%s has been deprecated since %s and an alternative has been available since %s: %s",
			name, goVersion(standard.DeprecatedSince), goVersion(standard.AlternativeAvailableSince), deprecation.Msg))
	}
}

// goVersion is version, such as "go1.6", as the messages write it, "Go 1.6".
func goVersion(version string) string {
	return "Go " + strings.TrimPrefix(version, "go")
}
