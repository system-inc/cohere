package parity

import (
	"go/ast"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
)

// probing reports what a rule could read of each file and function: the package's imports and whether the file's
// scope holds strings, and for each function whether its body is there, whether anything in it is typed, and whether
// its signature is.
var probing = &analysis.Analyzer{
	Name: "probing",
	Doc:  "probing reports what the type check recorded of each file and function.",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			imports := []string{}
			for _, imported := range pass.Pkg.Imports() {
				imports = append(imports, imported.Path())
			}
			scope := pass.TypesInfo.Scopes[file]
			pass.Reportf(file.Name.Pos(), "imports %v, file scope holds strings %t", imports,
				scope != nil && scope.Lookup("strings") != nil)
			for _, declaration := range file.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction {
					continue
				}
				bodyTyped := false
				if function.Body != nil {
					ast.Inspect(function.Body, func(node ast.Node) bool {
						identifier, isIdentifier := node.(*ast.Ident)
						bodyTyped = bodyTyped || isIdentifier && pass.TypesInfo.Uses[identifier] != nil
						return true
					})
				}
				pass.Reportf(function.Name.Pos(), "%s: body %t, body typed %t, signature typed %t",
					function.Name.Name, function.Body != nil, bodyTyped,
					pass.TypesInfo.Defs[function.Name] != nil &&
						pass.TypesInfo.TypeOf(function.Type.Params.List[0].Type) != nil)
			}
		}
		return nil, nil
	},
}

// The plain run reads everything typed. The real strip leaves cut's body in the syntax with nothing in it typed, its
// signature typed and strings still imported and in the file's scope, though only cut's body uses it. Each wrong strip
// differs from it in exactly what it is wrong in: BodilessSyntax hands over cut without its body, and DroppingImports
// loses strings.
func TestStrippedFindingsStripOnlyWhatTheStripSays(test *testing.T) {
	test.Parallel()
	plainKept := "kept: body true, body typed true, signature typed true"
	plain := []string{
		"imports [strings], file scope holds strings true", plainKept,
		"cut: body true, body typed true, signature typed true",
	}
	cases := []struct {
		name     string
		strip    Strip
		stripped []string
	}{
		{"the real strip", Strip{Functions: []string{"cut"}}, []string{
			"imports [strings], file scope holds strings true", plainKept,
			"cut: body true, body typed false, signature typed true",
		}},
		{"bodiless syntax", Strip{Functions: []string{"cut"}, BodilessSyntax: true}, []string{
			"imports [strings], file scope holds strings true", plainKept,
			"cut: body false, body typed false, signature typed true",
		}},
		{"dropping imports", Strip{Functions: []string{"cut"}, DroppingImports: true}, []string{
			"imports [], file scope holds strings false", plainKept,
			"cut: body true, body typed false, signature typed true",
		}},
	}
	for _, stripCase := range cases {
		test.Run(stripCase.name, func(subtest *testing.T) {
			subtest.Parallel()
			plainFindings, strippedFindings := StrippedFindings(subtest, probing, analysistest.TestData(),
				stripCase.strip, "stripped")
			if messages := Messages(plainFindings); !reflect.DeepEqual(messages, plain) {
				subtest.Errorf("the plain run found %q, want %q", messages, plain)
			}
			if messages := Messages(strippedFindings); !reflect.DeepEqual(messages, stripCase.stripped) {
				subtest.Errorf("the stripped run found %q, want %q", messages, stripCase.stripped)
			}
		})
	}
}

// A strip naming no function with a body would compare two plain runs, so it fails.
func TestStrippedFindingsRefuseAStripOfNothing(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		StrippedFindings(recording, probing, analysistest.TestData(), Strip{Functions: []string{"absent"}},
			"stripped")
	}), "the strip names absent, and no function of that name has a body")
}
