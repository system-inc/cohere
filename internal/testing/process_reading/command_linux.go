package process_reading

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

// Command is pid's command line, its arguments as it was started with them, read from /proc/<pid>/cmdline, where
// each ends in a zero byte.
func Command(pid int) ([]string, error) {
	raw, readError := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if errors.Is(readError, fs.ErrNotExist) {
		return nil, fmt.Errorf("reading the command line of process %d: %w", pid, ErrNoProcess)
	}
	if readError != nil {
		return nil, fmt.Errorf("reading the command line of process %d: %w", pid, readError)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("process %d has no command line, as an exited one has", pid)
	}
	return strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00"), nil
}
