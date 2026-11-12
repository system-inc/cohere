package check

import (
	"go/types"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// persistReads refuses reads that can never hold, so no entry carries a promise its package's reads were whole: a
// package with cgo files or importing runtime/cgo (vet's cgocall checks them again, unrecorded), one with a finding
// naming another package's file, one checked for its types only, one the findings cache restored, and reads never
// recorded. Each is refused, and recorded reads of the same package are persisted, so the refusal is what each mark
// decides. Without it, a run whose host hits its own store would keep such an entry with reads, and a build would
// restore a package whose reads were never whole (#pcv6eff, unit (3) wires the writing).
func TestPersistReadsRefusesReadsThatCanNeverHold(test *testing.T) {
	test.Parallel()

	own := types.NewPackage("example.com/own", "own")
	recorded := func() *build_reads.Reads {
		reads := &build_reads.Reads{}
		reads.RecordTypes(own, &types.Info{}, nil)
		return reads
	}
	if _, persisted := persistReads(Restoring{Path: own.Path(), Reads: recorded()}, measuredReadsBuild(),
		ReadsRendererVersion); !persisted {
		test.Fatal("recorded reads with no never mark were refused, so nothing tells a mark's refusal apart")
	}
	for _, reason := range []string{
		"it has cgo files, which vet's cgocall type-checks again, unrecorded",
		"it imports runtime/cgo, so vet's cgocall type-checks its cgo files again, unrecorded",
		"a finding of printf names a file of another package",
		"checked for its types only",
		build_reads.RestoredFromCache,
	} {
		reads := recorded()
		reads.MarkNever(reason)
		if record, persisted := persistReads(Restoring{Path: own.Path(), Reads: reads}, measuredReadsBuild(),
			ReadsRendererVersion); persisted {
			test.Errorf("reads marked never, %q, were persisted as %+v, want refused", reason, record)
		}
	}
	if record, persisted := persistReads(Restoring{Path: own.Path(), Reads: &build_reads.Reads{}},
		measuredReadsBuild(), ReadsRendererVersion); persisted {
		test.Errorf("reads never recorded were persisted as %+v, want refused", record)
	}
}
