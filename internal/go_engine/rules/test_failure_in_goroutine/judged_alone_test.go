package test_failure_in_goroutine

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The shapes of what a go statement starts, as startedShape names them.
const (
	shapeDeclaredFunction = "a function the package declares"
	shapeMethod           = "a method the package declares"
	shapeClosure          = "a closure"
	shapeFunctionLiteral  = "a function literal that closes over nothing"
	shapeWrapper          = "a wrapper"
	shapeOtherPackage     = "a function another package declares"
	shapeNoFunction       = "no function"
)

// startedShape is the shape of what goStatement, a go statement of function, starts.
func startedShape(
	function *intermediate_representation.Function,
	goStatement *intermediate_representation.Go,
) string {
	started := startedFunction(goStatement)
	_, isClosure := goStatement.Call.Value.(*intermediate_representation.MakeClosure)
	switch {
	case started == nil:
		return shapeNoFunction
	case started.Parent() != nil && isClosure:
		return shapeClosure
	case started.Parent() != nil:
		return shapeFunctionLiteral
	case started.Package == nil:
		return shapeWrapper
	case started.Package != function.Package:
		return shapeOtherPackage
	case started.Signature.Recv() != nil:
		return shapeMethod
	default:
		return shapeDeclaredFunction
	}
}

// loadFixtures loads the rule's test packages, every one the tests beside this file run it over, from source.
func loadFixtures(test *testing.T) []*packages.Package {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{upstream, "parity", "differences"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixtures: %v", loadError)
	}
	return loaded.Roots
}

// placeAt is position as two loads of a file name it alike: the file's name and the offset in it.
func placeAt(fileSet *token.FileSet, position token.Pos) string {
	file := fileSet.File(position)
	if file == nil {
		return "nowhere"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file.Name()), file.Offset(position))
}

// renderedFinding is diagnostic as one line, its range, its message and each related entry's, which two runs'
// findings are compared by.
func renderedFinding(fileSet *token.FileSet, diagnostic analysis.Diagnostic) string {
	rendered := fmt.Sprintf("%s to %s %q", placeAt(fileSet, diagnostic.Pos), placeAt(fileSet, diagnostic.End),
		diagnostic.Message)
	for _, related := range diagnostic.Related {
		rendered += fmt.Sprintf(" [related %s to %s %q]", placeAt(fileSet, related.Pos),
			placeAt(fileSet, related.End), related.Message)
	}
	return rendered
}

// functionKey names the function whose syntax is node, a function declaration or literal of the package at
// packagePath, as two loads name it alike, or the package's initializer for no syntax.
func functionKey(fileSet *token.FileSet, packagePath string, node ast.Node) string {
	if node == nil {
		return packagePath + " initializer"
	}
	return packagePath + " " + placeAt(fileSet, node.Pos())
}

// enclosingFunction is the innermost function declaration or literal of files holding position, nil for none.
func enclosingFunction(files []*ast.File, position token.Pos) ast.Node {
	var innermost ast.Node
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil || position < node.Pos() || position >= node.End() {
				return false
			}
			switch node.(type) {
			case *ast.FuncDecl, *ast.FuncLit:
				innermost = node
			}
			return true
		})
	}
	return innermost
}

// sourceFunction is the syntax of the function declaration or literal function was built from, or of the nearest one
// around it for a function built from a statement, and nil for the package's initializer.
func sourceFunction(function *intermediate_representation.Function) ast.Node {
	for enclosing := function; enclosing != nil; enclosing = enclosing.Parent() {
		switch enclosing.Syntax().(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return enclosing.Syntax()
		}
	}
	return nil
}

// wholeFindings runs the rule over roots as the engine does, every function built first, and is its findings by the
// function each one's go statement is in, in the order the rule reported them.
func wholeFindings(test *testing.T, roots []*packages.Package) map[string][]string {
	test.Helper()
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{Analyzer}, roots, check.DispatchOptions())
	if analyzeError != nil {
		test.Fatalf("running the rule whole: %v", analyzeError)
	}
	findings := map[string][]string{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running the rule whole on %s: %v", action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			key := functionKey(action.Package.Fset, action.Package.PkgPath,
				enclosingFunction(action.Package.Syntax, diagnostic.Pos))
			findings[key] = append(findings[key], renderedFinding(action.Package.Fset, diagnostic))
		}
	}
	return findings
}

// aloneRun is what judging each function of the fixtures alone found: the findings by function, how many go
// statements of each shape were judged and how many of them were reported, and how many functions the kind read.
type aloneRun struct {
	findings map[string][]string
	started  map[string]int
	reported map[string]int
	computed int64
}

// judgeAlone judges each function of roots alone, with hand's hand on the run's answers. No function is built first:
// the run holds only ctrlflow and StoppingCallsAnalyzer, so each package is made in the resolver's program from its
// syntax, unbuilt, which the probe requires of every declared function before it builds one. Then it takes each
// declared function in turn, builds only it, by the lazy Function.Build, and runs Check on it and on each function
// literal built with it. A function Check reads without asking is one built with the function judged, or one an
// earlier turn built, which a later turn cannot tell from unbuilt except by its findings.
func judgeAlone(test *testing.T, roots []*packages.Package, hand fact_resolver.Held) aloneRun {
	test.Helper()
	var lock sync.Mutex
	run := aloneRun{findings: map[string][]string{}, started: map[string]int{}, reported: map[string]int{}}
	probe := &analysis.Analyzer{
		Name:       "judged_alone_probe",
		Doc:        "builds each function of the package alone and runs SA2002's check on it",
		Requires:   []*analysis.Analyzer{StoppingCallsAnalyzer},
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			irPackage := resolver.Package(pass.Pkg).IntermediateRepresentation()
			for _, declared := range irPackage.Functions {
				if declared.Blocks != nil {
					return nil, fmt.Errorf("%s was built before any function was judged", declared)
				}
			}
			var found []analysis.Diagnostic
			context := rule.NewContext(rule.Namespace+"SA2002", pass, Listener.Requires, &Analyzer.Flags,
				func(string) bool { return false },
				func(_ string, diagnostic analysis.Diagnostic) { found = append(found, diagnostic) })
			var judge func(function *intermediate_representation.Function)
			judge = func(function *intermediate_representation.Function) {
				found = nil
				Check(context, function)
				lock.Lock()
				key := functionKey(pass.Fset, pass.Pkg.Path(), sourceFunction(function))
				for _, diagnostic := range found {
					run.findings[key] = append(run.findings[key], renderedFinding(pass.Fset, diagnostic))
				}
				for _, block := range function.Blocks {
					for _, instruction := range block.Instructions {
						goStatement, isGo := instruction.(*intermediate_representation.Go)
						if !isGo {
							continue
						}
						shape := startedShape(function, goStatement)
						run.started[shape]++
						if slices.ContainsFunc(found, func(diagnostic analysis.Diagnostic) bool {
							return diagnostic.Pos == goStatement.Source().Pos()
						}) {
							run.reported[shape]++
						}
					}
				}
				lock.Unlock()
				for _, anonymous := range function.AnonymousFunctions {
					judge(anonymous)
				}
			}
			for _, declared := range irPackage.Functions {
				declared.Build()
				judge(declared)
			}
			return "", nil
		},
	}
	options := check.DispatchOptions()
	resolver := fact_resolver.New(nil)
	resolver.HoldAnswers(hand)
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, roots, options)
	if analyzeError != nil {
		test.Fatalf("judging each function alone: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("judging each function of %s alone: %v", action.Package.ID, action.Err)
		}
	}
	run.computed = resolver.Counts(Kind).Computed.Load()
	return run
}

// requireSameFindings fails for each function whose findings in got are not want's, in order, naming both.
func requireSameFindings(test *testing.T, described string, want map[string][]string, got map[string][]string) {
	test.Helper()
	functions := slices.Sorted(maps.Keys(want))
	for _, function := range slices.Sorted(maps.Keys(got)) {
		if _, isWanted := want[function]; !isWanted {
			functions = append(functions, function)
		}
	}
	for _, function := range functions {
		if !slices.Equal(got[function], want[function]) {
			test.Errorf("in the function at %s, %s finds %q, want %q", function, described, got[function],
				want[function])
		}
	}
}

// Each function judged alone, with no other declaration built before it, finds what the whole run finds in it, finding
// for finding with its related entry, over every fixture of the rule: a function the package declares, started
// before it is built, is asked of the fact resolver, which builds it. The fixtures hold a go statement of each shape
// that is reported, a function and a method the package declares, a closure and a function literal, and one starting
// another package's function, which is never asked about, though it stops the test and its package is there to build.
func TestEachFunctionJudgedAloneFindsWhatTheWholeRunFinds(test *testing.T) {
	test.Parallel()
	whole := wholeFindings(test, loadFixtures(test))
	if len(whole) == 0 {
		test.Fatal("the whole run found nothing, so the comparison would prove nothing")
	}
	roots := loadFixtures(test)
	hand := newHeardAnswers(roots[0].Fset, nil)
	alone := judgeAlone(test, roots, hand)
	requireSameFindings(test, "the function judged alone", whole, alone.findings)
	for _, shape := range []string{shapeDeclaredFunction, shapeMethod, shapeClosure, shapeFunctionLiteral} {
		if alone.reported[shape] == 0 {
			test.Errorf("no go statement starting %s was reported, of %d judged, so the fixtures leave the shape "+
				"unproven", shape, alone.started[shape])
		}
	}
	if alone.started[shapeOtherPackage] == 0 || alone.reported[shapeOtherPackage] != 0 {
		test.Errorf("want go statements starting %s judged and none reported, got %d judged and %d reported",
			shapeOtherPackage, alone.started[shapeOtherPackage], alone.reported[shapeOtherPackage])
	}
	own := map[string]bool{}
	for _, root := range roots {
		own[root.PkgPath] = true
	}
	if len(hand.asked) == 0 {
		test.Error("the resolver was never asked, so no function the package declares was read through an answer")
	}
	for _, question := range hand.asked {
		if question.kind != Kind.Name || !own[question.object.Pkg().Path()] {
			test.Errorf("the resolver was asked %s about %s of %s, which no fixture judged declares", question.kind,
				question.object.Name(), question.object.Pkg().Path())
		}
	}
	test.Logf("judged alone, %d functions hold findings, and whole, %d; go statements reported of those judged, by "+
		"what they start: %v of %v; the resolver was asked %d questions and read %d functions", len(alone.findings),
		len(whole), alone.reported, alone.started, len(hand.asked), alone.computed)
}

// An answer held as its JSON and handed back in another run, as a store's or a test's is, reports what the answer
// computed in a run reports, each related entry at the same place: the second run reads no function, every answer
// about a function the package declares coming from the first run's, placed in the second run's own files.
func TestAnAnswerHandedBackReportsWhatTheComputedOneReports(test *testing.T) {
	test.Parallel()
	hearing := loadFixtures(test)
	heard := newHeardAnswers(hearing[0].Fset, nil)
	computed := judgeAlone(test, hearing, heard)
	if computed.computed == 0 || len(heard.computed) == 0 {
		test.Fatalf("the first run read %d functions and heard %d answers, so there is nothing to hand back",
			computed.computed, len(heard.computed))
	}
	handing := loadFixtures(test)
	handed := judgeAlone(test, handing, newHeardAnswers(handing[0].Fset, heard.computed))
	if handed.computed != 0 {
		test.Errorf("the second run read %d functions, want every answer handed back", handed.computed)
	}
	requireSameFindings(test, "the run handed its answers", computed.findings, handed.findings)
}

// A function's stopping calls encode to JSON and back without loss, the call with no syntax among them, and no call
// is the answer of no fact, which decodes as it was.
func TestStoppingCallsEncodeAndDecodeWithoutLoss(test *testing.T) {
	test.Parallel()
	for _, calls := range [][]StoppingCall{
		nil,
		{{Method: "FailNow"}},
		{
			{Method: "Fatalf", HasSyntax: true, File: "/fixtures/src/parity/parity.go", Start: 1024, End: 1047},
			{Method: "SkipNow"},
			{Method: "Skip", HasSyntax: true, File: "/fixtures/src/parity/parity.go", Start: 0, End: 9},
		},
	} {
		encoded, encodeError := json.Marshal(calls)
		if encodeError != nil {
			test.Fatalf("encoding %+v: %v", calls, encodeError)
		}
		var decoded []StoppingCall
		if decodeError := json.Unmarshal(encoded, &decoded); decodeError != nil {
			test.Fatalf("decoding %s: %v", encoded, decodeError)
		}
		if !slices.Equal(decoded, calls) || (decoded == nil) != (calls == nil) {
			test.Errorf("%+v encodes as %s, which decodes as %+v", calls, encoded, decoded)
		}
	}
}
