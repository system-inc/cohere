package rule_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// declarationsProbe builds a program where one symbol is declared in two files: `Shared`, an interface
// two scripts each declare, which merges into one global symbol. It returns that symbol and both files,
// and the symbol of `parseInt`, whose declarations are all in the lib.
func declarationsProbe(t *testing.T) (shared *ast.Symbol, first *ast.SourceFile, second *ast.SourceFile, global *ast.Symbol) {
	t.Helper()
	directory := t.TempDir()
	files := map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "target": "ES2022", "lib": ["ES2022"], "types": []}, "include": ["*.ts"]}`,
		"first.ts":      "interface Shared { a: string }\ndeclare const fromFirst: Shared;\nparseInt('1');\n",
		"second.ts":     "interface Shared { b: string }\ndeclare const fromSecond: Shared;\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json"), CurrentDirectory: directory, SingleThreaded: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, sourceFile := range graph.ProjectFiles() {
		switch filepath.Base(sourceFile.FileName().AsString()) {
		case "first.ts":
			first = sourceFile
		case "second.ts":
			second = sourceFile
		}
	}
	if first == nil || second == nil {
		t.Fatal("the probe program does not hold both files")
	}
	checker, release := graph.CheckerForFile(context.Background(), first)
	defer release()
	statements := first.Statements.Nodes
	shared = checker.GetSymbolAtLocation(statements[0].Name())
	callee := statements[2].AsExpressionStatement().Expression.AsCallExpression().Expression
	global = checker.GetSymbolAtLocation(callee)
	if shared == nil || len(shared.Declarations) != 2 || global == nil {
		t.Fatalf("the probe did not produce a symbol merged across two files and a lib global (%v, %v)", shared, global)
	}
	return shared, first, second, global
}

// DeclarationsIn keeps exactly the declarations in the file it is handed, from either side of a merge,
// so a rule reading through it cannot reach another file's declaration.
func TestDeclarationsInKeepsOnlyTheFilesOwn(t *testing.T) {
	t.Parallel()
	shared, first, second, global := declarationsProbe(t)
	for _, sourceFile := range []*ast.SourceFile{first, second} {
		own := rule.DeclarationsIn(sourceFile, shared)
		if len(own) != 1 || ast.GetSourceFileOfNode(own[0]) != sourceFile {
			t.Errorf("%s: kept %d declarations, want its own one", filepath.Base(sourceFile.FileName().AsString()), len(own))
		}
	}
	if own := rule.DeclarationsIn(first, global); len(own) != 0 {
		t.Errorf("a lib global kept %d declarations in a source file, want none", len(own))
	}
	if rule.DeclarationsIn(first, nil) != nil || rule.DeclarationsIn(nil, shared) != nil {
		t.Error("a nil symbol or file gave declarations")
	}
}

// IsDeclaredOnlyInDeclarationFiles tells a lib global from a binding a source file declares.
func TestIsDeclaredOnlyInDeclarationFilesTellsALibGlobalFromASourceBinding(t *testing.T) {
	t.Parallel()
	shared, _, _, global := declarationsProbe(t)
	if !rule.IsDeclaredOnlyInDeclarationFiles(global) {
		t.Error("parseInt, declared only in the lib, was not recognised as a global")
	}
	if rule.IsDeclaredOnlyInDeclarationFiles(shared) {
		t.Error("an interface two source files declare was taken for a global")
	}
	if rule.IsDeclaredOnlyInDeclarationFiles(nil) {
		t.Error("a nil symbol was taken for a global")
	}
	if !rule.IsDeclaredInASourceFile(shared) {
		t.Error("an interface two source files declare was not recognised as declared in a source file")
	}
	if rule.IsDeclaredInASourceFile(global) || rule.IsDeclaredInASourceFile(nil) {
		t.Error("parseInt, or a nil symbol, was taken for something a source file declares")
	}
}

// The first declaration's file classification is all the reader may expose. Mixed symbols in both
// orders distinguish it from an any/all query and from ignoring the declaration-file check.
func TestIsFirstDeclaredInDeclarationFilePreservesOrderAndFileKind(t *testing.T) {
	t.Parallel()
	shared, first, _, global := declarationsProbe(t)
	source := rule.DeclarationsIn(first, shared)[0]
	library := global.Declarations[0]
	for _, tc := range []struct {
		name   string
		symbol *ast.Symbol
		want   bool
	}{
		{"nil", nil, false},
		{"empty", &ast.Symbol{}, false},
		{"missing file", &ast.Symbol{Declarations: []*ast.Node{nil}}, false},
		{"source", shared, false},
		{"library", global, true},
		{"library then source", &ast.Symbol{Declarations: []*ast.Node{library, source}}, true},
		{"source then library", &ast.Symbol{Declarations: []*ast.Node{source, library}}, false},
	} {
		if got := rule.IsFirstDeclaredInDeclarationFile(tc.symbol); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// IsDeclaredByADependency tells a type a dependency declares from one our source declares: a global from a
// declaration file, and a type an installed package ships as plain TypeScript under node_modules, against
// an interface a source file declares and one it merges into the global.
func TestIsDeclaredByADependencyTellsADependencysTypeFromOurs(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	files := map[string]string{
		"tsconfig.json":                 `{"compilerOptions": {"strict": true, "target": "ES2022", "lib": ["ES2022"], "types": []}, "include": ["*.ts"]}`,
		"routes.d.ts":                   "interface PageProps { route: string }\ninterface Merged { a: string }\n",
		"node_modules/kit/package.json": `{"name": "kit", "types": "index.ts"}`,
		"node_modules/kit/index.ts":     "export interface KitProps { a: string }\n",
		"subject.ts": "import type { KitProps } from 'kit';\ninterface Ours { a: string }\ndeclare global { interface Merged { b: string } }\n" +
			"export declare const page: PageProps;\nexport declare const kit: KitProps;\nexport declare const ours: Ours;\nexport declare const merged: Merged;\n",
	}
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	graph, err := program.Build(program.Options{ConfigFileName: filepath.Join(directory, "tsconfig.json"), CurrentDirectory: directory, SingleThreaded: true})
	if err != nil {
		t.Fatal(err)
	}
	var subject *ast.SourceFile
	for _, sourceFile := range graph.ProjectFiles() {
		if filepath.Base(sourceFile.FileName().AsString()) == "subject.ts" {
			subject = sourceFile
		}
	}
	if subject == nil {
		t.Fatal("the probe program does not hold the subject")
	}
	checker, release := graph.CheckerForFile(context.Background(), subject)
	defer release()

	// Each `declare const` names its type once, and the type reference's symbol is what a rule asks about.
	symbolOfTypeAt := func(statement int) *ast.Symbol {
		declaration := subject.Statements.Nodes[statement].AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
		symbol := checker.GetSymbolAtLocation(declaration.Type().AsTypeReferenceNode().TypeName)
		if symbol == nil {
			t.Fatalf("statement %d named no symbol", statement)
		}
		return symbol
	}
	page, kit, ours, merged := symbolOfTypeAt(3), checker.GetAliasedSymbol(symbolOfTypeAt(4)), symbolOfTypeAt(5), symbolOfTypeAt(6)

	if !rule.IsDeclaredByADependency(page) {
		t.Error("a global declared in a declaration file was not recognised as a dependency's")
	}
	if !rule.IsDeclaredByADependency(kit) {
		t.Error("a type a package ships as plain TypeScript under node_modules was not recognised as a dependency's")
	}
	if rule.IsDeclaredByADependency(ours) {
		t.Error("an interface a source file declares was taken for a dependency's")
	}
	if !rule.IsDeclaredByADependency(merged) {
		t.Error("a global our source merges into was not recognised as a dependency's")
	}
	if rule.IsDeclaredByADependency(nil) || rule.IsDependencyDeclaration(nil) {
		t.Error("a nil symbol or declaration was taken for a dependency's")
	}

	// The import specifier itself is in our source, so a caller has to follow the alias to ask the question.
	if rule.IsDeclaredByADependency(symbolOfTypeAt(4)) {
		t.Error("the import alias, declared in the subject, was taken for the package's type")
	}
}
