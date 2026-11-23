//go:build unix

// Package status reads a file's size through the standard library's syscall, whose types differ by build.
package status

import "syscall"

// Size is path's size in bytes.
func Size(path string) (int64, error) {
	var stat syscall.Stat_t
	statError := syscall.Stat(path, &stat)
	return stat.Size, statError
}

// Mode is path's mode bits as the build spells them.
func Mode(path string) (uint32, error) {
	var stat syscall.Stat_t
	statError := syscall.Stat(path, &stat)
	return uint32(stat.Mode), statError
}
