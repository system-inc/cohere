package imports

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/parser"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
)

func firstImport(t *testing.T, sourceText string) *ast.Node {
	t.Helper()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{
		FileName: "/repository/source/Thing.tsx",
		Path:     tspath.Path("/repository/source/Thing.tsx"),
	}, sourceText, core.ScriptKindTSX)
	if file == nil || file.Statements == nil || len(file.Statements.Nodes) == 0 {
		t.Fatalf("no statement in %q", sourceText)
	}
	return file.Statements.Nodes[0]
}

// Three binding kinds live in three places in this AST rather than one flat list, and a reader that
// checks one field pins half the forms while looking complete. That mistake was made and corrected
// once in this tree already.
func TestBindingsOfSeesAllThreeKinds(t *testing.T) {
	cases := []struct {
		name          string
		sourceText    string
		wantDefault   bool
		wantNamespace bool
		wantNamed     int
	}{
		{"a default import", "import Thing from 'm';\n", true, false, 0},
		{"a namespace import", "import * as All from 'm';\n", false, true, 0},
		{"named imports", "import { a, b } from 'm';\n", false, false, 2},
		{"a default and a namespace together", "import Thing, * as All from 'm';\n", true, true, 0},
		{"a default and named together", "import Thing, { a } from 'm';\n", true, false, 1},
		{"a renamed named import", "import { a as b } from 'm';\n", false, false, 1},
		// A side-effect import binds nothing, and that is a real shape rather than an error.
		{"a side-effect import", "import 'm';\n", false, false, 0},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			bindings := BindingsOf(firstImport(t, testCase.sourceText))
			if (bindings.Default != nil) != testCase.wantDefault {
				t.Fatalf("default: want %v, got %v", testCase.wantDefault, bindings.Default != nil)
			}
			if (bindings.Namespace != nil) != testCase.wantNamespace {
				t.Fatalf("namespace: want %v, got %v", testCase.wantNamespace, bindings.Namespace != nil)
			}
			if len(bindings.Named) != testCase.wantNamed {
				t.Fatalf("named: want %d, got %d", testCase.wantNamed, len(bindings.Named))
			}
		})
	}
}

// The specifier is compared exactly, which is stricter than the upstream that motivated this and
// deliberately so: oxc never resolves the module at all and string-matches the element name, so it
// fires on any local component sharing that name.
func TestLocalNameOfDefaultImport(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
		specifier  string
		want       string
		wantFound  bool
	}{
		{"a matching default import", "import Head from 'next/head';\n", "next/head", "Head", true},
		{"an aliased default import", "import H from 'next/head';\n", "next/head", "H", true},
		{"a different module", "import Head from 'other/head';\n", "next/head", "", false},
		// The substring trap that has caught greps in this project repeatedly.
		{"a module whose name extends the specifier", "import H from 'next/headers';\n", "next/head", "", false},
		{"named rather than default", "import { Head } from 'next/head';\n", "next/head", "", false},
		{"a namespace import", "import * as Head from 'next/head';\n", "next/head", "", false},
		{"a side-effect import", "import 'next/head';\n", "next/head", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, found := LocalNameOfDefaultImport(firstImport(t, testCase.sourceText), testCase.specifier)
			if found != testCase.wantFound {
				t.Fatalf("found: want %v, got %v", testCase.wantFound, found)
			}
			if got != testCase.want {
				t.Fatalf("want %q, got %q", testCase.want, got)
			}
		})
	}
}

// A non-import node and a nil answer rather than panic, since a rule reaching a shared helper with a
// node it did not check is a bug in the rule and should not take the run down.
func TestBindingHelpersSurviveWrongInput(t *testing.T) {
	if bindings := BindingsOf(nil); bindings.Default != nil || bindings.Namespace != nil || bindings.Named != nil {
		t.Fatal("want nil to answer with empty bindings")
	}
	notAnImport := firstImport(t, "const value = 1;\n")
	if bindings := BindingsOf(notAnImport); bindings.Default != nil {
		t.Fatal("want a non-import to answer with empty bindings")
	}
	if _, found := LocalNameOfDefaultImport(nil, "next/head"); found {
		t.Fatal("want nil to report nothing found")
	}
}
