package rules

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// directImports is why a function that asks whether a package imports another directly is covered.
const directImports = "its one caller hands it the judged package, whose direct imports are its own input"

// listedCrossPackageReads are the functions under rules that read another package's scope by name, or walk a
// package's imports, other than the judged package's own, outside the recorder (rule.Context.LookupIn and
// build_reads.Reads.AddLookup), each with why its read is covered all the same (#b2t9xmf). The key is the file,
// relative to rules, and the function, a method as (Type).Name.
var listedCrossPackageReads = map[string]string{
	"build_intermediate_representation/analyzer.go:NewPackage": "creates each direct import of the judged " +
		"package from its types; a rule reaches their members only through a call or use of the judged package, " +
		"which the type reads hold",
	"consistency_no_abbreviated_identifier/analyzer.go:reachOf": "walks the import closure; " +
		"fixedByAnotherPackage and isTakenInGo record each question they ask of what it gathers",
	"consistency_no_abbreviated_identifier/analyzer.go:declaredMethodsOf": "indexes the scope of a package " +
		"reachOf reached; fixedByAnotherPackage records each answer read off the index",
	"consistency_no_any/analyzer.go:buildForeignMethods": "walks the import closure; matchesForeignMethod " +
		"records each question it asks of what it gathers",
	"consistency_no_uppercase_initialism/foreign_interfaces.go:(*foreignInterfaces).build": "walks the import " +
		"closure; fix records each question it asks of what it gathers",
	"correctness_no_caller_data_mutation/analyzer.go:findIoReaderType": "walks the import closure; findIoReader " +
		"records what it found",
	"correctness_require_handled_error/analyzer.go:findHashType": "walks the import closure; findHashInterface " +
		"records what it found",
	"unused_code/graph_entry.go:(*graph).entry": "looks a //go:linkname name up in the judged package's own " +
		"scope",
	"vet/atomic/analyzer.go:imports": directImports,
	"vet/cgo_call/analyzer.go:importsPath": "asks the pass's own package, and a package importing runtime/cgo " +
		"is never restored",
	"vet/defers/analyzer.go:importsPackage": directImports,
	"vet/host_port/analyzer.go:isJudged":    directImports,
	"vet/http_response/analyzer.go:imports": directImports,
	"vet/lost_cancel/analyzer.go:imports":   directImports,
	"vet/signal_channel_analyzer/analyzer.go:importedPackage": directImports + "; the Notify looked up in what " +
		"it finds is recorded",
	"vet/testing_goroutine/functions.go:imports": directImports,
	"vet/wait_group/analyzer.go:imports":         directImports,
}

// crossPackageRead is one call that reads another package's scope or imports, where it is.
type crossPackageRead struct {
	key      string
	position token.Position
	call     string
}

// crossPackageReadsOf are the calls of file, named relative under rules, that read a package's scope by name,
// `X.Scope().Lookup(name)`, or walk its imports, `X.Imports()`, where X is not the judged package, nor a scope the
// type checker owns.
func crossPackageReadsOf(fileSet *token.FileSet, file *ast.File, relative string) []crossPackageRead {
	var found []crossPackageRead
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Body == nil {
			continue
		}
		key := relative + ":" + functionKey(function)
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, isCall := node.(*ast.CallExpr)
			if !isCall {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			switch selector.Sel.Name {
			case "Lookup":
				scopeCall, isScopeCall := selector.X.(*ast.CallExpr)
				if !isScopeCall || len(scopeCall.Args) != 0 {
					return true
				}
				scopeSelector, isScopeSelector := scopeCall.Fun.(*ast.SelectorExpr)
				if !isScopeSelector || scopeSelector.Sel.Name != "Scope" {
					return true
				}
				found = append(found, crossPackageRead{key: key, position: fileSet.Position(call.Pos()),
					call: expressionText(fileSet, scopeSelector.X) + ".Scope().Lookup"})
			case "Imports":
				if len(call.Args) == 0 {
					found = append(found, crossPackageRead{key: key, position: fileSet.Position(call.Pos()),
						call: expressionText(fileSet, selector.X) + ".Imports"})
				}
			}
			return true
		})
	}
	return found
}

// functionKey is a function's name, or a method's as (Type).Name or (*Type).Name.
func functionKey(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	pointer := ""
	if star, isStar := receiver.(*ast.StarExpr); isStar {
		pointer, receiver = "*", star.X
	}
	if indexed, isIndexed := receiver.(*ast.IndexExpr); isIndexed {
		receiver = indexed.X
	}
	name := "?"
	if identifier, isIdentifier := receiver.(*ast.Ident); isIdentifier {
		name = identifier.Name
	}
	return "(" + pointer + name + ")." + function.Name.Name
}

// expressionText is expression as gofmt writes it.
func expressionText(fileSet *token.FileSet, expression ast.Expr) string {
	var written strings.Builder
	if printError := printer.Fprint(&written, fileSet, expression); printError != nil {
		return "?"
	}
	return written.String()
}

// crossPackageReadProblems is each read of found in a function listed does not name, and each listed function with no
// such read left, so the list neither misses a new read nor keeps a stale entry.
func crossPackageReadProblems(found []crossPackageRead, listed map[string]string) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, read := range found {
		seen[read.key] = true
		if _, isListed := listed[read.key]; !isListed {
			problems = append(problems, fmt.Sprintf("%s: %s reads another package outside the recorder; record it "+
				"through rule.Context.LookupIn or build_reads.Reads.AddLookup, or list %s in listedCrossPackageReads "+
				"with why the type reads already hold it", read.position, read.call, read.key))
		}
	}
	for _, key := range slices.Sorted(maps.Keys(listed)) {
		if !seen[key] {
			problems = append(problems, fmt.Sprintf("listedCrossPackageReads names %s, which reads no other package "+
				"any more", key))
		}
	}
	return problems
}

// TestEveryCrossPackageReadOfARuleIsRecordedOrListed holds the rules to the recorder (#b2t9xmf): an extra build
// restores a package only while everything its rules read of other packages holds there, so a rule that looks a name
// up in another package's scope, or walks a package's imports past the judged package's own, records the question and
// its answer, or is listed here with why the type reads already hold it. Whatever this cannot see, a read that needs
// types to tell apart, the run rechecks. The check also runs over a planted file, to prove it can fail.
func TestEveryCrossPackageReadOfARuleIsRecordedOrListed(test *testing.T) {
	test.Parallel()

	var found []crossPackageRead
	fileSet := token.NewFileSet()
	walkError := filepath.WalkDir(".", func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() && entry.Name() == "testdata" {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseError := parser.ParseFile(fileSet, path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		found = append(found, crossPackageReadsOf(fileSet, file, filepath.ToSlash(path))...)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	for _, problem := range crossPackageReadProblems(found, listedCrossPackageReads) {
		test.Error(problem)
	}

	const planted = "package planted\n\nimport \"go/types\"\n\n" +
		"func own(context struct{ Package *types.Package }) {\n\t_ = context.Package.Scope().Lookup(\"Own\")\n" +
		"\t_ = context.Package.Imports()\n\t_ = types.Unsafe.Scope().Lookup(\"Pointer\")\n}\n\n" +
		"func foreign(declaring *types.Package) {\n\t_ = declaring.Scope().Lookup(\"Name\")\n}\n"
	plantedFile, parseError := parser.ParseFile(fileSet, "planted.go", planted, parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	problems := crossPackageReadProblems(crossPackageReadsOf(fileSet, plantedFile, "planted/planted.go"), nil)
	if len(problems) != 1 || !strings.Contains(problems[0], "planted.go:12:6: declaring.Scope().Lookup") {
		test.Errorf("want the planted foreign lookup alone found, got %q", problems)
	}
}
