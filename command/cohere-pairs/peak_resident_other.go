//go:build !unix

package main

import "os"

// peakResidentBytes is 0 off unix, whose process state carries no rusage to read a peak from. A pair is measured on
// linux and darwin; this is here so the module builds everywhere it is built.
func peakResidentBytes(*os.ProcessState) int64 {
	return 0
}
