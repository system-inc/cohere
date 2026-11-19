//go:build !windows

package p

// Helper is an int everywhere but windows. Its declaration sits under a line directive naming shared.go, as a
// generator's output may name any file, so a reader sees it in a file both builds compile, while only the host compiles
// the file that holds it.
//
//line shared.go:1
func Helper() int { return 1 }
