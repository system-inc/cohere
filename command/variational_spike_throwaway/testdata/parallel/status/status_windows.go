// Package status reads a file's size through the standard library's os, as windows has no Stat_t.
package status

import "os"

// Size is path's size in bytes.
func Size(path string) (int64, error) {
	info, statError := os.Stat(path)
	if statError != nil {
		return 0, statError
	}
	return info.Size(), nil
}

// Mode is path's mode bits as the build spells them.
func Mode(path string) (uint32, error) {
	info, statError := os.Stat(path)
	if statError != nil {
		return 0, statError
	}
	return uint32(info.Mode()), nil
}
