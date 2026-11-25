//go:build !windows && !freebsd

package p

// Helper is an int everywhere but windows and freebsd, under a line directive naming shared.go, so a reader sees it in
// a file every build compiles, while only the host compiles the file that holds it.
//
//line shared.go:1
func Helper() int { return 1 }
