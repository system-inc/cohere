package program

import "time"

// DiskTiming is filesystem calls by kind, each a count and the time summed across every goroutine
// that made one.
type DiskTiming struct {
	Reads     DiskCalls // ReadFile
	Existence DiskCalls // FileExists and DirectoryExists
	Stats     DiskCalls // Stat
	Listings  DiskCalls // GetAccessibleEntries, a directory listing
	Realpaths DiskCalls // Realpath
}

// Summed is every call's duration, added up.
func (timing DiskTiming) Summed() time.Duration {
	return timing.Reads.Summed + timing.Existence.Summed + timing.Stats.Summed + timing.Listings.Summed +
		timing.Realpaths.Summed
}

func (timing DiskTiming) plus(other DiskTiming) DiskTiming {
	sum := func(left DiskCalls, right DiskCalls) DiskCalls {
		return DiskCalls{Count: left.Count + right.Count, Summed: left.Summed + right.Summed}
	}
	return DiskTiming{
		Reads:     sum(timing.Reads, other.Reads),
		Existence: sum(timing.Existence, other.Existence),
		Stats:     sum(timing.Stats, other.Stats),
		Listings:  sum(timing.Listings, other.Listings),
		Realpaths: sum(timing.Realpaths, other.Realpaths),
	}
}
