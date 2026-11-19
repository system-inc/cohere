package fact_resolver_test

import (
	"fmt"
	"go/types"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
)

// answeredNames is answers as a test compares them, each as its kind, its object's name and its answer on one line, in
// the order they came.
func answeredNames(answers []fact_resolver.Answered) []string {
	var named []string
	for _, answer := range answers {
		named = append(named, fmt.Sprintf("%s %s %s", answer.Kind, answer.Object.Name(), answer.Encoded))
	}
	return named
}

// Answered is every answer the run holds, where Recorded holds only the ones with an objectpath that a run with a
// store computed (#kemy8sq). Asked of the unexported quiet and then of Middle, each computed as true, a run with a
// store answers both and records Middle alone, quiet having no path, and a run with no store answers both and records
// nothing. The store's answer about Middle, false, and a hand's, false of both, are answered too, and never recorded.
// The answers come in the order their objects are declared, Middle before quiet, though quiet was asked of first.
func TestAnsweredHoldsEveryAnswerWhereRecordedHoldsOnlyThoseWithAPathAndAStore(test *testing.T) {
	test.Parallel()
	for name, held := range map[string]struct {
		store        fact_resolver.Store
		handed       []byte
		wantAnswered []string
		wantRecorded []string
	}{
		"computed in a run with a store": {store: keepStore{},
			wantAnswered: []string{"keep_probe Middle true", "keep_probe quiet true"},
			wantRecorded: []string{"Middle"}},
		"computed in a run with no store": {
			wantAnswered: []string{"keep_probe Middle true", "keep_probe quiet true"}},
		"the store's answer": {store: keepStore{stored: []byte("false")},
			wantAnswered: []string{"keep_probe Middle false", "keep_probe quiet true"}},
		"a hand's answers": {store: keepStore{}, handed: []byte("false"),
			wantAnswered: []string{"keep_probe Middle false", "keep_probe quiet false"}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			run := learnMiddleWith(test, held.store, held.handed, false)
			quiet, isFunction := run.middle.Types.Scope().Lookup("quiet").(*types.Func)
			if !isFunction {
				test.Fatal("middle declares no function quiet")
			}
			run.ask(quiet, true)
			run.ask(run.function, true)
			if answered := answeredNames(run.resolver.Answered()); !slices.Equal(answered, held.wantAnswered) {
				test.Errorf("want %q answered, got %q", held.wantAnswered, answered)
			}
			var recorded []string
			for path := range run.resolver.Recorded(run.middle.Types)[keepProbe.Name] {
				recorded = append(recorded, string(path))
			}
			slices.Sort(recorded)
			if !slices.Equal(recorded, held.wantRecorded) {
				test.Errorf("want %q recorded, got %q", held.wantRecorded, recorded)
			}
		})
	}
}

// An unsure answer is never among the answers a run holds, since nothing unsure is memoized: a question of
// bottom.Bodied, which the run restored and no store answers, is answered a placeholder, and one of Middle afterwards
// is computed in a package left unsure. Neither is answered.
func TestAnUnsureAnswerIsNeverAmongTheAnswered(test *testing.T) {
	test.Parallel()
	run := learnMiddle(test, nil, true)
	run.ask(run.bodied, true)
	run.ask(run.function, true)
	if !run.resolver.Unsure(run.middle.Types) || run.computed != 1 {
		test.Fatalf("want middle unsure and Middle's answer computed once, got unsure %t after computing %d times",
			run.resolver.Unsure(run.middle.Types), run.computed)
	}
	if answered := answeredNames(run.resolver.Answered()); len(answered) != 0 {
		test.Errorf("want no answer held in a run that was only ever unsure, got %q", answered)
	}
}
