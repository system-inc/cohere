package odd_slice_argument_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/system-inc/go-types/types"
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

// Every mark a judgment settles is told, once, the asked one and those it rests on alike (#z9fze0t). Over
// order/forward the rule asks one mark, of back's slice, for odd's call. Judging it settles forward's and pairs' too,
// which back's answer rests on and nobody asks: each is handed to the resolver as an answer of its own
// (fact_resolver.Keep), so a hand hears all three, each once and marked, back's among them only once though it was
// both kept and asked. What the run records for the store is another matter, the asked marks alone
// (TestTheRecordIsTheAskedMarksInEitherOrder, #8d9r2rh).
func TestEveryMarkAJudgmentSettlesIsToldOnce(test *testing.T) {
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
		test.Errorf("the run told the marks %v, want %v told, each once, marked", heard,
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

// directQuestions is an analyzer on reader asking the marks of subject's functions named in order, each of subject
// itself, and then Back's as reader asks it.
func directQuestions(order []string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     "direct_questions",
		Doc:      "asks subject's marks of subject itself in a given order, and then Back's as reader asks it",
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
			parameterOf := func(name string) (*types.Var, error) {
				function, isFunction := subject.Scope().Lookup(name).(*types.Func)
				if !isFunction {
					return nil, fmt.Errorf("%s declares no function %s", subject.Path(), name)
				}
				return function.Signature().Params().At(0), nil
			}
			for _, name := range order {
				parameter, parameterError := parameterOf(name)
				if parameterError != nil {
					return nil, parameterError
				}
				odd_slice_argument.Ask(resolver, home, parameter)
			}
			back, backError := parameterOf("Back")
			if backError != nil {
				return nil, backError
			}
			odd_slice_argument.Ask(resolver, reader, back)
			return nil, nil
		},
	}
}

// The resolver's record of the marks is exactly the marks questions asked, whichever judgment settled them and in
// whatever order they were asked (#8d9r2rh). Package reader asks one mark of package subject, of Back's slice. Judging
// it settles Forward's and a function literal's, and asks below.Pairs's across a package. After the rule's one
// question the record holds Back's mark and Pairs's, and nothing of Forward, the literal or Loose: Forward's mark is
// kept in subject's state, never recorded unasked. Asking Forward's and Back's both, callee first and then caller
// first, records the two in either order, and nothing else; so a judgment entering a chain at either end leaves the
// same record. Every package's reads are the same in all three, and no package reads its own answer.
func TestTheRecordIsTheAskedMarksInEitherOrder(test *testing.T) {
	test.Parallel()
	reads := map[string][]string{
		"recorded/reader":  {"odd_slice_argument recorded/subject Back.PA0=true"},
		"recorded/subject": {"odd_slice_argument recorded/below Pairs.PA0=true"},
		"recorded/below":   {},
	}
	oneQuestion := recordedRun{
		marks: map[string]map[objectpath.Path]string{
			"recorded/reader":  {},
			"recorded/subject": {"Back.PA0": "true"},
			"recorded/below":   {"Pairs.PA0": "true"},
		},
		reads: reads,
	}
	bothAsked := recordedRun{
		marks: map[string]map[objectpath.Path]string{
			"recorded/reader":  {},
			"recorded/subject": {"Back.PA0": "true", "Forward.PA0": "true"},
			"recorded/below":   {"Pairs.PA0": "true"},
		},
		reads: reads,
	}
	for name, asked := range map[string]struct {
		analyzer *analysis.Analyzer
		want     recordedRun
	}{
		"the rule's one question": {analyzer: odd_slice_argument.Analyzer, want: oneQuestion},
		"callee first":            {analyzer: directQuestions([]string{"Forward", "Back"}), want: bothAsked},
		"caller first":            {analyzer: directQuestions([]string{"Back", "Forward"}), want: bothAsked},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			ran := runRecorded(test, asked.analyzer)
			if !reflect.DeepEqual(ran, asked.want) {
				test.Errorf("the resolver holds the marks %v and the reads %v, want %v and %v", ran.marks, ran.reads,
					asked.want.marks, asked.want.reads)
			}
		})
	}
}
