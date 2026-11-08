// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation and the same environment. The
// original flattens the argument through phis and then switches on the one value left, binding a name to it in each
// case and reporting outside the switch with the value before it was bound; the port carries the value in a struct.
// They could disagree where the related entry points, at an extract or at the call it reads, and where phis form a
// cycle that flattening must cut.
package differences

import (
	"os"
	"path/filepath"
)

// Found by both: the related entry of a two-result call.
func twoResults() {
	home, homeError := os.UserHomeDir()
	if homeError == nil {
		os.RemoveAll(home)
	}
}

// Found by both: phis in nested loops that reach one another and the one call, the variable given back the directory
// a second variable saved at each level, so the phis form a cycle flattening must cut.
func nestedLoops(rows int, columns int) {
	cache, _ := os.UserCacheDir()
	saved := cache
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			if column > 2 {
				cache = saved
			}
		}
		if row > 2 {
			cache = saved
		}
	}
	os.RemoveAll(cache)
}

// Found by neither: the same cycle, broken by a value merged in.
func nestedLoopsMerged(rows int, columns int) {
	cache, _ := os.UserCacheDir()
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			cache = filepath.Join(cache, "level")
		}
	}
	os.RemoveAll(cache)
}

// Found by both: the directory passed straight in, so the related entry and the finding share a line.
func sameLine() {
	defer os.RemoveAll(os.TempDir())
}
