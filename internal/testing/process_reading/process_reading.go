// Package process_reading is for tests that read a process they started, its command line or how nice it runs, from
// the kernel in this process rather than through ps. macOS's ps and top are setuid, and a land job's sandbox refuses
// to start a setuid program (#5wj23dd), while it lets a process read its own processes through sysctl and /proc.
package process_reading

import "errors"

// ErrNoProcess is what Command wraps when no process holds the id, so a caller can tell a process that has ended from
// one whose command line couldn't be read.
var ErrNoProcess = errors.New("no process holds the id")
