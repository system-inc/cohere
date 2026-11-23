package fact_resolver_test

import (
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// probeRun is a run that learned top and middle, for a test of Probe: its resolver, the two packages as it holds them,
// and bottom.Bodied, a function of the package the run restored and no store answers of.
type probeRun struct {
	resolver *fact_resolver.Resolver
	top      *fact_resolver.Package
	middle   *fact_resolver.Package
	bodied   *types.Func
}

// learnTop runs ctrlflow on top and, below it, on middle, both loaded from source, with bottom restored and a store
// that answers nothing, so the resolver learns top and middle and a question of bottom has no answer.
func learnTop(test *testing.T) probeRun {
	test.Helper()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"bodiless/top"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	middle := loaded.Roots[0].Imports["bodiless/middle"]
	bottom := middle.Imports["bodiless/bottom"]
	run := probeRun{resolver: fact_resolver.New(keepStore{})}
	options := check.DispatchOptions()
	options.Resolver = run.resolver
	options.Restored = func(restoring *packages.Package) bool { return restoring == bottom }
	options.Restore = func(*checker.Action) error { return nil }
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{ctrlflow.Analyzer}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running ctrlflow on %s: %v", action.Package.ID, action.Err)
		}
	}
	run.top, run.middle = run.resolver.Package(loaded.Roots[0].Types), run.resolver.Package(middle.Types)
	run.bodied = bottomFunction(bottom.Types, "Bodied")
	if run.top == nil || run.middle == nil || run.bodied == nil {
		test.Fatalf("the resolver learned top as %v and middle as %v, and bottom declares Bodied as %v", run.top,
			run.middle, run.bodied)
	}
	return run
}

// leaveMiddleUnsure asks, as middle's reader, a question of bottom.Bodied, which the run restored and no store
// answers, as a pass on middle would: the answer is a placeholder, which leaves middle unsure and top sure.
func leaveMiddleUnsure(test *testing.T, run probeRun) {
	test.Helper()
	fact_resolver.Ask(run.resolver, run.middle, keepProbe, run.bodied,
		func(*fact_resolver.Package) bool { return true }, func() bool { return false })
	if !run.resolver.Unsure(run.middle.Types) || run.resolver.Unsure(run.top.Types) {
		test.Fatalf("want middle unsure and top sure after a question of bottom, got middle unsure %t, top unsure %t",
			run.resolver.Unsure(run.middle.Types), run.resolver.Unsure(run.top.Types))
	}
}

// A probe never answers from an unsure package, as Ask keeps nothing computed in one: its verdicts may rest on a
// placeholder. Probed of middle.Fresh after a question left middle unsure, the resolver could not answer, where a
// run with no placeholder answers never nil. Nor of middle.Middle, whose answer is no fact, null, the very bytes of
// the placeholder's zero Value, so only the package's being unsure tells the two apart. And probed of top.Top, whose
// answer is read from middle's, it could not either, though top was sure when the probe began: the probe's own
// question, answered from an unsure package, left top unsure, which no reload abandons, since middle was learned. So
// Probe asks whether the package is unsure after its question, never only before.
func TestAProbeNeverAnswersFromAnUnsurePackage(test *testing.T) {
	test.Parallel()
	const neverNil = `[{"Inner":4,"Outer":1}]`
	for name, probed := range map[string]struct {
		ofTop bool
		path  objectpath.Path
		want  string
	}{
		"the package probed was left unsure":                       {ofTop: false, path: "Fresh", want: neverNil},
		"the package probed was, and the answer is no fact":        {ofTop: false, path: "Middle", want: "null"},
		"a package below it was, whose answer the probe then read": {ofTop: true, path: "Top", want: neverNil},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			owner := func(run probeRun) *types.Package {
				if probed.ofTop {
					return run.top.Types
				}
				return run.middle.Types
			}
			sure := learnTop(test)
			encoded, answered := sure.resolver.Probe(return_nil_ness.Kind.Name, owner(sure), probed.path)
			if !answered || string(encoded) != probed.want {
				test.Fatalf("with no placeholder, want %s probed as %s, got %q (answered: %t)", probed.path,
					probed.want, encoded, answered)
			}
			unsure := learnTop(test)
			leaveMiddleUnsure(test, unsure)
			encoded, answered = unsure.resolver.Probe(return_nil_ness.Kind.Name, owner(unsure), probed.path)
			if answered || encoded != nil {
				test.Errorf("after a placeholder left middle unsure, want no answer of %s, got %q (answered: %t)",
					probed.path, encoded, answered)
			}
			if !unsure.resolver.Unsure(owner(unsure)) {
				test.Errorf("want the package of %s unsure once the probe has asked", probed.path)
			}
		})
	}
}
