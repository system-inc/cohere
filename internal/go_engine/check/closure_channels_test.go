package check

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// engineImportPath is the import path of the go_engine directory, by which a file's imports name the packages below it.
const engineImportPath = "github.com/system-inc/cohere/internal/go_engine/"

// resolverAskers are the packages a rule's run reaches that take the run's fact resolver from a pass, by rule.Resolved
// or fact_resolver.Of. The resolver holds every package of the run, so a question through it could read a package
// outside the asker's import closure; each of these asks only through fact_resolver.Visible, or shares a root's
// representation the resolver builds the same way whoever asks first (#23b2grt).
var resolverAskers = []string{
	"rules/build_intermediate_representation",
	"rules/function_purity",
	"rules/odd_slice_argument",
	"rules/return_nil_ness",
}

// factLearners are the analyzers that declare FactTypes among every rule and everything it requires, by name. The
// checker carries a fact only up the imports (inheritFacts), so a fact is bounded by the import closure by
// construction; the list is here so a new learner is read before its facts join what the walk restores. ctrlflow's
// facts are which functions never return, fact_deprecated's which objects are deprecated, and printf's which functions
// wrap a print, each judged from the function's own body and the facts of what it calls.
var factLearners = []string{"ctrlflow", "fact_deprecated", "printf"}

// packageStateWriters are the packages a rule's run reaches that write a package-level variable from a function body,
// outside a sync.Once or sync.OnceValue: state that outlives one package's pass, which a pass on one package could
// leave for a pass on another to read. The checker's initializer hands upstream's internal package its pass accessor,
// as upstream's does; fact_resolver's registry is each kind registering as its package initializes;
// intermediate_representation's are a build census only tests open and a sync.Pool of scratch space a lift empties
// before it gives it back; and the abbreviation rule's memo is keyed by each load's file set and each declaring
// package's types, and holds what that package declares, so a reader reads the same whichever package asked first.
var packageStateWriters = []string{
	"check/checker",
	"fact_resolver",
	"intermediate_representation",
	"rules/consistency_no_abbreviated_identifier",
}

// engineSource is every package under the go_engine directory, parsed once for the tests that read it.
var engineSource = sync.OnceValues(func() (map[string]*sourcePackage, error) {
	return parseEngineSource("..")
})

// sourcePackage is one directory's non-test Go files under the go_engine directory, parsed with object resolution, so
// an identifier a file declares locally is told apart from a package-level one.
type sourcePackage struct {
	fileSet *token.FileSet
	files   []*ast.File

	// imports are the packages below the go_engine directory its files import, by their directory relative to it.
	imports []string
}

// parseEngineSource is every directory under root, the go_engine directory, that holds a non-test Go file outside
// testdata, by its path relative to root.
func parseEngineSource(root string) (map[string]*sourcePackage, error) {
	parsed := map[string]*sourcePackage{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if parsed[relative] == nil {
			parsed[relative] = &sourcePackage{fileSet: token.NewFileSet()}
		}
		file, parseError := parser.ParseFile(parsed[relative].fileSet, path, nil, 0)
		if parseError != nil {
			return parseError
		}
		parsed[relative].files = append(parsed[relative].files, file)
		for _, imported := range file.Imports {
			importPath, unquoteError := strconv.Unquote(imported.Path.Value)
			if unquoteError != nil {
				return unquoteError
			}
			if below, isBelow := strings.CutPrefix(importPath, engineImportPath); isBelow &&
				!slices.Contains(parsed[relative].imports, below) {
				parsed[relative].imports = append(parsed[relative].imports, below)
			}
		}
		return nil
	})
	return parsed, walkError
}

// parsedEngineSource is engineSource, failing the test when it could not be parsed.
func parsedEngineSource(test *testing.T) map[string]*sourcePackage {
	test.Helper()

	parsed, parseError := engineSource()
	if parseError != nil {
		test.Fatalf("parsing the go_engine directory: %v", parseError)
	}
	return parsed
}

// ruleReach is every package a rule's run can execute: the rules directory and everything below it, and every package
// below the go_engine directory those import, directly or not.
func ruleReach(parsed map[string]*sourcePackage) []string {
	reached := map[string]bool{}
	var queue []string
	for directory := range parsed {
		if directory == "rules" || strings.HasPrefix(directory, "rules/") {
			reached[directory] = true
			queue = append(queue, directory)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if parsed[current] == nil {
			continue
		}
		for _, imported := range parsed[current].imports {
			if !reached[imported] {
				reached[imported] = true
				queue = append(queue, imported)
			}
		}
	}
	return slices.Sorted(maps.Keys(reached))
}

// isSelectorOf reports whether expression is qualifier.name.
func isSelectorOf(expression ast.Expr, qualifier string, name string) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	identifier, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && identifier.Name == qualifier && selector.Sel.Name == name
}

// resolverAskersIn is each package of reach, but fact_resolver, which is the resolver, whose non-test files name
// rule.Resolved or fact_resolver.Of, with where they do.
func resolverAskersIn(parsed map[string]*sourcePackage, reach []string) map[string][]string {
	askers := map[string][]string{}
	for _, directory := range reach {
		if directory == "fact_resolver" || parsed[directory] == nil {
			continue
		}
		for _, file := range parsed[directory].files {
			ast.Inspect(file, func(node ast.Node) bool {
				expression, isExpression := node.(ast.Expr)
				if isExpression && (isSelectorOf(expression, "rule", "Resolved") ||
					isSelectorOf(expression, "fact_resolver", "Of")) {
					askers[directory] = append(askers[directory],
						parsed[directory].fileSet.Position(expression.Pos()).String())
				}
				return true
			})
		}
	}
	return askers
}

// factLearnersOf is each analyzer that declares FactTypes among registered and everything they require, directly or
// not, by name, with the rules that reach it.
func factLearnersOf(registered []analysis.Analyzer, ruleNames []string) map[string][]string {
	learners := map[string][]string{}
	for index := range registered {
		seen := map[*analysis.Analyzer]bool{}
		var visit func(analyzer *analysis.Analyzer)
		visit = func(analyzer *analysis.Analyzer) {
			if seen[analyzer] {
				return
			}
			seen[analyzer] = true
			// The first few rules are enough to say where a learner is reached from; ctrlflow is reached by dozens.
			if len(analyzer.FactTypes) > 0 && len(learners[analyzer.Name]) < 3 &&
				!slices.Contains(learners[analyzer.Name], ruleNames[index]) {
				learners[analyzer.Name] = append(learners[analyzer.Name], ruleNames[index])
			}
			for _, required := range analyzer.Requires {
				visit(required)
			}
		}
		visit(&registered[index])
	}
	return learners
}

// mutatingMethods are the methods of sync's and sync/atomic's types that write the value they are called on, which a
// call on a package-level variable counts as a write by. Every other method call is taken for a read: telling them
// apart needs the receiver's type, which a walk of the syntax alone does not have, and these are how a package keeps
// shared state safe for concurrent passes.
var mutatingMethods = []string{
	"Add", "And", "Clear", "CompareAndDelete", "CompareAndSwap", "Delete", "LoadAndDelete", "LoadOrStore", "Or", "Put",
	"Store", "Swap",
}

// packageVariables is every name a package-level var declaration of files declares, and every such declaration.
func packageVariables(files []*ast.File) (map[string]bool, map[*ast.ValueSpec]bool) {
	names := map[string]bool{}
	declarations := map[*ast.ValueSpec]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			general, isGeneral := declaration.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.VAR {
				continue
			}
			for _, specification := range general.Specs {
				value, isValue := specification.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				declarations[value] = true
				for _, name := range value.Names {
					names[name.Name] = true
				}
			}
		}
	}
	return names, declarations
}

// writtenRoot is the identifier a write to expression writes through: the variable itself, or the one whose element,
// field or pointee it writes, or nil for a write through anything else, as a call's result.
func writtenRoot(expression ast.Expr) *ast.Ident {
	current := expression
	for {
		switch typed := current.(type) {
		case *ast.Ident:
			return typed
		case *ast.IndexExpr:
			current = typed.X
		case *ast.IndexListExpr:
			current = typed.X
		case *ast.SelectorExpr:
			// A selector of an import is another package's variable, whose qualifier is the root written through.
			current = typed.X
		case *ast.StarExpr:
			current = typed.X
		case *ast.ParenExpr:
			current = typed.X
		default:
			return nil
		}
	}
}

// isOnceBody reports whether call runs its function argument once for the whole process, as a sync.Once's Do and
// sync.OnceFunc, sync.OnceValue and sync.OnceValues do, so a write inside it is made once, not once per pass.
func isOnceBody(call *ast.CallExpr) bool {
	if selector, isSelector := call.Fun.(*ast.SelectorExpr); isSelector && selector.Sel.Name == "Do" {
		return true
	}
	// An instantiation spelled out, as sync.OnceValue[int], is the generic function it instantiates.
	function := call.Fun
	if index, isIndex := function.(*ast.IndexExpr); isIndex {
		function = index.X
	}
	if index, isIndex := function.(*ast.IndexListExpr); isIndex {
		function = index.X
	}
	return isSelectorOf(function, "sync", "OnceFunc") || isSelectorOf(function, "sync", "OnceValue") ||
		isSelectorOf(function, "sync", "OnceValues")
}

// isImportQualifier reports whether expression is a bare name of one of a file's imports, which no declaration of the
// file resolves and no package-level variable of the package shadows.
func isImportQualifier(expression ast.Expr, imports map[string]bool, names map[string]bool) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && identifier.Obj == nil && imports[identifier.Name] && !names[identifier.Name]
}

// packageStateWritesOf is where the package's non-test files write a package-level variable, its own or an imported
// package's, from a function body outside a once.
func packageStateWritesOf(source *sourcePackage) []string {
	names, declarations := packageVariables(source.files)
	var writes []string
	for _, file := range source.files {
		imports := map[string]bool{}
		for _, imported := range file.Imports {
			// Ignored: the parser accepted the file, so every import path is a quoted string that unquotes.
			importPath, _ := strconv.Unquote(imported.Path.Value)
			name := importPath[strings.LastIndex(importPath, "/")+1:]
			if imported.Name != nil {
				name = imported.Name.Name
			}
			imports[name] = true
		}
		// isPackageLevel is whether identifier, written through, names a package-level variable: one this file
		// declares, or a name no declaration in the file resolves that another file of the package declares, or an
		// import's qualifier.
		isPackageLevel := func(identifier *ast.Ident) bool {
			if identifier == nil || identifier.Name == "_" {
				return false
			}
			if identifier.Obj != nil {
				value, isValue := identifier.Obj.Decl.(*ast.ValueSpec)
				return isValue && declarations[value]
			}
			return names[identifier.Name] || imports[identifier.Name]
		}
		record := func(at ast.Node) {
			writes = append(writes, source.fileSet.Position(at.Pos()).String())
		}
		var inspect func(node ast.Node, inFunction bool)
		inspect = func(node ast.Node, inFunction bool) {
			ast.Inspect(node, func(current ast.Node) bool {
				if current == node {
					return true
				}
				switch typed := current.(type) {
				case *ast.FuncDecl:
					if typed.Body != nil {
						inspect(typed.Body, true)
					}
					return false
				case *ast.FuncLit:
					inspect(typed.Body, true)
					return false
				case *ast.CallExpr:
					if isOnceBody(typed) {
						return false
					}
					if !inFunction {
						return true
					}
					if isSelectorOf(typed.Fun, "", "") {
						return true
					}
					if builtin, isIdentifier := typed.Fun.(*ast.Ident); isIdentifier &&
						(builtin.Name == "delete" || builtin.Name == "clear") && len(typed.Args) > 0 &&
						isPackageLevel(writtenRoot(typed.Args[0])) {
						record(typed)
					}
					// A call qualified by an import, as slices.Delete or cmp.Or, is a function of that package, never a
					// method of a variable.
					if selector, isSelector := typed.Fun.(*ast.SelectorExpr); isSelector &&
						slices.Contains(mutatingMethods, selector.Sel.Name) &&
						isPackageLevel(writtenRoot(selector.X)) && !isImportQualifier(selector.X, imports, names) {
						record(typed)
					}
				case *ast.AssignStmt:
					if inFunction && typed.Tok != token.DEFINE {
						for _, written := range typed.Lhs {
							if isPackageLevel(writtenRoot(written)) {
								record(written)
							}
						}
					}
				case *ast.IncDecStmt:
					if inFunction && isPackageLevel(writtenRoot(typed.X)) {
						record(typed)
					}
				case *ast.RangeStmt:
					if inFunction && typed.Tok == token.ASSIGN {
						for _, written := range []ast.Expr{typed.Key, typed.Value} {
							if written != nil && isPackageLevel(writtenRoot(written)) {
								record(written)
							}
						}
					}
				}
				return true
			})
		}
		inspect(file, false)
	}
	return writes
}

// requireOnlyKnown fails test for each package of found not in known, with where, and for each of known found
// nowhere, naming list, the variable that holds known, and why a newcomer is read before it joins.
func requireOnlyKnown(test *testing.T, found map[string][]string, known []string, list string, why string) {
	test.Helper()

	for _, newcomer := range slices.Sorted(maps.Keys(found)) {
		if !slices.Contains(known, newcomer) {
			test.Errorf("%s is new to %s (%s): %s", newcomer, list, strings.Join(found[newcomer], ", "), why)
		}
	}
	for _, listed := range known {
		if _, holds := found[listed]; !holds {
			test.Errorf("%s is in %s but no longer reaches its channel: take it out", listed, list)
		}
	}
}

// Every package a rule's run reaches that takes the run's fact resolver from a pass is one whose questions were read:
// the resolver holds every package of the run, the asker's importers and unrelated packages too, so a question that
// reached past fact_resolver.Visible would make a package's findings depend on what else the run loaded, and the walk
// hands an untouched package the host's findings on the premise that they never do (#23b2grt).
func TestOnlyTheKnownRulesAskTheRunsFactResolver(test *testing.T) {
	test.Parallel()

	parsed := parsedEngineSource(test)
	requireOnlyKnown(test, resolverAskersIn(parsed, ruleReach(parsed)), resolverAskers, "resolverAskers",
		"read that every question it asks reaches only the asker's import closure, through fact_resolver.Visible, "+
			"and that its answer is the same whichever package asks first, before adding it")
}

// Every analyzer the rules reach that declares FactTypes is one whose facts were read: the checker carries a fact only
// from a package to its importers, so the facts are bounded by the import closure, and this list keeps a new learner
// from joining the facts the walk restores before anyone has looked at it (#23b2grt).
func TestOnlyTheKnownAnalyzersLearnFacts(test *testing.T) {
	test.Parallel()

	registered := rules.All()
	analyzers := make([]analysis.Analyzer, len(registered))
	names := make([]string, len(registered))
	for index, enabled := range registered {
		analyzers[index], names[index] = *enabled.Analyzer, enabled.Name
	}
	requireOnlyKnown(test, factLearnersOf(analyzers, names), factLearners, "factLearners",
		"read that its facts are about its own package's objects and computed from its import closure alone, "+
			"before adding it")
}

// Every package a rule's run reaches that writes a package-level variable from a function body is one whose state was
// read: a pass runs once per package in one process, so such a variable outlives the pass and could carry what one
// package's pass saw into another's, an importer's into its dependency's (#23b2grt). A write inside a sync.Once or
// sync.OnceValue runs once in the process, whatever package asked first, so it is left out.
func TestOnlyTheKnownPackagesWritePackageStateFromAFunction(test *testing.T) {
	test.Parallel()

	parsed := parsedEngineSource(test)
	found := map[string][]string{}
	for _, directory := range ruleReach(parsed) {
		if parsed[directory] == nil {
			continue
		}
		if writes := packageStateWritesOf(parsed[directory]); len(writes) > 0 {
			found[directory] = writes
		}
	}
	requireOnlyKnown(test, found, packageStateWriters, "packageStateWriters",
		"read that what it keeps is never read by a pass on another package, or reads the same whichever package "+
			"wrote it first, before adding it")
}
