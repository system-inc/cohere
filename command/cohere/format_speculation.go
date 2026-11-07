package main

import (
	"sync"
	"sync/atomic"

	"github.com/microsoft/TypeScript/tsc/shim/tspath"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/types/program"
)

// formatSpeculation is the format pass begun before the walk: one attempt per format candidate, computed as
// formatInParallel computes a file with no proposals, and kept or discarded once the walk is over.
type formatSpeculation struct {
	done     chan struct{}
	attempts map[string]formatAttempt
	// read is the text each unchanged attempt was computed from, so a result is kept only for those exact
	// bytes. A changed attempt, kept only by a run that writes nothing, has none: its text is the printer's.
	read map[string]string

	// stopped, once closed, has the workers take no more files: what they have not begun is left to
	// formatInParallel, which has every core. See stop.
	stopped  chan struct{}
	stopOnce sync.Once

	// limit is how many of the workers may take another file. See narrow.
	limit atomic.Int32
}

// narrow has all but the first workers finish the file they are on and take no more. The files left go to the
// workers that remain, and what they have not begun when the walk ends goes to formatInParallel.
func (speculation *formatSpeculation) narrow(workers int) {
	speculation.limit.Store(int32(workers))
}

// stop has the workers finish the files they are on and begin no more. Called once the walk is over on a run
// whose walk also did the type check (program.FusedCheck), and once the plan is taken on a run with no program,
// such as `--format-only`, which has no walk: nothing else then wants the cores, and a quarter of them
// formatting the rest would set the run's end (#679s763, #9h6g0s3).
func (speculation *formatSpeculation) stop() {
	speculation.stopOnce.Do(func() { close(speculation.stopped) })
}

// wait returns once every attempt is in.
func (speculation *formatSpeculation) wait() {
	<-speculation.done
}

// keepable waits for the speculation and returns the attempts that stand: a file no rule proposed a fix for,
// whose bytes, where the walk read the file, are the bytes the attempt read. A result whose file has a
// proposal describes text the fixes will replace, and one read from other bytes than the walk's would pair
// the walk's findings with formatting of a different file. A changed attempt holds no read bytes to compare,
// so where the program holds its file, a json file a module imports, it is discarded.
func (speculation *formatSpeculation) keepable(
	byFileName map[string][]edit.Proposal,
	graph *program.Graph,
) map[string]formatAttempt {
	speculation.wait()
	kept := make(map[string]formatAttempt, len(speculation.attempts))
	for fileName, attempt := range speculation.attempts {
		if len(byFileName[fileName]) != 0 {
			continue
		}
		if attempt.formatError == nil && graph != nil && graph.Program != nil {
			if sourceFile := graph.Program.GetSourceFile(tspath.RootedFilePath(fileName)); sourceFile != nil {
				if read, held := speculation.read[fileName]; !held || sourceFile.Text() != read {
					continue
				}
			}
		}
		kept[fileName] = attempt
	}
	return kept
}
