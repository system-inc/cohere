package load

import (
	"slices"
	"strings"
)

// dependencyMerge makes each package's Deps, every package it reaches, by import path, sorted, as go list prints them,
// bottom up: a package's Deps is what it walks to, merged with each of their Deps. Each is computed once and read by
// every importer, where a walk from each package would visit everything below it again (#zcc03r6). It holds two
// buffers it merges between, reused from one package to the next, so the only allocation a package costs is its own
// Deps.
type dependencyMerge struct {
	merged  []string
	spare   []string
	ordered []string
}

// deps is current's Deps, from the Deps of the packages it walks to, which must already be set: order puts each
// package after everything it walks to, and a package an earlier order put has its Deps from that pass. It is nil when
// current walks to nothing, as go list omits an empty Deps.
//
// A package current walks to that is already in another's Deps is not merged: everything it reaches, that other
// reaches too, so its Deps are inside the other's, which is merged. The other is the one with the most Deps, which
// holds the most of current's walks, and it is never itself passed over, since order refuses a cycle, so no Deps
// holds its own package. The answer is the union either way; a package passed over costs a search of the most Deps
// in place of a merge through all of them.
func (merging *dependencyMerge) deps(current *inProcessPackage, byPath map[string]*inProcessPackage) []string {
	if len(current.walked) == 0 {
		return nil
	}
	merging.ordered = append(merging.ordered[:0], current.walked...)
	slices.Sort(merging.ordered)
	merging.merged = append(merging.merged[:0], slices.Compact(merging.ordered)...)
	var mostDeps []string
	for _, walked := range current.walked {
		if below := byPath[walked].listed.Deps; len(below) > len(mostDeps) {
			mostDeps = below
		}
	}
	for _, walked := range current.walked {
		below := byPath[walked].listed.Deps
		if len(below) == 0 {
			continue
		}
		// Every Deps is sorted, as this merge makes it, so the search is a binary one.
		if _, covered := slices.BinarySearch(mostDeps, walked); covered {
			continue
		}
		merging.spare = mergeSorted(merging.spare[:0], merging.merged, below)
		merging.merged, merging.spare = merging.spare, merging.merged
	}
	return slices.Clone(merging.merged)
}

// mergeSorted is into with the union of left and right appended, each sorted with no repeats, the union sorted with
// no repeats.
func mergeSorted(into []string, left []string, right []string) []string {
	merged := into
	leftIndex, rightIndex := 0, 0
	for leftIndex < len(left) && rightIndex < len(right) {
		switch strings.Compare(left[leftIndex], right[rightIndex]) {
		case -1:
			merged = append(merged, left[leftIndex])
			leftIndex++
		case 1:
			merged = append(merged, right[rightIndex])
			rightIndex++
		default:
			merged = append(merged, left[leftIndex])
			leftIndex++
			rightIndex++
		}
	}
	merged = append(merged, left[leftIndex:]...)
	return append(merged, right[rightIndex:]...)
}
