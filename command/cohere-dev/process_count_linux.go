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
