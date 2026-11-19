package checker

import (
	"reflect"
	"slices"
	"sync"
	"time"

	"golang.org/x/tools/go/analysis"
)

// standingPass is what one pass of a standingPasses analyzer found: whether it held anything under standingKey, a
// nil included, and what, what it held under failedKey, the names of the results it held beside those keys, sorted,
// and whether the analyzer the test has it follow had finished on its package by then.
type standingPass struct {
	keyed    bool
	standing any
	failed   map[*analysis.Analyzer]error
	results  []string
	followed bool
}

// standingPasses records, by analyzer name and package path, what each pass of the analyzers it builds found, the
// packages the followed analyzer has finished on, and, by package ID, what a test's Options.StandingBodies was asked
// with each time it was asked, a string or, for anything else, nothing.
type standingPasses struct {
	mutex    sync.Mutex
	passes   map[string]standingPass
	finished map[string]bool
	asked    map[string][]string
}

// newStandingPasses is a standingPasses that has recorded nothing.
func newStandingPasses() *standingPasses {
	return &standingPasses{passes: map[string]standingPass{}, finished: map[string]bool{}, asked: map[string][]string{}}
}

// ask records that a test's Options.StandingBodies was asked for packageId with after.
func (passes *standingPasses) ask(packageId string, after any) {
	afterText, _ := after.(string)
	passes.mutex.Lock()
	defer passes.mutex.Unlock()
	passes.asked[packageId] = append(passes.asked[packageId], afterText)
}

// analyzer is one named name that requires required and records what each of its passes found.
func (passes *standingPasses) analyzer(name string, required ...*analysis.Analyzer) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     name,
		Doc:      name + " records what its pass holds under the standing key and beside it",
		Requires: required,
		Run: func(pass *analysis.Pass) (any, error) {
			found := standingPass{}
			found.standing, found.keyed = pass.ResultOf[standingKey]
			found.failed, _ = pass.ResultOf[failedKey].(map[*analysis.Analyzer]error)
			for held := range pass.ResultOf {
				if held != standingKey && held != failedKey {
					found.results = append(found.results, held.Name)
				}
			}
			slices.Sort(found.results)
			passes.mutex.Lock()
			defer passes.mutex.Unlock()
			found.followed = passes.finished[pass.Pkg.Path()]
			passes.passes[name+" "+pass.Pkg.Path()] = found
			return nil, nil
		},
	}
}

// followedAnalyzer is the analyzer a test has the dispatchers follow, named followed: on each package it waits long
// enough that an action that did not wait for it would have run, then marks the package finished and returns
// "followed on" and the package's path, or failure when that is not nil.
func (passes *standingPasses) followedAnalyzer(failure error) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:       "followed",
		Doc:        "followed is what the dispatchers of a standing test follow",
		ResultType: reflect.TypeFor[string](),
		Run: func(pass *analysis.Pass) (any, error) {
			time.Sleep(20 * time.Millisecond)
			passes.mutex.Lock()
			defer passes.mutex.Unlock()
			passes.finished[pass.Pkg.Path()] = true
			if failure != nil {
				return nil, failure
			}
			return "followed on " + pass.Pkg.Path(), nil
		},
	}
}
