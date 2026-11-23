//go:build unix

package main

import (
	"syscall"
	"time"
)

// resourceSample is the process's CPU so far and its children's, read together with the wall clock.
type resourceSample struct {
	wall     time.Time
	self     syscall.Rusage
	children syscall.Rusage
}

// sampleResources reads the clock and the process's resource usage.
func sampleResources() resourceSample {
	sample := resourceSample{wall: time.Now()}
	// Ignored: getrusage fails only for a bad argument, and a zero sample reads as no CPU spent.
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &sample.self)
	_ = syscall.Getrusage(syscall.RUSAGE_CHILDREN, &sample.children)
	return sample
}

// since is the phase from started to now.
func (started resourceSample) since() phaseTiming {
	ended := sampleResources()
	return phaseTiming{
		WallSeconds:   ended.wall.Sub(started.wall).Seconds(),
		UserSeconds:   elapsed(started.self.Utime, ended.self.Utime),
		SystemSeconds: elapsed(started.self.Stime, ended.self.Stime),
		ChildrenCpuSeconds: elapsed(started.children.Utime, ended.children.Utime) +
			elapsed(started.children.Stime, ended.children.Stime),
	}
}

// elapsed is the time from started to ended, counted in whole microseconds so an unchanged clock is exactly zero.
func elapsed(started syscall.Timeval, ended syscall.Timeval) float64 {
	microseconds := (ended.Sec-started.Sec)*1_000_000 + int64(ended.Usec-started.Usec)
	return float64(microseconds) / 1e6
}

// peakResidentBytes is the most memory the process has held resident, which darwin's getrusage gives in bytes and
// every other unix in kilobytes.
func peakResidentBytes(operatingSystem string) int64 {
	var usage syscall.Rusage
	// Ignored: as in sampleResources.
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	if operatingSystem == "darwin" {
		return usage.Maxrss
	}
	return usage.Maxrss * 1024
}
