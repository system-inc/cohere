package odd_slice_argument_test

import (
	"fmt"
	"go/types"
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
)

// hearingHand is a test's hand on a run's answers that answers nothing itself and hears every answer the run computes
// of the marks, each by its object, as many times as it was told.
type hearingHand struct {
	lock  sync.Mutex
	heard map[types.Object][]string
}

func (*hearingHand) Answer(string, types.Object) ([]byte, bool) {
	return nil, false
}

func (hand *hearingHand) Computed(kindName string, object types.Object, encoded []byte) {
	if kindName != odd_slice_argument.Kind.Name {
		return
	}
	hand.lock.Lock()
	defer hand.lock.Unlock()
	hand.heard[object] = append(hand.heard[object], string(encoded))
}

// Every mark a judgment settles reaches the resolver's record as an asked one does (#8d9r2rh). Over order/forward the
// rule asks one mark, of back's slice, for odd's call. Judging it settles forward's and pairs' too, which back's
// answer rests on and nobody asks. An answer on record is a sound place to stop comparing two builds only when
// everything it read is on record beside it, so the resolver tells all three as it tells an answer it was asked, each
// once and marked, and nothing else: enter and odd take no slice.
func TestEveryMarkAJudgmentSettlesIsRecorded(test *testing.T) {
	test.Parallel()
	roots := loadFixtures(test, analysistest.TestData(), []string{"order/forward"})
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	hand := &hearingHand{heard: map[types.Object][]string{}}
	resolver.HoldAnswers(hand)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing order/forward: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running on %s: %v", action.Package.ID, action.Err)
		}
	}
	heard := map[string][]string{}
	for object, answers := range hand.heard {
		heard[object.Name()+" at "+roots[0].Fset.Position(object.Pos()).String()] = answers
	}
	want := map[string][]string{}
	for _, name := range []string{"back", "forward", "pairs"} {
		function, isFunction := roots[0].Types.Scope().Lookup(name).(*types.Func)
		if !isFunction {
			test.Fatalf("order/forward declares no function %s", name)
		}
		parameter := function.Signature().Params().At(0)
		want[parameter.Name()+" at "+roots[0].Fset.Position(parameter.Pos()).String()] = []string{"true"}
	}
	if !maps.EqualFunc(heard, want, slices.Equal) {
		test.Errorf("the run told the marks %v, want each of %v told once, marked", heard,
			slices.Sorted(maps.Keys(want)))
	}
}

// A package with no call passing a slice of known odd length asks no mark and judges nothing (#8d9r2rh), which is
// nearly every package: demand/cycle and parity/pairs check lengths and pass slices on, and hold no such call, so over
// them the resolver judges no function and tells no answer.
func TestAPackageWithNoOddSliceJudgesNothing(test *testing.T) {
	test.Parallel()
	roots := loadFixtures(test, analysistest.TestData(), []string{"demand/cycle", "parity/pairs"})
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	hand := &hearingHand{heard: map[types.Object][]string{}}
	resolver.HoldAnswers(hand)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{odd_slice_argument.Analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the packages: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil || len(action.Diagnostics) != 0 {
			test.Fatalf("running on %s: %d findings, %v", action.Package.ID, len(action.Diagnostics), action.Err)
		}
	}
	if judged := resolver.Counts(odd_slice_argument.Kind).Computed.Load(); judged != 0 || len(hand.heard) != 0 {
		test.Errorf("with no odd slice passed anywhere the resolver judged %d functions and told %d answers, want none",
			judged, len(hand.heard))
	}
}

// recordedRun is what one run left in the resolver for the three packages of the recorded fixture, each by its path:
// the marks recorded for the store, by objectpath, and the answers the package's analysis read of other packages, each
// as its owner's path, the object's path and the answer.
type recordedRun struct {
	marks map[string]map[objectpath.Path]string
	reads map[string][]string
}

// runRecorded loads recorded/reader, runs analyzer on it under a store that holds nothing, so the run records every
// answer and every read, and returns what it left for reader, subject and below.
func runRecorded(test *testing.T, analyzer *analysis.Analyzer) recordedRun {
	test.Helper()
	roots := loadFixtures(test, analysistest.TestData(), []string{"recorded/reader"})
	resolver := fact_resolver.New(emptyStore{})
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing recorded/reader: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s on %s: %v", action.Analyzer.Name, action.Package.ID, action.Err)
		}
	}
	subject := roots[0].Imports["recorded/subject"]
	ran := recordedRun{marks: map[string]map[objectpath.Path]string{}, reads: map[string][]string{}}
	for _, each := range []*packages.Package{roots[0], subject, subject.Imports["recorded/below"]} {
		ran.marks[each.PkgPath] = map[objectpath.Path]string{}
		for path, answer := range resolver.Recorded(each.Types)[odd_slice_argument.Kind.Name] {
			ran.marks[each.PkgPath][path] = string(answer)
		}
		reads, whole := resolver.Reads(each.Types)
		if !whole {
			test.Fatalf("%s read an answer no entry can name", each.PkgPath)
		}
		ran.reads[each.PkgPath] = []string{}
		for _, read := range reads {
			if read.Owner == each.Types {
				test.Errorf("%s holds a read of its own answer at %s, so its entry would depend on itself",
					each.PkgPath, read.Path)
			}
			ran.reads[each.PkgPath] = append(ran.reads[each.PkgPath], fmt.Sprintf("%s %s %s=%s", read.Kind,
				read.Owner.Path(), read.Path, read.Value))
		}
	}
	return ran
}

// A mark a judgment settles is recorded exactly as a direct question of it records one, and reads nothing more
// (#8d9r2rh). Package reader asks one mark of package subject, of Back's slice. Judging it settles Forward's and a
// function literal's, and reads below.Pairs's across a package. The run's record must hold Back's and Forward's marks,
// the declared functions', and nothing of the literal or of Loose, which no pass reaches; and both the record and
// every package's reads must be what a second run leaves that asks each mark directly, callee first, so that no
// judgment there settles a mark nobody asked. No package reads its own answer, in either.
func TestASettledMarkIsRecordedAsADirectQuestionRecordsIt(test *testing.T) {
	test.Parallel()
	settled := runRecorded(test, odd_slice_argument.Analyzer)
	direct := runRecorded(test, &analysis.Analyzer{
		Name:     "direct_questions",
		Doc:      "asks each of subject's marks of subject itself, callee first, and then Back's as reader asks it",
		Requires: []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			subject := pass.Pkg.Imports()[0]
			home, reader := resolver.Package(subject), resolver.Package(pass.Pkg)
			if home == nil || reader == nil {
				return nil, fmt.Errorf("the resolver never learned %s or %s", subject.Path(), pass.Pkg.Path())
			}
			for _, name := range []string{"Forward", "Back"} {
				function, isFunction := subject.Scope().Lookup(name).(*types.Func)
				if !isFunction {
					return nil, fmt.Errorf("%s declares no function %s", subject.Path(), name)
				}
				odd_slice_argument.Ask(resolver, home, function.Signature().Params().At(0))
				if name == "Back" {
					odd_slice_argument.Ask(resolver, reader, function.Signature().Params().At(0))
				}
			}
			return nil, nil
		},
	})
	want := recordedRun{
		marks: map[string]map[objectpath.Path]string{
			"recorded/reader":  {},
			"recorded/subject": {"Back.PA0": "true", "Forward.PA0": "true"},
			"recorded/below":   {"Pairs.PA0": "true"},
		},
		reads: map[string][]string{
			"recorded/reader":  {"odd_slice_argument recorded/subject Back.PA0=true"},
			"recorded/subject": {"odd_slice_argument recorded/below Pairs.PA0=true"},
			"recorded/below":   {},
		},
	}
	for name, ran := range map[string]recordedRun{"the rule's one question": settled, "direct questions": direct} {
		if !reflect.DeepEqual(ran, want) {
			test.Errorf("after %s the resolver holds the marks %v and the reads %v, want %v and %v", name, ran.marks,
				ran.reads, want.marks, want.reads)
		}
	}
}
