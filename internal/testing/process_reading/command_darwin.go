package process_reading

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// processEntry is pid's entry in the process table, the kern.proc.pid sysctl, wrapping ErrNoProcess when no process
// holds the id: the table answers for one with no entry at all, which x/sys's own decoder reports only as an I/O
// error, so the length is read first.
func processEntry(pid int) (*unix.KinfoProc, error) {
	raw, sysctlError := unix.SysctlRaw("kern.proc.pid", pid)
	if sysctlError != nil {
		return nil, fmt.Errorf("reading process %d from the process table: %w", pid, sysctlError)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("reading process %d from the process table: %w", pid, ErrNoProcess)
	}
	process, tableError := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if tableError != nil {
		return nil, fmt.Errorf("reading process %d from the process table: %w", pid, tableError)
	}
	return process, nil
}

// Command is pid's command line, its arguments as it was started with them, read from the kernel with the
// kern.procargs2 sysctl: the argument count as a 32-bit integer, the executable's path and the padding after it, then
// each argument, every one ending in a zero byte, and the environment after them, which Command never reads.
func Command(pid int) ([]string, error) {
	raw, sysctlError := unix.SysctlRaw("kern.procargs2", pid)
	if sysctlError != nil {
		if _, entryError := processEntry(pid); errors.Is(entryError, ErrNoProcess) {
			return nil, fmt.Errorf("reading the command line of process %d: %w", pid, ErrNoProcess)
		}
		return nil, fmt.Errorf("reading the command line of process %d: %w", pid, sysctlError)
	}
	if len(raw) < 4 {
		return nil, fmt.Errorf("the command line of process %d is %d bytes, too short to hold its count", pid, len(raw))
	}
	count := int(binary.LittleEndian.Uint32(raw[:4]))
	rest := raw[4:]
	// The executable's path, then the zero bytes that pad it.
	if end := bytes.IndexByte(rest, 0); end >= 0 {
		rest = rest[end:]
	}
	rest = bytes.TrimLeft(rest, "\x00")
	arguments := make([]string, 0, count)
	for len(arguments) < count {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			return nil, fmt.Errorf("the command line of process %d holds %d of its %d arguments", pid, len(arguments),
				count)
		}
		arguments = append(arguments, string(rest[:end]))
		rest = rest[end+1:]
	}
	return arguments, nil
}
