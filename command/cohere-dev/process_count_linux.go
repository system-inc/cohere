package main

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processNoun is what the process limit counts here: Linux holds threads to it, every thread of every
// process the account owns.
const processNoun = "threads"

// backstopFactor is how many times the margin the account-wide backstop limit allows (bound.go): Linux counts
// threads against it, and a Go process runs ten or more, so sixteen times the group's margin of processes.
const backstopFactor = 16

// groupProcessCount is how many processes are in a process group, read from each /proc/<pid>/stat, whose fifth
// field is the group. A process that ends between the listing and the read is not counted.
func groupProcessCount(group int) (int, error) {
	entries, readDirectoryError := os.ReadDir("/proc")
	if readDirectoryError != nil {
		return 0, readDirectoryError
	}
	count := 0
	for _, entry := range entries {
		if _, parseError := strconv.Atoi(entry.Name()); parseError != nil {
			continue
		}
		stat, readError := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if readError != nil {
			continue
		}
		// The command sits in parentheses and may hold spaces, so the fields are read after its last ')'.
		closing := bytes.LastIndexByte(stat, ')')
		fields := strings.Fields(string(stat[closing+1:]))
		if len(fields) > 2 && fields[2] == strconv.Itoa(group) {
			count++
		}
	}
	return count, nil
}

// accountProcessCount is how many threads this account's processes run, by real user id, which is what the
// kernel counts against the process limit when one forks or starts a thread.
func accountProcessCount() (int, error) {
	entries, readDirectoryError := os.ReadDir("/proc")
	if readDirectoryError != nil {
		return 0, readDirectoryError
	}
	user := strconv.Itoa(os.Getuid())
	count := 0
	for _, entry := range entries {
		if _, parseError := strconv.Atoi(entry.Name()); parseError != nil {
			continue
		}
		status, readError := os.ReadFile(filepath.Join("/proc", entry.Name(), "status"))
		if errors.Is(readError, fs.ErrNotExist) || errors.Is(readError, os.ErrPermission) {
			// A process that ended between the listing and the read, or one this account can't read, which is
			// not this account's.
			continue
		}
		if readError != nil {
			return 0, readError
		}
		owner, threads := "", 0
		scanner := bufio.NewScanner(bytes.NewReader(status))
		for scanner.Scan() {
			name, value, _ := strings.Cut(scanner.Text(), ":")
			fields := strings.Fields(value)
			if len(fields) == 0 {
				continue
			}
			switch name {
			case "Uid":
				owner = fields[0]
			case "Threads":
				// Ignored: the kernel writes a number here, and a zero is counted as one thread below.
				threads, _ = strconv.Atoi(fields[0])
			}
		}
		if owner == user {
			count += max(threads, 1)
		}
	}
	return count, nil
}
