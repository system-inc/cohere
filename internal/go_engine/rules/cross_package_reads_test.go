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

// The classes of a listed read: why a read of a scope or of imports outside the recorder is covered.
const (
	// readRecorded is a walk of the import closure whose answers the rule records where it uses them.
	readRecorded = "recorded"
	// readOwnPackage is a look into the judged package's own scope, an own input.
	readOwnPackage = "own package"
	// readDirectImports asks the judged package's direct imports, whose import map is an own input.
	readDirectImports = "direct imports only (the import map is an own input)"
	// readUnsafe is a look into unsafe's scope, which the type checker owns and no build changes.
	readUnsafe = "unsafe, the same in every build"
)

// listedRead is why one function's reads of a scope or of imports are covered: its class, and what the class leaves
// to say.
type listedRead struct {
	class  string
	reason string
}

// listedCrossPackageReads are the functions under rules that call `X.Scope().Lookup(name)` or `X.Imports()` outside
// the recorder (rule.Context.LookupIn and build_reads.Reads.AddLookup), each with why its read is covered
// (#b2t9xmf). The key is the file, relative to rules, and the function, a method as (Type).Name.
var listedCrossPackageReads = map[string]listedRead{
	"build_intermediate_representation/analyzer.go:NewPackage": {
		class: readDirectImports,
		reason: "creates each direct import from its types, its members reached only through a call or use of " +
			"the judged package, which the type reads hold",
	},
	"concurrency_require_escape_hatch_reason/analyzer.go:enterFile": {class: readUnsafe},
	"consistency_no_abbreviated_identifier/analyzer.go:declaredMethodsOf": {
		class:  readRecorded,
		reason: "fixedByAnotherPackage records each answer read off the index",
	},
	"consistency_no_abbreviated_identifier/analyzer.go:reachOf": {
		class:  readRecorded,
		reason: "fixedByAnotherPackage and isTakenInGo record each question they ask of the closure",
	},
	"consistency_no_any/analyzer.go:buildForeignMethods": {
		class:  readRecorded,
		reason: "matchesForeignMethod records each question it asks of the closure",
	},
	"consistency_no_screaming_snake_case/analyzer.go:isMemberOf": {
		class:  readDirectImports,
		reason: "and the tested package found among them is looked into through LookupIn",
	},
	"consistency_no_uppercase_initialism/analyzer.go:directedObjects": {
		class:  readOwnPackage,
		reason: "a //go:linkname name looked up in the judged package",
	},
	"consistency_no_uppercase_initialism/foreign_interfaces.go:(*foreignInterfaces).build": {
		class:  readRecorded,
		reason: "fix records each question it asks of the closure",
	},
	"correctness_no_caller_data_mutation/analyzer.go:findIoReader": {
		class:  readRecorded,
		reason: "hands the judged package's imports to build_reads.ClosureInterface and records what it found",
	},
	"staticcheck_code/is_main_like.go:IsMainLike": {class: readDirectImports},
	"unused_code/graph_entry.go:(*graph).entry": {
		class:  readOwnPackage,
		reason: "a //go:linkname name looked up in the judged package",
	},
	"unused_write/analyzer.go:Check":        {class: readDirectImports},
	"vet/atomic/analyzer.go:imports":        {class: readDirectImports},
	"vet/defers/analyzer.go:importsPackage": {class: readDirectImports},
	"vet/host_port/analyzer.go:isJudged":    {class: readDirectImports},
	"vet/http_response/analyzer.go:imports": {class: readDirectImports},
	"vet/lost_cancel/analyzer.go:imports":   {class: readDirectImports},
	"vet/signal_channel_analyzer/analyzer.go:importedPackage": {
		class:  readDirectImports,
		reason: "and Notify is looked up in what it finds through LookupIn",
	},
	"vet/testing_goroutine/functions.go:imports": {class: readDirectImports},
	"vet/tests/analyzer.go:lookupObjects": {
		class:  readDirectImports,
		reason: "its own scope first, then each direct import looked into through LookupIn",
	},
	"vet/wait_group/analyzer.go:imports": {class: readDirectImports},
}

// crossPackageRead is one call that reads another package's scope or imports, where it is.
type crossPackageRead struct {
	key      string
	position token.Position
	call     string
}

// crossPackageReadsOf are the calls of file, named relative under rules, that read a package's scope by name,
// `X.Scope().Lookup(name)`, or walk its imports, `X.Imports()`, whatever X is: telling the judged package from another
// takes types, so every call is listed with its class instead.
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

// crossPackageReadProblems is each read of found in a function listed does not name, each listed function of a class
// that is none of the four, and each listed function with no such read left, so the list neither misses a new read
// nor keeps a stale entry.
func crossPackageReadProblems(found []crossPackageRead, listed map[string]listedRead) []string {
	problems := []string{}
	seen := map[string]bool{}
	for _, read := range found {
		seen[read.key] = true
		if _, isListed := listed[read.key]; !isListed {
			problems = append(problems, fmt.Sprintf("%s: %s reads another package outside the recorder; record it "+
				"through rule.Context.LookupIn or build_reads.Reads.AddLookup, or list %s in listedCrossPackageReads "+
				"with its class", read.position, read.call, read.key))
		}
	}
	classes := []string{readRecorded, readOwnPackage, readDirectImports, readUnsafe}
	for _, key := range slices.Sorted(maps.Keys(listed)) {
		if !slices.Contains(classes, listed[key].class) {
			problems = append(problems, fmt.Sprintf("listedCrossPackageReads gives %s the class %q, which is none of "+
				"the four", key, listed[key].class))
		}
		if !seen[key] {
			problems = append(problems, fmt.Sprintf("listedCrossPackageReads names %s, which makes no such call any "+
				"more", key))
		}
	}
	return problems
}

// TestEveryCrossPackageReadOfARuleIsRecordedOrListed holds the rules to the recorder (#b2t9xmf): an extra build
// restores a package only while everything its rules read of other packages holds there, so every call under rules
// that looks a name up in a package's scope, or walks a package's imports, goes through the recorder or is listed with
// its class: recorded where its answers are used, the judged package's own, its direct imports only, or unsafe's.
// Method-set and field lookups through an expression's type are not guarded here: the restore's rendering of each
// recorded object's type, methods and fields included, covers them. The check also runs over a planted file, to prove
// it can fail.
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
	// The own function listed, as the registry's own are, leaves the foreign lookup the one read unaccounted for.
	plantedListed := map[string]listedRead{"planted/planted.go:own": {class: readOwnPackage}}
	problems := crossPackageReadProblems(crossPackageReadsOf(fileSet, plantedFile, "planted/planted.go"),
		plantedListed)
	if len(problems) != 1 || !strings.Contains(problems[0], "planted.go:12:6: declaring.Scope().Lookup") {
		test.Errorf("want the planted foreign lookup alone found, got %q", problems)
	}
}
