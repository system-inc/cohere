//go:build !windows

package p

// Helper is an int everywhere but windows. Its declaration sits under a line directive, as a generator's output does,
// so a reader sees it in gen.tmpl, a file no build lists, while only the host compiles the file that holds it.
//
//line gen.tmpl:1
func Helper() int { return 1 }
