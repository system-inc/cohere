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
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// engineImportPath is the import path of the go_engine directory, by which a file's imports name the packages below it.
const engineImportPath = "github.com/system-inc/cohere/internal/go_engine/"

// resolverAskers are the packages a rule's run reaches that take the run's fact resolver from a pass, by rule.Resolved
// or fact_resolver.Of. The resolver holds every package of the run, so a question through it could read a package
// outside the asker's import closure; each of these asks only through fact_resolver.Visible, or shares a root's
// representation the resolver builds the same way whoever asks first (#23b2grt), or, as test_failure_in_goroutine,
// asks only of a function the asker's own package declares, read from that function's own instructions, and panics
// on any other (#h0k644s). function_triviality asks only of the asker's own package too, refusing any other, and cuts
// its answer where the walk would read another package's body, so the answer reads nothing outside the package and is
// the same in whichever program the package builds.
var resolverAskers = []string{
	"rules/build_intermediate_representation",
	"rules/function_purity",
	"rules/function_triviality",
	"rules/odd_slice_argument",
	"rules/return_nil_ness",
	"rules/test_failure_in_goroutine",
}

// factLearners are the analyzers that declare FactTypes among every rule and everything it requires, by name. The
// checker carries a fact only up the imports (inheritFacts), so a fact is bounded by the import closure by
// construction; the list is here so a new learner is read before its facts join what the walk restores. ctrlflow's
// facts are which functions never return, fact_deprecated's which objects are deprecated, and printf's which functions
// wrap a print, each judged from the function's own body and the facts of what it calls.
var factLearners = []string{"ctrlflow", "fact_deprecated", "printf"}

// packageStateWriters are the packages a rule's run reaches that write package state from a function body (see
// packageStateWritesOf): a package-level variable, or what one holds behind a pointer, a field or a map's entry,
// written by any function or method of the reach through a value of the held type or a local given such state. That
// state outlives one package's pass, which a pass on one package could leave for a pass on another to read. A write
// inside a once's body counts too when the body captures its caller (see capturesCaller), since the first package to
// reach the once decides it for every later pass. What runs before any pass is left out: an init function's body, a
// function that only initialization calls, as each vet rule's newAnalyzer is, and a flag's setter that only flag calls
// (see packageStateExemptionsOf). dispatch's is each dispatcher's registry: the shared ones are filled from package
// initializers, every `var Analyzer = dispatch.RuleAnalyzer(...)` and each vet rule's newAnalyzer, and a dispatcher of
// its own from NewSyntax is filled only by a rule's NewAnalyzer, which only tests call; register panics on a name it
// holds, and a pass only reads the registry, through run. fact_resolver's registry is each kind registering as its
// package initializes; intermediate_representation's are a build census only tests open and a sync.Pool of scratch
// space a lift empties before it gives it back. cgo_call's are the syntax of the cgo files its pass parsed for itself,
// which it rewrites to declare nothing before it type-checks them, and no other pass is handed. load_memo's is the
// per-load memo of consistency_no_any and consistency_no_abbreviated_identifier, each held by a package-level variable:
// it is keyed by each load's file set and each declaring package's types, and holds what compute reads off that package
// alone, once, so a reader reads the same whichever package asked first, and an entry goes only when its load's file
// set is collected (#k8e189q). return_nil_ness's is a sync.Pool of value numberings, each emptied by release of every
// slot and entry its function claimed before it goes back, so a pass reads an empty numbering whichever package's
// judgment put it there (#fex6s0v).
// staticcheck_configuration's build caches are written by go env's answers (load.ReadGoEnvironment), never by a pass,
// and only grow, each by a directory go writes its own files to, so a pass reads the same whatever ran before it.
//
// What a green does not cover, since the guard reads types, never how values flow:
//   - state passed to a helper, or returned by one, that writes it, as `fill(memo.byLoad)`, since an alias is followed
//     within one function only;
//   - a value held behind an interface or a function, whose type does not say what it holds, but for a package-level
//     variable's initial value;
//   - a method of a type outside the reach that writes its receiver, but for mutatingMethods;
//   - a send on a channel package state holds;
//   - a pass that sets a flag through flag.FlagSet.Set or the flag.Value interface, which reaches an exempt setter
//     with no reference the guard can see;
//   - an append into the spare capacity of a held slice whose result is dropped, which writes the shared array and
//     assigns nothing;
//   - a mutating method taken as a value or a method expression, as `store := cache.Store; store(key, value)` or
//     `(*sync.Map).Store(&cache, key, value)`, which no call of the method's name shows;
//   - state moved through a composite literal, as `local := wrapper{inner: held.byKey}; local.inner[key] = 1`, since
//     an alias is a local given held state directly;
//   - state a package-level variable of function type returns, as `lazyState().byKey[key] = 1` after
//     `var lazyState = sync.OnceValue(...)`, since a call's result is held only by its type;
//   - a method value kept at package level, as `var putter = (&T{}).put`, whose receiver no type walk reaches;
//   - an interface variable assigned in an init function, whose dynamic type is not its initializer's.
var packageStateWriters = []string{
	"fact_resolver",
	"intermediate_representation",
	"rules/dispatch",
	"rules/load_memo",
	"rules/return_nil_ness",
	"rules/vet/cgo_call",
	"staticcheck_configuration",
}

// engineSource is every package under the go_engine directory, parsed once for the tests that read it.
var engineSource = sync.OnceValues(func() (map[string]*sourcePackage, error) {
	return parseEngineSource("..")
})

// sourcePackage is one directory's non-test Go files under the go_engine directory, parsed for the imports that make
// the rules' reach and the names its packages call.
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
func factLearnersOf(registered []*analysis.Analyzer, ruleNames []string) map[string][]string {
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
		visit(registered[index])
	}
	return learners
}

// requireOnlyKnown fails test for each package of found not in known, with where, and for each of known found
// nowhere, naming list, the variable that holds known, why a newcomer is read before it joins, and stopped, what a
// listed one found nowhere no longer does.
func requireOnlyKnown(
	test *testing.T,
	found map[string][]string,
	known []string,
	list string,
	why string,
	stopped string,
) {
	test.Helper()

	for _, newcomer := range slices.Sorted(maps.Keys(found)) {
		if !slices.Contains(known, newcomer) {
			test.Errorf("%s is new to %s (%s): %s", newcomer, list, strings.Join(found[newcomer], ", "), why)
		}
	}
	for _, listed := range known {
		if _, holds := found[listed]; !holds {
			test.Errorf("%s is in %s but %s: take it out", listed, list, stopped)
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
			"and that its answer is the same whichever package asks first, before adding it",
		"no longer reaches its channel")
}

// Every analyzer the rules reach that declares FactTypes is one whose facts were read: the checker carries a fact only
// from a package to its importers, so the facts are bounded by the import closure, and this list keeps a new learner
// from joining the facts the walk restores before anyone has looked at it (#23b2grt).
func TestOnlyTheKnownAnalyzersLearnFacts(test *testing.T) {
	test.Parallel()

	registered := rules.All()
	analyzers := make([]*analysis.Analyzer, len(registered))
	names := make([]string, len(registered))
	for index, enabled := range registered {
		analyzers[index], names[index] = enabled.Analyzer, enabled.Name
	}
	requireOnlyKnown(test, factLearnersOf(analyzers, names), factLearners, "factLearners",
		"read that its facts are about its own package's objects and computed from its import closure alone, "+
			"before adding it",
		"no longer reaches its channel")
}

// Every package a rule's run reaches that writes package state from a function body is one whose state was read: a
// pass runs once per package in one process, so a package-level variable, and what it holds behind a pointer, a field
// or a map's entry, outlives the pass and could carry what one package's pass saw into another's, an importer's into
// its dependency's (#23b2grt, #rhsx4gc). A sync.Once or sync.OnceValue does not make a write safe, since the first
// package to reach it decides it for every pass after: a write in its body counts unless the body reads nothing of the
// function that calls it, and so builds the same data whoever runs it.
func TestOnlyTheKnownPackagesWritePackageStateFromAFunction(test *testing.T) {
	test.Parallel()

	parsed := parsedEngineSource(test)
	reach := loadedReach(test)
	test.Logf("the rules' reach, %d packages, loaded with types in %s", len(reach.byDirectory), reach.wall)
	var reached []*packages.Package
	for _, directory := range ruleReach(parsed) {
		if parsed[directory] == nil {
			continue
		}
		if reach.byDirectory[directory] == nil {
			test.Fatalf("%s is in the rules' reach, but the typed load holds no package of it", directory)
		}
		reached = append(reached, reach.byDirectory[directory])
	}
	held := heldTypesOf(reached)
	exempt := packageStateExemptionsOf(reached)
	found := map[string][]string{}
	for _, analyzed := range reached {
		if writes := packageStateWritesOf(analyzed, held, exempt); len(writes) > 0 {
			found[strings.TrimPrefix(analyzed.PkgPath, engineImportPath)] = writes
		}
	}
	requireOnlyKnown(test, found, packageStateWriters, "packageStateWriters",
		"read that what it keeps is never read by a pass on another package, or reads the same whichever package "+
			"wrote it first, before adding it",
		"no longer writes package state")
}
