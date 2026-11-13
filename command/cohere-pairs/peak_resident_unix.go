//go:build unix

package main

import (
	"os"
	"runtime"
	"syscall"
)

// peakResidentBytes is a finished process's peak resident set in bytes, from its rusage, whose Maxrss is kilobytes on
// linux and bytes on darwin; 0 when the state carries no rusage.
func peakResidentBytes(state *os.ProcessState) int64 {
	usage, isRusage := state.SysUsage().(*syscall.Rusage)
	if !isRusage || usage == nil {
		return 0
	}
	if runtime.GOOS == "linux" {
		return usage.Maxrss * 1024
	}
	return usage.Maxrss
}
