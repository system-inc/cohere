package test_failure_in_goroutine

import (
	"encoding/json"
	"fmt"
	"go/types"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
)

// handedAnswers is a store that hands back the kind's answers an earlier run recorded (fact_resolver.Store), by the
// package's path, since two loads of a package hold two type packages, and reloaded nothing.
type handedAnswers struct {
	recorded map[string]map[objectpath.Path][]byte
}

func (handed handedAnswers) Answers(packageTypes *types.Package) *fact_resolver.Answers {
	values, isHeld := handed.recorded[packageTypes.Path()]
	if !isHeld {
		return nil
	}
	return &fact_resolver.Answers{Values: map[string]map[objectpath.Path][]byte{Kind.Name: values}}
}

func (handedAnswers) Reloaded(*types.Package) bool {
	return false
}

// storedRun is one run of the rule over the fixtures as the engine runs it, with a store: its findings, each rendered
// with its related entries, sorted, its resolver, and its hand on the answers.
type storedRun struct {
	roots    []*packages.Package
	findings []string
	resolver *fact_resolver.Resolver
	hand     *heardAnswers
}

// runWithStore loads the fixtures and runs the rule over them with store as the run's store.
func runWithStore(test *testing.T, store fact_resolver.Store) storedRun {
	test.Helper()
	run := storedRun{roots: loadFixtures(test), resolver: fact_resolver.New(store)}
	run.hand = newHeardAnswers(run.roots[0].Fset, nil)
	run.resolver.HoldAnswers(run.hand)
	options := check.DispatchOptions()
	options.Resolver = run.resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{Analyzer}, run.roots, options)
	if analyzeError != nil {
		test.Fatalf("running the rule: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package.ID, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			run.findings = append(run.findings, renderedFinding(action.Package.Fset, diagnostic))
		}
	}
	slices.Sort(run.findings)
	return run
}

// An answer a store gives back, the branch of fact_resolver.Ask a saved entry's answers come through, reports what the
// answer computed in a run reports. A first run over a store that holds nothing records the one answer that can be
// kept, that about (*parity.suite).stops, the one function the fixtures start that has an objectpath. A second run, of
// another load, over a store that hands that answer back finds what the first found, the finding that rests on the
// answer with its related entry placed in the second run's own files, and never reads the function: it computes one
// answer fewer, the store's one hit, and its hand hears no question about the function. The other functions the
// fixtures start have no objectpath, so no store can hold their answers, and both runs compute them.
func TestAnAnswerAStoreGivesBackReportsWhatTheComputedOneReports(test *testing.T) {
	test.Parallel()
	const stored = "(*parity.suite).stops"
	first := runWithStore(test, noAnswers{})
	handed := handedAnswers{recorded: map[string]map[objectpath.Path][]byte{}}
	var related string
	for _, root := range first.roots {
		answers := first.resolver.Recorded(root.Types)[Kind.Name]
		if len(answers) == 0 {
			continue
		}
		handed.recorded[root.Types.Path()] = answers
		for _, encoded := range answers {
			var calls []StoppingCall
			if decodeError := json.Unmarshal(encoded, &calls); decodeError != nil {
				test.Fatalf("decoding the answer the first run recorded in %s: %v", root.PkgPath, decodeError)
			}
			if len(calls) != 1 || !calls[0].HasSyntax {
				test.Fatalf("the first run recorded %+v in %s, want one call with syntax", calls, root.PkgPath)
			}
			related = fmt.Sprintf("[related %s:%d to %s:%d %q]", filepath.Base(calls[0].File), calls[0].Start,
				filepath.Base(calls[0].File), calls[0].End, "call to T."+calls[0].Method)
		}
	}
	if len(handed.recorded) != 1 || len(handed.recorded["parity"]) != 1 {
		test.Fatalf("the first run recorded the kind as %v, want the one answer about %s, so there is one to hand back",
			handed.recorded, stored)
	}
	resting := func(findings []string) int {
		return len(slices.DeleteFunc(slices.Clone(findings), func(finding string) bool {
			return !strings.Contains(finding, related)
		}))
	}
	if resting(first.findings) != 1 {
		test.Fatalf("the first run holds %d findings with the related entry %s, want the one at the go statement "+
			"starting %s: %q", resting(first.findings), related, stored, first.findings)
	}

	second := runWithStore(test, handed)
	if !slices.Equal(second.findings, first.findings) {
		test.Errorf("the run handed the stored answer finds %q, want what the run that computed it finds, %q",
			second.findings, first.findings)
	}
	if resting(second.findings) != 1 {
		test.Errorf("the run handed the stored answer holds %d findings with the related entry %s, want one",
			resting(second.findings), related)
	}
	computedFirst := first.resolver.Counts(Kind).Computed.Load()
	computedSecond := second.resolver.Counts(Kind).Computed.Load()
	if computedSecond != computedFirst-1 || second.resolver.StoreHits() != 1 {
		test.Errorf("the run handed the stored answer read %d functions and took %d answers from its store, where the "+
			"first read %d: want one fewer read and one answer from the store", computedSecond,
			second.resolver.StoreHits(), computedFirst)
	}
	if len(second.hand.asked) == 0 {
		test.Error("the second run's hand heard no question, so it proves nothing of the one it should not hear")
	}
	for _, question := range second.hand.asked {
		if function, isFunction := question.object.(*types.Func); isFunction && function.FullName() == stored {
			test.Errorf("the second run would have computed %s of %s, whose answer its store holds", question.kind,
				stored)
		}
	}
	for _, root := range second.roots {
		if again := second.resolver.Recorded(root.Types)[Kind.Name]; len(again) != 0 {
			test.Errorf("the second run recorded %d answers of the kind in %s, want none, the one that can be kept "+
				"having come from the store", len(again), root.PkgPath)
		}
	}
}
