//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_report_test

import (
	"flag"
	"fmt"
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/report"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// rendering is what one render did: the text it printed, and what it panicked with, if it did.
type rendering struct {
	text     string
	panicked string
}

// The kit's Render prints every node of the rendered and shapes fixtures, expressions, statements, declarations and the
// nodes the printer refuses alike, as staticcheck's report.Render does, panicking where it panics, and the kit's
// RenderArguments prints every call's arguments as staticcheck's does. The test fails unless a call of no arguments,
// one of one and one of several, and an expression spread over lines, were met, so a fixture that stops holding one
// cannot pass by printing nothing.
func TestRenderMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"rendered", "shapes"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixtures: %v", loadError)
	}
	met := map[string]int{}
	rendered := 0
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
		// Render reads only the package's positions, so a pass of the loaded package's own stands in for a run.
		loadedPass := &analysis.Pass{
			Pkg:        loadedPackage.Types,
			TypesInfo:  loadedPackage.TypesInfo,
			TypesSizes: loadedPackage.TypesSizes,
			Fset:       loadedPackage.Fset,
			Files:      loadedPackage.Syntax,
			ResultOf:   map[*analysis.Analyzer]any{},
		}
		context := rule.NewContext("planted", loadedPass, nil, flag.NewFlagSet("planted", flag.ContinueOnError),
			func(string) bool { return false },
			func(string, analysis.Diagnostic) {
				// Render reports nothing, so the context's sink is never called.
			})
		upstreamPass := &analysis.Pass{Fset: loadedPackage.Fset}
		for _, file := range loadedPackage.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					return true
				}
				rendered++
				upstream := renderingOf(func() string { return report.Render(upstreamPass, node) })
				kit := renderingOf(func() string { return staticcheck_report.Render(context, node) })
				if upstream != kit {
					test.Errorf("Render of %T at %s: staticcheck %+v, the kit %+v", node,
						loadedPackage.Fset.Position(node.Pos()), upstream, kit)
				}
				if upstream.panicked != "" {
					met["a node the printer refuses"]++
				}
				if expression, isExpression := node.(ast.Expr); isExpression {
					start := loadedPackage.Fset.Position(expression.Pos())
					if end := loadedPackage.Fset.Position(expression.End()); end.Line > start.Line {
						met["an expression spread over lines"]++
					}
				}
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				switch len(call.Args) {
				case 0:
					met["a call of no arguments"]++
				case 1:
					met["a call of one argument"]++
				default:
					met["a call of several arguments"]++
				}
				upstreamArguments := report.RenderArgs(upstreamPass, call.Args)
				kitArguments := staticcheck_report.RenderArguments(context, call.Args)
				if upstreamArguments != kitArguments {
					test.Errorf("RenderArguments at %s: staticcheck %q, the kit %q",
						loadedPackage.Fset.Position(call.Pos()), upstreamArguments, kitArguments)
				}
				return true
			})
		}
	}
	test.Logf("%d nodes rendered by each side", rendered)
	for _, label := range []string{
		"a call of no arguments", "a call of one argument", "a call of several arguments",
		"an expression spread over lines", "a node the printer refuses",
	} {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixtures never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// renderingOf runs render and is what it printed or what it panicked with.
func renderingOf(render func() string) (result rendering) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = rendering{panicked: fmt.Sprint(recovered)}
		}
	}()
	return rendering{text: render()}
}
