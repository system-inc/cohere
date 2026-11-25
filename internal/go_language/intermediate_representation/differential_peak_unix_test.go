//go:build unix

package intermediate_representation_test

import (
	"fmt"
	"runtime"
	"syscall"
)

// differentialPeakResident is the test process's peak resident set so far, as getrusage reports it, or unknown
// when it can't be read.
func differentialPeakResident() string {
	var usage syscall.Rusage
	if usageError := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); usageError != nil {
		return "unknown"
	}
	// Darwin reports the peak resident set in bytes, Linux in kibibytes.
	peakBytes := usage.Maxrss
	if runtime.GOOS == "linux" {
		peakBytes *= 1024
	}
	return fmt.Sprintf("%.0f MiB", float64(peakBytes)/(1<<20))
}
