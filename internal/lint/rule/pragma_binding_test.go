package rule

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"testing"
)

func pragmaParse(t *testing.T, name, text string) *ast.SourceFile {
	t.Helper()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: tspath.RootedFilePath(name), PathKey: tspath.PathKey(name)}, text, core.ScriptKindTS)
	// Standalone parser fixtures need the same parent links the bound program supplies.
	var parents func(*ast.Node)
	parents = func(node *ast.Node) {
		node.ForEachChild(func(child *ast.Node) bool { child.Parent = node; parents(child); return false })
	}
	parents(file.AsNode())
	return file
}

func TestPragmaBindingRejectsWrongFileIdentifier(t *testing.T) {
	t.Parallel()
	home := pragmaParse(t, "/home.ts", "const local = 1;")
	foreign := pragmaParse(t, "/foreign.ts", "const createElement = React.createElement;")
	identifier := foreign.Statements.Nodes[0].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Name()
	defer func() {
		if recover() == nil {
			t.Error("wrong-file identifier accepted before checker lookup")
		}
	}()
	IsReactPragmaBinding(home, nil, NewFileCache(), identifier)
}

func TestPragmaBindingChecksEveryForeignHeaderAgainstCuts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text string
		inside     bool
	}{
		{"module", "const createElement = React.createElement;", false},
		{"block var", "if (true) { var createElement = React.createElement; }", false},
		{"namespace", "namespace Shared { export const createElement = React.createElement; }", false},
		{"function", "function f() { const createElement = React.createElement; }", true},
		{"method", "class C { f() { const createElement = React.createElement; } }", true},
		{"getter", "class C { get f() { const createElement = React.createElement; return 1; } }", true},
		{"setter", "class C { set f(x: unknown) { const createElement = React.createElement; } }", true},
		{"constructor", "class C { constructor() { const createElement = React.createElement; } }", true},
		{"arrow", "const f = () => { const createElement = React.createElement; };", true},
		{"expression", "const f = function() { const createElement = React.createElement; };", true},
		{"static block", "class C { static { const createElement = React.createElement; } }", false},
		{"nested default arrow", "function f(x = () => { const createElement = React.createElement; }) {}", true},
		{"parameter header", "function f({createElement} = React) {}", false},
		{"top level arrow initializer", "const createElement = () => React.createElement;", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			home := pragmaParse(t, "/home.ts", "export {};")
			file := pragmaParse(t, "/foreign.ts", tc.text)
			var target *ast.Node
			var walk func(*ast.Node)
			walk = func(node *ast.Node) {
				if (node.Kind == ast.KindVariableDeclaration || node.Kind == ast.KindBindingElement) && node.Name() != nil && node.Name().Kind == ast.KindIdentifier && node.Name().Text() == "createElement" {
					target = node
				}
				node.ForEachChild(func(child *ast.Node) bool { walk(child); return false })
			}
			walk(file.AsNode())
			if target == nil {
				t.Fatal("fixture has no target declaration")
			}

			reader := pragmaBindingReader{home: home, coverage: map[*ast.Node]pragmaCoverage{}}
			panicked := false
			func() {
				defer func() {
					if recover() != nil {
						panicked = true
					}
				}()
				reader.declaration(target)
			}()
			if panicked != tc.inside {
				t.Errorf("foreign panic %v, want %v", panicked, tc.inside)
			}
			// The same syntax in the home file remains legal, including local function bindings.
			reader.home = file
			if !reader.declaration(target) && tc.name != "top level arrow initializer" && tc.name != "parameter header" {
				t.Error("own-file pragma finding dropped")
			}
		})
	}
}
