// Package timeline records, under `--timeline`, when each phase of a Go run and each build began and ended, on one
// clock from the engine process's start, with the process's user and system CPU at each, for the timeline record.
//
// A Recorder travels by argument, never as package state: the engine makes one for a run that asked, and hands the
// load and the check a hook of it (Prefixed), so two runs in one process, as two parallel tests are, never stamp each
// other's. A nil *Recorder is a run that did not ask, and every method on it does nothing and reads no clock, so a
// stamp site stamps without asking first, and a run without the switch pays nothing for it.
package timeline

import (
	"sync"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// cpuUnavailable is the record's reason when a read of the process's CPU failed, so no CPU figure means anything.
const cpuUnavailable = "the process's CPU clock could not be read"

// Recorder holds a run's stamps in the order they arrived. It is safe to use from many goroutines at once.
type Recorder struct {
	// start is the moment every stamp's wall is measured from, the engine process's start.
	start time.Time
	clock Clock

	// lock is held across each stamp's reads of the clock and its append, so the stamps' order is their wall's.
	lock   sync.Mutex
	stamps []contract.TimelineStamp

	// cpuUnknown is set once any read of the process's CPU failed.
	cpuUnknown bool
}

// New is a recorder measuring from start, reading clock.
func New(start time.Time, clock Clock) *Recorder {
	return &Recorder{start: start, clock: clock}
}

// Stamp appends label with the wall since the recorder's start and the process's user and system CPU so far. The
// clock is read under the lock, so of two stamps from two goroutines the one appended first has the earlier wall.
func (recorder *Recorder) Stamp(label string) {
	if recorder == nil {
		return
	}
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	wall := recorder.clock.Now().Sub(recorder.start)
	user, system, known := recorder.clock.ProcessTimes()
	if !known {
		recorder.cpuUnknown = true
	}
	recorder.stamps = append(recorder.stamps, contract.TimelineStamp{
		Label:              label,
		WallMicroseconds:   wall.Microseconds(),
		UserMicroseconds:   user.Microseconds(),
		SystemMicroseconds: system.Microseconds(),
	})
}

// Prefixed is a hook stamping prefix before each label it is told, for a load or a check that stamps its own steps
// under the name of the build it is part of, or nil on a nil recorder, which the hook's every caller reads as nothing
// to stamp (Tell).
func (recorder *Recorder) Prefixed(prefix string) func(label string) {
	if recorder == nil {
		return nil
	}
	return func(label string) { recorder.Stamp(prefix + label) }
}

// Record is the timeline record of every stamp so far, in the order they arrived, with timed saying whether the run
// was also under `--timing`. When any read of the process's CPU failed, no CPU figure is written and the record says
// why. A nil recorder's is the zero record.
func (recorder *Recorder) Record(timed bool) contract.TimelineRecord {
	if recorder == nil {
		return contract.TimelineRecord{}
	}
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	record := contract.TimelineRecord{
		Timed:  timed,
		Stamps: make([]contract.TimelineStamp, 0, len(recorder.stamps)),
	}
	for _, stamp := range recorder.stamps {
		if recorder.cpuUnknown {
			stamp.UserMicroseconds, stamp.SystemMicroseconds = 0, 0
		}
		record.Stamps = append(record.Stamps, stamp)
	}
	if recorder.cpuUnknown {
		record.Unavailable = cpuUnavailable
	}
	return record
}

// Tell tells hook of label, when there is a hook: a Prefixed hook as the load and the check are handed it, nil in a
// run that did not ask.
func Tell(hook func(label string), label string) {
	if hook != nil {
		hook(label)
	}
}
