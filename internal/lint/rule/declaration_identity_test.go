package rule_test

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"testing"
)

func TestDeclarationIdentityReadersPreserveFirstAnyAndFile(t *testing.T) {
	t.Parallel()
	shared, home, foreignFile, _ := declarationsProbe(t)
	own := rule.DeclarationsIn(home, shared)[0]
	foreign := rule.DeclarationsIn(foreignFile, shared)[0]
	for _, tc := range []struct {
		name       string
		symbol     *ast.Symbol
		any, first bool
	}{
		{"nil", nil, false, false},
		{"empty", &ast.Symbol{}, false, false},
		{"foreign", &ast.Symbol{Declarations: []*ast.Node{foreign}}, false, false},
		{"own", &ast.Symbol{Declarations: []*ast.Node{own}}, true, true},
		{"foreign then own", &ast.Symbol{Declarations: []*ast.Node{foreign, own}}, true, false},
		{"own then foreign", &ast.Symbol{Declarations: []*ast.Node{own, foreign}}, true, true},
	} {
		set := map[*ast.Node]bool{own: true, foreign: false}
		got := [4]bool{rule.HasDeclaration(tc.symbol, own), rule.HasDeclarationInSet(tc.symbol, set), rule.FirstDeclarationInSet(tc.symbol, set), rule.IsDeclaredInFile(home, tc.symbol)}
		want := [4]bool{tc.any, tc.any, tc.first, tc.any}
		if got != want {
			t.Errorf("%s: got %v, want %v", tc.name, got, want)
		}
	}
	if rule.IsDeclaredInFile(home, &ast.Symbol{Declarations: []*ast.Node{nil}}) {
		t.Error("nil declaration counted as own")
	}
	if rule.FirstDeclarationInSet(shared, nil) || rule.HasDeclarationInSet(shared, nil) {
		t.Error("empty set matched")
	}
}
