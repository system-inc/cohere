package rule_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/types/program"
)

// declarationsProbe builds a program where one symbol is declared in two files: `Shared`, an interface
// two scripts each declare, which merges into one global symbol. It returns that symbol and both files,
// and the symbol of `parseInt`, whose declarations are all in the lib.
func declarationsProbe(
	test *testing.T,
) (shared *ast.Symbol, first *ast.SourceFile, second *ast.SourceFile, global *ast.Symbol) {
	test.Helper()
	directory := test.TempDir()
	files := map[string]string{
		"tsconfig.json": `{"compilerOptions": {"strict": true, "target": "ES2022", "lib": ["ES2022"], "types": []}, "include": ["*.ts"]}`,
		"first.ts":      "interface Shared { a: string }\ndeclare const fromFirst: Shared;\nparseInt('1');\n",
		"second.ts":     "interface Shared { b: string }\ndeclare const fromSecond: Shared;\n",
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(directory, "tsconfig.json"),
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
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
		test.Fatal("the probe program does not hold both files")
	}
	checker, release := graph.CheckerForFile(context.Background(), first)
	defer release()
	statements := first.Statements.Nodes
	shared = checker.GetSymbolAtLocation(statements[0].Name())
	callee := statements[2].AsExpressionStatement().Expression.AsCallExpression().Expression
	global = checker.GetSymbolAtLocation(callee)
	if shared == nil || len(shared.Declarations) != 2 || global == nil {
		test.Fatalf(
			"the probe did not produce a symbol merged across two files and a lib global (%v, %v)",
			shared,
			global,
		)
	}
	return shared, first, second, global
}

// DeclarationsIn keeps exactly the declarations in the file it is handed, from either side of a merge,
// so a rule reading through it cannot reach another file's declaration.
func TestDeclarationsInKeepsOnlyTheFilesOwn(test *testing.T) {
	test.Parallel()
	shared, first, second, global := declarationsProbe(test)
	for _, sourceFile := range []*ast.SourceFile{first, second} {
		own := rule.DeclarationsIn(sourceFile, shared)
		if len(own) != 1 || ast.GetSourceFileOfNode(own[0]) != sourceFile {
			test.Errorf(
				"%s: kept %d declarations, want its own one",
				filepath.Base(sourceFile.FileName().AsString()),
				len(own),
			)
		}
	}
	if own := rule.DeclarationsIn(first, global); len(own) != 0 {
		test.Errorf("a lib global kept %d declarations in a source file, want none", len(own))
	}
	if rule.DeclarationsIn(first, nil) != nil || rule.DeclarationsIn(nil, shared) != nil {
		test.Error("a nil symbol or file gave declarations")
	}
}

// IsDeclaredOnlyInDeclarationFiles tells a lib global from a binding a source file declares.
func TestIsDeclaredOnlyInDeclarationFilesTellsALibGlobalFromASourceBinding(test *testing.T) {
	test.Parallel()
	shared, _, _, global := declarationsProbe(test)
	if !rule.IsDeclaredOnlyInDeclarationFiles(global) {
		test.Error("parseInt, declared only in the lib, was not recognised as a global")
	}
	if rule.IsDeclaredOnlyInDeclarationFiles(shared) {
		test.Error("an interface two source files declare was taken for a global")
	}
	if rule.IsDeclaredOnlyInDeclarationFiles(nil) {
		test.Error("a nil symbol was taken for a global")
	}
	if !rule.IsDeclaredInASourceFile(shared) {
		test.Error("an interface two source files declare was not recognised as declared in a source file")
	}
	if rule.IsDeclaredInASourceFile(global) || rule.IsDeclaredInASourceFile(nil) {
		test.Error("parseInt, or a nil symbol, was taken for something a source file declares")
	}
}

// The first declaration's file classification is all the reader may expose. Mixed symbols in both
// orders distinguish it from an any/all query and from ignoring the declaration-file check.
func TestIsFirstDeclaredInDeclarationFilePreservesOrderAndFileKind(test *testing.T) {
	test.Parallel()
	shared, first, _, global := declarationsProbe(test)
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
			test.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// IsDeclaredByADependency tells a type a dependency declares from one our source declares: a global from a
// declaration file, and a type an installed package ships as plain TypeScript under node_modules, against
// an interface a source file declares and one it merges into the global.
func TestIsDeclaredByADependencyTellsADependencysTypeFromOurs(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	files := map[string]string{
		"tsconfig.json":                 `{"compilerOptions": {"strict": true, "target": "ES2022", "lib": ["ES2022"], "types": []}, "include": ["*.ts"]}`,
		"routes.d.ts":                   "interface PageProps { route: string }\ninterface Merged { a: string }\n",
		"node_modules/kit/package.json": `{"name": "kit", "types": "index.ts"}`,
		"node_modules/kit/index.ts":     "export interface KitProps { a: string }\n",
		"subject.ts": "import type { KitProps } from 'kit';\ninterface Ours { a: string }\n" +
			"declare global { interface Merged { b: string } }\n" +
			"export declare const page: PageProps;\nexport declare const kit: KitProps;\nexport declare const ours: Ours;\nexport declare const merged: Merged;\n",
	}
	for name, contents := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(directory, "tsconfig.json"),
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
	}
	var subject *ast.SourceFile
	for _, sourceFile := range graph.ProjectFiles() {
		if filepath.Base(sourceFile.FileName().AsString()) == "subject.ts" {
			subject = sourceFile
		}
	}
	if subject == nil {
		test.Fatal("the probe program does not hold the subject")
	}
	checker, release := graph.CheckerForFile(context.Background(), subject)
	defer release()

	// Each `declare const` names its type once, and the type reference's symbol is what a rule asks about.
	symbolOfTypeAt := func(statement int) *ast.Symbol {
		variableStatement := subject.Statements.Nodes[statement].AsVariableStatement()
		declaration := variableStatement.DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
		symbol := checker.GetSymbolAtLocation(declaration.Type().AsTypeReferenceNode().TypeName)
		if symbol == nil {
			test.Fatalf("statement %d named no symbol", statement)
		}
		return symbol
	}
	page, kit, ours, merged := symbolOfTypeAt(
		3,
	), checker.GetAliasedSymbol(symbolOfTypeAt(4)), symbolOfTypeAt(5), symbolOfTypeAt(6)

	if !rule.IsDeclaredByADependency(page) {
		test.Error("a global declared in a declaration file was not recognised as a dependency's")
	}
	if !rule.IsDeclaredByADependency(kit) {
		test.Error("a type a package ships as plain TypeScript under node_modules was not recognised as a dependency's")
	}
	if rule.IsDeclaredByADependency(ours) {
		test.Error("an interface a source file declares was taken for a dependency's")
	}
	if !rule.IsDeclaredByADependency(merged) {
		test.Error("a global our source merges into was not recognised as a dependency's")
	}
	if rule.IsDeclaredByADependency(nil) || rule.IsDependencyDeclaration(nil) {
		test.Error("a nil symbol or declaration was taken for a dependency's")
	}

	// The import specifier itself is in our source, so a caller has to follow the alias to ask the question.
	if rule.IsDeclaredByADependency(symbolOfTypeAt(4)) {
		test.Error("the import alias, declared in the subject, was taken for the package's type")
	}
}

// Mixed declarations in both orders pin all, any, empty and missing-file semantics independently.
func TestDeclarationMetadataQueriesPreserveAllAnyAndEmpty(test *testing.T) {
	test.Parallel()
	shared, first, _, global := declarationsProbe(test)
	source := rule.DeclarationsIn(first, shared)[0]
	library := global.Declarations[0]
	for _, tc := range []struct {
		name                                    string
		declarations                            []*ast.Node
		has, onlyAmbient, anySource, nonAmbient bool
	}{
		{"empty", nil, false, false, false, false},
		{"missing", []*ast.Node{nil}, true, false, true, false},
		{"library", []*ast.Node{library}, true, true, false, false},
		{"source", []*ast.Node{source}, true, false, true, true},
		{"library then source", []*ast.Node{library, source}, true, false, true, true},
		{"source then library", []*ast.Node{source, library}, true, false, true, true},
	} {
		symbol := &ast.Symbol{Declarations: tc.declarations}
		got := [4]bool{
			rule.HasDeclarations(symbol),
			rule.IsDeclaredOnlyInDeclarationFiles(symbol),
			rule.IsDeclaredInASourceFile(symbol),
			rule.IsDeclaredInANonDeclarationFile(symbol),
		}
		want := [4]bool{tc.has, tc.onlyAmbient, tc.anySource, tc.nonAmbient}
		if got != want {
			test.Errorf("%s: got %v, want %v", tc.name, got, want)
		}
	}
	if rule.HasDeclarations(nil) || rule.IsDeclaredInANonDeclarationFile(nil) {
		test.Error("nil symbol has declarations")
	}
}

// A real missing import yields the checker's unknown symbol, while a bare missing
// global may yield a declaration-free error symbol after diagnostics. Neither establishes ownership.
func TestIsResolvedSymbolRequiresADeclaration(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	files := map[string]string{
		"tsconfig.json": `{"compilerOptions":{"strict":true,"target":"ES2022","types":[]},"include":["*.ts"]}`,
		"subject.ts":    "import type { Missing } from './missing'; interface PageProps {} declare const page: PageProps; declare const absent: Absent; declare const imported: Missing; declare const untyped: any;",
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   filepath.Join(directory, "tsconfig.json"),
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
	}
	subject := graph.ProjectFiles()[0]
	if diagnostics := graph.Program.GetSemanticDiagnostics(context.Background(), subject); len(diagnostics) != 2 {
		test.Fatalf("got %d compiler diagnostics, want missing module and missing name", len(diagnostics))
	}
	checker, release := graph.CheckerForFile(context.Background(), subject)
	defer release()
	typeSymbol := func(index int) *ast.Symbol {
		variableStatement := subject.Statements.Nodes[index].AsVariableStatement()
		declaration := variableStatement.DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0]
		return checker.GetSymbolAtLocation(declaration.Type().AsTypeReferenceNode().TypeName)
	}
	own, absent, alias := typeSymbol(2), typeSymbol(3), typeSymbol(4)
	typeAt := func(index int) *ast.Node {
		variableStatement := subject.Statements.Nodes[index].AsVariableStatement()
		return variableStatement.DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].Type()
	}
	if !rule.IsUnresolvedType(checker.GetTypeAtLocation(typeAt(3))) {
		test.Error("missing global lost its unresolved type")
	}
	if rule.IsUnresolvedType(checker.GetTypeAtLocation(typeAt(2))) ||
		rule.IsUnresolvedType(checker.GetTypeAtLocation(typeAt(5))) ||
		rule.IsUnresolvedType(nil) {
		test.Error("own type, explicit any, or no context was classified as unresolved")
	}
	unknown := checker.GetAliasedSymbol(alias)
	if rule.HasDeclarations(absent) || alias == nil || !checker.IsUnknownSymbol(unknown) {
		test.Fatal("fixture did not produce a declaration-free missing name and an unknown alias target")
	}
	for _, tc := range []struct {
		name   string
		symbol *ast.Symbol
		want   bool
	}{
		{"nil", nil, false}, {"without declarations", &ast.Symbol{}, false}, {"missing global", absent, false},
		{"unknown", unknown, false}, {"unresolved alias", alias, false}, {"own", own, true},
	} {
		// Not parallel: the checker lease belongs to the parent and must remain live.
		test.Run(tc.name, func(test *testing.T) {
			if got := rule.IsResolvedSymbol(checker, tc.symbol); got != tc.want {
				test.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if rule.IsResolvedSymbol(nil, own) {
		test.Fatal("a missing checker established ownership")
	}
}

// IsDeclaredInPackage names a file's package from the last node_modules on its path, whole and scoped, so
// a pnpm store's copy of a package is that package and a package whose name only starts the same is not.
// Each probe file declares one global, and a symbol is built over the declarations a case names, in both
// orders where it holds two.
func TestIsDeclaredInPackageReadsTheFilesPackage(test *testing.T) {
	test.Parallel()
	const typesReact = "node_modules/@types/react/index.d.ts"
	const pnpmCopy = "node_modules/.pnpm/@types+react@19.3.0/node_modules/@types/react/global.d.ts"
	const nestedCopy = "node_modules/kit/node_modules/@types/react/index.d.ts"
	const typesReactDom = "node_modules/@types/react-dom/index.d.ts"
	const fiber = "node_modules/@react-three/fiber/three-types.d.ts"
	const react = "node_modules/react/index.d.ts"
	const ours = "subject.ts"
	directory := test.TempDir()
	files := []string{typesReact, pnpmCopy, nestedCopy, typesReactDom, fiber, react, ours}
	quoted := make([]string, 0, len(files))
	for index, name := range files {
		path := filepath.Join(directory, name)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		contents := "declare const declaredIn" + strconv.Itoa(index) + ": number;\n"
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		quoted = append(quoted, strconv.Quote(name))
	}
	// Listed by name, since an include pattern skips node_modules.
	configuration := `{"compilerOptions": {"strict": true, "target": "ES2022", "lib": ["ES2022"], "types": []}, ` +
		`"files": [` + strings.Join(quoted, ", ") + `]}`
	configurationPath := filepath.Join(directory, "tsconfig.json")
	if writeError := os.WriteFile(configurationPath, []byte(configuration), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   configurationPath,
		CurrentDirectory: directory,
		SingleThreaded:   true,
	})
	if buildError != nil {
		test.Fatal(buildError)
	}
	declarationIn := map[string]*ast.Node{}
	for _, sourceFile := range graph.Program.SourceFiles() {
		relative, relativeError := filepath.Rel(directory, sourceFile.FileName().AsString())
		if relativeError == nil && slices.Contains(files, filepath.ToSlash(relative)) {
			declarationIn[filepath.ToSlash(relative)] = sourceFile.Statements.Nodes[0]
		}
	}
	if len(declarationIn) != len(files) {
		test.Fatalf("the probe program holds %d of the %d files", len(declarationIn), len(files))
	}

	for _, testCase := range []struct {
		name  string
		files []string
		want  bool
	}{
		{"the package's own directory", []string{typesReact}, true},
		{"a pnpm store's copy", []string{pnpmCopy}, true},
		{"a copy nested under another package", []string{nestedCopy}, true},
		{"a package whose name starts the same", []string{typesReactDom}, false},
		{"another scoped package", []string{fiber}, false},
		{"the unscoped package of the same name", []string{react}, false},
		{"our own source", []string{ours}, false},
		{"merged, the package's first", []string{typesReact, fiber}, true},
		{"merged, the package's last", []string{fiber, typesReact}, true},
		{"no declarations", nil, false},
	} {
		symbol := &ast.Symbol{}
		for _, name := range testCase.files {
			symbol.Declarations = append(symbol.Declarations, declarationIn[name])
		}
		if got := rule.IsDeclaredInPackage(symbol, "@types/react"); got != testCase.want {
			test.Errorf("%s: got %v, want %v", testCase.name, got, testCase.want)
		}
	}
	if !rule.IsDeclaredInPackage(&ast.Symbol{Declarations: []*ast.Node{declarationIn[fiber]}}, "@react-three/fiber") {
		test.Error("a scoped package other than @types/react was not named by its two segments")
	}
	if rule.IsDeclaredInPackage(nil, "@types/react") {
		test.Error("a nil symbol was declared in a package")
	}
}
