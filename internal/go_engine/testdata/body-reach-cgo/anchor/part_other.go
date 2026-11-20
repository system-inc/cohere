//go:build !windows

// Package anchor holds a file of windows's, so the run checks a windows build whatever gate holds.
package anchor

// Name is every platform's but windows's.
func Name() string { return "other" }
