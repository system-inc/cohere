package fact_resolver_test

import (
	"go/token"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_language/timing"
)

// passHolding is a pass on a package of its own whose ResultOf holds resolver under rule.Resolved, a typed nil for a
// nil resolver, and, when collector is not nil, collector under rule.Timed, as the checker hands a rule both.
func passHolding(resolver *fact_resolver.Resolver, collector *timing.Collector) *analysis.Pass {
	pass := passHoldingNothing()
	pass.ResultOf[rule.Resolved] = resolver
	if collector != nil {
		pass.ResultOf[rule.Timed] = collector
	}
	return pass
}

// passHoldingNothing is a pass on a package of its own whose ResultOf holds nothing, as a driver that set no
// checker.Options.Resolver hands one.
func passHoldingNothing() *analysis.Pass {
	return &analysis.Pass{Pkg: types.NewPackage("example.com/asking", "asking"),
		ResultOf: map[*analysis.Analyzer]any{}}
}

// sharedRowNames is the name of every shared row of collector's record, in order, every row being shared here since no
// analyzer is a rule's.
func sharedRowNames(collector *timing.Collector) []string {
	record := collector.Record(func(string) (string, contract.SharedKind) { return "", "" })
	var names []string
	for _, row := range record.Shared {
		names = append(names, row.Name)
	}
	slices.Sort(names)
	return names
}

// A pass whose rule.Resolved holds no resolver, or a typed nil one, is refused with an error and no resolver, so a rule
// never reads one the checker did not hand it.
func TestAPassWithoutAResolverIsRefused(test *testing.T) {
	test.Parallel()
	for name, pass := range map[string]*analysis.Pass{
		"nothing":     passHoldingNothing(),
		"a typed nil": passHolding(nil, nil),
	} {
		resolver, resolverError := fact_resolver.Of(pass)
		if resolver != nil || resolverError == nil {
			test.Errorf("with %s the pass returned %p and %v, want an error alone", name, resolver, resolverError)
		}
	}
}

// A question runs its work once, timed or not, and so does a build inside it: an untimed run is the default, and work
// run twice there would cost every question twice.
func TestAQuestionRunsItsWorkOnce(test *testing.T) {
	test.Parallel()
	for name, collector := range map[string]*timing.Collector{"untimed": nil, "timed": timing.New()} {
		resolver := fact_resolver.New(nil)
		if _, resolverError := fact_resolver.Of(passHolding(resolver, collector)); resolverError != nil {
			test.Fatal(resolverError)
		}
		questions, builds := 0, 0
		resolver.Question("asking", func() {
			questions++
			resolver.MeasureBuild(func() { builds++ })
		})
		if questions != 1 || builds != 1 {
			test.Errorf("%s: the question ran %d times and its build %d, want once each", name, questions, builds)
		}
	}
}

// Under `--timing` a question is billed to JudgeRow and a build inside it to BuildRow, the resolver's two rows, the
// collector being the one the first pass handed over (Of).
func TestATimedQuestionIsBilledToTheResolversRows(test *testing.T) {
	test.Parallel()
	collector := timing.New()
	resolver := fact_resolver.New(nil)
	if _, resolverError := fact_resolver.Of(passHolding(resolver, collector)); resolverError != nil {
		test.Fatal(resolverError)
	}
	resolver.Question("asking", func() {
		resolver.MeasureBuild(func() { /* A build of nothing: only the row it is billed to is read. */ })
	})
	want := []string{fact_resolver.BuildRow, fact_resolver.JudgeRow}
	if rows := sharedRowNames(collector); !reflect.DeepEqual(rows, want) {
		test.Errorf("the record's shared rows are %v, want %v", rows, want)
	}
}

// graphsRun is a run on middle, loaded from source, of ctrlflow and an analyzer that hands on ctrlflow's graphs as its
// own result, beside build_intermediate_representation when readers is set: the resolver, middle, and the package the
// resolver held of middle while that analyzer ran.
type graphsRun struct {
	resolver *fact_resolver.Resolver
	middle   *packages.Package
	learned  *fact_resolver.Package
}

// runGraphsOnMiddle runs graphsRun's analyses on middle.
func runGraphsOnMiddle(test *testing.T, readers bool) graphsRun {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/middle"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	run := graphsRun{resolver: fact_resolver.New(nil), middle: loaded.Roots[0]}
	options := check.DispatchOptions()
	options.Resolver = run.resolver
	handing := &analysis.Analyzer{
		Name:       "graphs_handed_on",
		Doc:        "hands on ctrlflow's graphs as its own result, holding the package the resolver learned",
		Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
		ResultType: reflect.TypeFor[*ctrlflow.CFGs](),
		Run: func(pass *analysis.Pass) (any, error) {
			if pass.Pkg == run.middle.Types {
				run.learned = run.resolver.Package(pass.Pkg)
			}
			return pass.ResultOf[ctrlflow.Analyzer], nil
		},
	}
	analyzers := []*analysis.Analyzer{handing}
	if readers {
		analyzers = append(analyzers, build_intermediate_representation.Analyzer)
	}
	graph, analyzeError := checker.Analyze(analyzers, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s on %s: %v", action.Analyzer.Name, action.Package.ID, action.Err)
		}
	}
	return run
}

// The resolver learns a package from ctrlflow's action alone: an analyzer that finishes after it with ctrlflow's
// graphs as its own result leaves the package ctrlflow taught it, which questions asked before hold, in place.
func TestFinishedLearnsAPackageFromCtrlflowAlone(test *testing.T) {
	test.Parallel()
	run := runGraphsOnMiddle(test, false)
	if run.learned == nil {
		test.Fatal("the resolver had not learned middle when an analyzer requiring ctrlflow ran there")
	}
	if held := run.resolver.Package(run.middle.Types); held != run.learned {
		test.Errorf("after the run the resolver holds middle as %p, want %p, the package ctrlflow taught it", held,
			run.learned)
	}
}

// ReadersPackage is the package build_intermediate_representation's result holds only for a package that analyzer
// runs on: none for one ctrlflow alone taught the resolver, one where the analyzer ran beside ctrlflow, and none for
// one the resolver never learned, asked last since a wrong guard there dereferences the missing package.
func TestReadersPackageIsHeldOnlyWhereTheReadersRun(test *testing.T) {
	test.Parallel()
	if alone := runGraphsOnMiddle(test, false); alone.resolver.ReadersPackage(alone.middle.Types) != nil {
		test.Error("middle, taught by ctrlflow with no readers there, has a readers' package, want none")
	}
	if beside := runGraphsOnMiddle(test, true); beside.resolver.ReadersPackage(beside.middle.Types) == nil {
		test.Error("middle, with build_intermediate_representation run there, has no readers' package")
	}
	never := types.NewPackage("example.com/never", "never")
	if readers := fact_resolver.New(nil).ReadersPackage(never); readers != nil {
		test.Errorf("a package the resolver never learned has a readers' package %v, want none", readers)
	}
}

// Owner is nil for an object of no package, as the error interface's Error is, and fails loudly for an object of a
// package the resolver never learned, which answering would have to guess.
func TestOwnerFailsLoudlyForAPackageNeverLearned(test *testing.T) {
	test.Parallel()
	resolver := fact_resolver.New(nil)
	errorInterface, isInterface := types.Universe.Lookup("error").Type().Underlying().(*types.Interface)
	if !isInterface {
		test.Fatal("error is no interface")
	}
	if owner := resolver.Owner(errorInterface.Method(0)); owner != nil {
		test.Errorf("error's Error, of no package, is owned by %v, want nil", owner)
	}
	never := types.NewPackage("example.com/never", "never")
	function := types.NewFunc(token.NoPos, never, "Never", types.NewSignatureType(nil, nil, nil, nil, nil, false))
	recovered := func() (recovered any) {
		defer func() { recovered = recover() }()
		resolver.Owner(function)
		return nil
	}()
	if message, isMessage := recovered.(string); !isMessage || !strings.Contains(message, "never learned") {
		test.Errorf("asking the owner of %s recovered %v, want a panic naming the package never learned",
			function.FullName(), recovered)
	}
}
