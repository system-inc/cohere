package main

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

// loadAverage is the host's one-minute load average: the first field of /proc/loadavg on linux, and of
// `sysctl -n vm.loadavg`'s "{ 1.23 1.45 1.67 }" on darwin.
func loadAverage() (float64, error) {
	var described string
	switch runtime.GOOS {
	case "linux":
		contents, readError := os.ReadFile("/proc/loadavg")
		if readError != nil {
			return 0, readError
		}
		described = string(contents)
	case "darwin":
		output, sysctlError := commandOutput("sysctl", "-n", "vm.loadavg")
		if sysctlError != nil {
			return 0, sysctlError
		}
		described = strings.TrimPrefix(strings.TrimSpace(output), "{")
	default:
		return 0, fmt.Errorf("the load average is read on linux and darwin, not %s", runtime.GOOS)
	}
	fields := strings.Fields(described)
	if len(fields) == 0 {
		return 0, fmt.Errorf("the load average read as %q", described)
	}
	return strconv.ParseFloat(fields[0], 64)
}
