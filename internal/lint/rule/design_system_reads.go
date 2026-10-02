package rule

import (
	"sort"
	"sync"

	"github.com/microsoft/TypeScript/tsc/shim/compiler"
)

// designSystemRecordings keeps every recording file system DesignSystemFS handed out for one program, so the
// findings cache can learn what the design system read without importing the reader (#35nqkwc). One slot,
// replaced when another program asks, the same shape as the design system's own cache: a run has one
// program, and a slot per program would hold every program a test process ever built.
var designSystemRecordings struct {
	sync.Mutex
	program   *compiler.Program
	recorders []*RecordingFS
}

func recordDesignSystemFS(program *compiler.Program, recorder *RecordingFS) {
	designSystemRecordings.Lock()
	defer designSystemRecordings.Unlock()
	if designSystemRecordings.program != program {
		designSystemRecordings.program = program
		designSystemRecordings.recorders = nil
	}
	designSystemRecordings.recorders = append(designSystemRecordings.recorders, recorder)
}

// DesignSystemReads is everything the design system read for program, and whether it was loaded at all. A
// run whose rules never asked for it reports false: nothing it reported can depend on a stylesheet.
//
// The reads of every load are merged, a path seen present in any keeping present, so a second load that
// asked about more than the first widens the set rather than replacing it.
func DesignSystemReads(program *compiler.Program) ([]FileRead, bool) {
	designSystemRecordings.Lock()
	defer designSystemRecordings.Unlock()
	if designSystemRecordings.program != program || len(designSystemRecordings.recorders) == 0 {
		return nil, false
	}
	merged := map[string]FileRead{}
	for _, recorder := range designSystemRecordings.recorders {
		for _, read := range recorder.Reads() {
			if earlier, seen := merged[read.Path]; seen && (earlier.Present || !read.Present) {
				continue
			}
			merged[read.Path] = read
		}
	}
	reads := make([]FileRead, 0, len(merged))
	for _, read := range merged {
		reads = append(reads, read)
	}
	sort.Slice(reads, func(first, second int) bool { return reads[first].Path < reads[second].Path })
	return reads, true
}
