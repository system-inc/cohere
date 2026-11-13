package main

import (
	"slices"
	"testing"
)

// The schedule runs corpus by corpus and round by round, base first in odd rounds and branch first in even ones,
// and each side's cache-on run before its `--no-cache` run.
func TestScheduleAlternatesWhichSideRunsFirst(test *testing.T) {
	test.Parallel()
	names := []string{}
	for _, planned := range schedule([]string{corpusRoot, corpusLoom}, 3) {
		names = append(names, planned.name())
	}
	want := []string{}
	for _, corpusName := range []string{"root", "loom"} {
		want = append(want,
			corpusName+"-base-1-cache", corpusName+"-base-1-no-cache",
			corpusName+"-branch-1-cache", corpusName+"-branch-1-no-cache",
			corpusName+"-branch-2-cache", corpusName+"-branch-2-no-cache",
			corpusName+"-base-2-cache", corpusName+"-base-2-no-cache",
			corpusName+"-base-3-cache", corpusName+"-base-3-no-cache",
			corpusName+"-branch-3-cache", corpusName+"-branch-3-no-cache",
		)
	}
	if !slices.Equal(names, want) {
		test.Errorf("the schedule is\n%q\nwant\n%q", names, want)
	}
}

// The corpora run smallest first, the order every pair has been posted in.
func TestCorpusOrderIsRootDeeafbf4LoomAdamic(test *testing.T) {
	test.Parallel()
	if want := []string{"root", "deeafbf4", "loom", "adamic"}; !slices.Equal(corpusOrder, want) {
		test.Errorf("the corpora run in the order %q, want %q", corpusOrder, want)
	}
}
