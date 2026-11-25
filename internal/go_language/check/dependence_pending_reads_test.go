package check

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
)

// A set asked of reads whose walk is still running is the whole import closure for that ask alone, never stored
// (#ybjbqm2): the copied user's reads, asked before they are recorded, give the whole closure, and asked again once
// recorded, the set they reach, as a memo that never saw them pending gives it. Reads marked never are final, and
// their whole closure is stored.
func TestADependenceSetAskedOfPendingReadsIsNotStored(test *testing.T) {
	test.Parallel()

	host, memo := genericHost(test)
	pending := &build_reads.Reads{}
	early := memo.Dependence(Restoring{Path: genericPrefix + "copied", Reads: pending})
	if !early.WholeClosure {
		test.Errorf("reads still pending gave %v, want the whole closure", dependenceNames(early))
	}
	if _, stored := memo.dependences.Load(pending); stored {
		test.Errorf("the memo stored the set of reads still pending")
	}
	recorded := genericRestoringInto(test, host, "copied", pending)
	want := dependenceNames(measuredReadsBuild().Dependence(genericRestoring(test, host, "copied")))
	if slices.Equal(want, []string{"whole"}) {
		test.Fatalf("the copied user's set is the whole closure, so a stored pending answer would read the same")
	}
	if got := dependenceNames(memo.Dependence(recorded)); !slices.Equal(got, want) {
		test.Errorf("asked again once recorded, the set is %v, want %v", got, want)
	}

	never := &build_reads.Reads{}
	never.MarkNever(build_reads.RestoredFromCache)
	if set := memo.Dependence(Restoring{Path: genericPrefix + "held", Reads: never}); !set.WholeClosure {
		test.Errorf("reads marked never gave %v, want the whole closure", dependenceNames(set))
	}
	if _, stored := memo.dependences.Load(never); !stored {
		test.Errorf("the memo did not store the whole closure of reads marked never")
	}
}
