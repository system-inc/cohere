package go_language

import (
	"time"

	"github.com/system-inc/cohere/internal/go_language/timeline"
)

// engineStart is the moment every timeline stamp's wall is measured from, taken as the package initializes, after the
// runtime and every package the engine imports have initialized, which is as early as the engine can read it. The
// process's CPU counts from the process's own start, before it. One read of the wall clock, in every process, asked or
// not: the start must be had before the command line says whether anyone wants it.
var engineStart = time.Now()

// newTimeline is a check run's recorder under `--timeline`, stamped `engine.main.begin` as it is made, reading the
// request's clock, the system's unless a test's run hands it one, and nil for a run that did not ask, which stamps
// nothing and reads no clock (#3v2zp7g). Only a check run reaches here with the switch: any other refuses it
// (invocation.check). Made as the command line is read, the first moment the run knows it was asked: what came before
// is the package's initialization and Main's memory limit, which the stamp's wall holds.
func newTimeline(request invocation) *timeline.Recorder {
	if !request.switches["timeline"] {
		return nil
	}
	clock := request.timelineClock
	if clock.Now == nil {
		clock = timeline.SystemClock()
	}
	recorder := timeline.New(engineStart, clock)
	recorder.Stamp("engine.main.begin")
	return recorder
}
