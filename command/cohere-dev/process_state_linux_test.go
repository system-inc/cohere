package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// isZombie reports whether pid has exited and waits only to be reaped: its /proc/<pid>/stat's state, the first field
// after the command in parentheses, is Z.
func isZombie(pid int) bool {
	stat, readError := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if readError != nil {
		return false
	}
	fields := strings.Fields(string(stat[bytes.LastIndexByte(stat, ')')+1:]))
	return len(fields) > 0 && fields[0] == "Z"
}
