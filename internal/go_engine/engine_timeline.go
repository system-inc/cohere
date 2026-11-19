package go_engine

import (
	"time"

	"github.com/system-inc/cohere/internal/go_engine/timeline"
)

// engineStart is the moment every timeline stamp's wall is measured from, taken as the package initializes, which is
// the engine process's start. One read of the wall clock, in every process, asked or not: the start must be had before
// the command line says whether anyone wants it.
var engineStart = time.Now()

// timelineClock is the clock a run's recorder reads under `--timeline`, the system's, which a test replaces with one
// that counts its reads, to show that a run that did not ask reads none.
var timelineClock = timeline.SystemClock()

// newTimeline is a check run's recorder under `--timeline`, stamped `engine.main.begin` as it is made, and nil for a
// run that did not ask, which stamps nothing and reads no clock (#3v2zp7g), and for a listing or the editor's save,
// which write no timeline record. Made as the command line is read, the first moment the run knows it was asked: what
// came before is the process's start and Main's memory limit, which the stamp's wall holds.
func newTimeline(request invocation) *timeline.Recorder {
	if !request.switches["timeline"] || request.mode() != modeCheck {
		return nil
	}
	recorder := timeline.New(engineStart, timelineClock)
	recorder.Stamp("engine.main.begin")
	return recorder
}
