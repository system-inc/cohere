package main

import "time"

// phaseTiming is one phase's wall time and the CPU the process spent in it, its own and its finished children's (go
// list's), in seconds.
type phaseTiming struct {
	WallSeconds        float64 `json:"wallSeconds"`
	UserSeconds        float64 `json:"userSeconds"`
	SystemSeconds      float64 `json:"systemSeconds"`
	ChildrenCpuSeconds float64 `json:"childrenCpuSeconds"`
}

// CpuSeconds is the process's own CPU in the phase, user and system.
func (phase phaseTiming) CpuSeconds() float64 { return phase.UserSeconds + phase.SystemSeconds }

// add is phase and other summed.
func (phase phaseTiming) add(other phaseTiming) phaseTiming {
	return phaseTiming{
		WallSeconds:        phase.WallSeconds + other.WallSeconds,
		UserSeconds:        phase.UserSeconds + other.UserSeconds,
		SystemSeconds:      phase.SystemSeconds + other.SystemSeconds,
		ChildrenCpuSeconds: phase.ChildrenCpuSeconds + other.ChildrenCpuSeconds,
	}
}

// measure runs phase and is how long it took.
func measure(phase func()) phaseTiming {
	started := sampleResources()
	phase()
	return started.since()
}

// unmeasured runs phase and measures nothing, for the profiled repeats: a getrusage around each package would cost
// a share of a small package's check, and on darwin the profiler's ticks land on the getrusage that reads them.
func unmeasured(phase func()) phaseTiming {
	phase()
	return phaseTiming{}
}

// wallOnly runs phase and measures its wall alone, for a job of a parallel schedule: the process's CPU over one job is
// every job's running beside it, so only the schedule as a whole is measured for CPU.
func wallOnly(phase func()) phaseTiming {
	started := time.Now()
	phase()
	return phaseTiming{WallSeconds: time.Since(started).Seconds()}
}
