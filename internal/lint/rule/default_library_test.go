package rule_test

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/lint/rule"
	"testing"
)

// Embedding fails unexpected Program reads rather than supplying permissive defaults.
type metadataLibraryProgram struct {
	rule.Program
	library tspath.PathKey
}

func (p metadataLibraryProgram) IsSourceFileDefaultLibrary(path tspath.PathKey) bool {
	return path == p.library
}
func (p metadataLibraryProgram) Options() *core.CompilerOptions {
	return &core.CompilerOptions{Lib: []string{}}
}
func (p metadataLibraryProgram) UseCaseSensitiveFileNames() bool { return true }

func TestDefaultLibraryReadersPreserveAnyAndVacuousAll(t *testing.T) {
	t.Parallel()
	shared, first, _, global := declarationsProbe(t)
	source := rule.DeclarationsIn(first, shared)[0]
	library := global.Declarations[0]
	program := metadataLibraryProgram{library: ast.GetSourceFileOfNode(library).PathKey()}
	for _, tc := range []struct {
		name     string
		symbol   *ast.Symbol
		any, all bool
	}{
		{"nil", nil, false, true},
		{"empty", &ast.Symbol{}, false, true},
		{"missing file", &ast.Symbol{Declarations: []*ast.Node{nil}}, false, true},
		{"source", shared, false, false},
		{"library", global, true, true},
		{"library then source", &ast.Symbol{Declarations: []*ast.Node{library, source}}, true, false},
		{"source then library", &ast.Symbol{Declarations: []*ast.Node{source, library}}, true, false},
	} {
		// The any reader historically requires a file for every declaration it reaches; do not widen
		// its input contract while testing the all reader's deliberate missing-file exemption.
		if tc.name != "missing file" {
			if got := rule.IsSymbolFromDefaultLibrary(program, tc.symbol); got != tc.any {
				t.Errorf("%s any: got %v, want %v", tc.name, got, tc.any)
			}
		}
		if got := rule.IsDeclaredOnlyInDefaultLibraries(program, tc.symbol); got != tc.all {
			t.Errorf("%s all: got %v, want %v", tc.name, got, tc.all)
		}
	}
	// An arbitrary declaration file is neither the default library nor an installed dependency test.
	first.IsDeclarationFile = true
	if rule.IsSymbolFromDefaultLibrary(program, shared) || rule.IsDeclaredOnlyInDefaultLibraries(program, shared) {
		t.Error("non-library declaration file accepted")
	}
}
