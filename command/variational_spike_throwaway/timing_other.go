//go:build !unix

package main

import "time"

// resourceSample is the wall clock alone: a build without getrusage (windows) measures no CPU. The driver runs on unix;
// this file is here so the tree it checks, which holds the driver, compiles for every build.
type resourceSample struct {
	wall time.Time
}

// sampleResources reads the clock.
func sampleResources() resourceSample {
	return resourceSample{wall: time.Now()}
}

// since is the wall time from started to now, with no CPU.
func (started resourceSample) since() phaseTiming {
	return phaseTiming{WallSeconds: time.Since(started.wall).Seconds()}
}

// peakResidentBytes is unknown without getrusage.
func peakResidentBytes(string) int64 { return 0 }
