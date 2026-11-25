// Package filepath is a package named filepath at another path: its WalkDir and Glob are not the ones the rule knows.
package filepath

import "io/fs"

// WalkDir has the standard library's name and signature, and walks nothing.
func WalkDir(root string, visit fs.WalkDirFunc) error { return nil }

// Glob has the standard library's name and signature, and matches nothing.
func Glob(pattern string) ([]string, error) { return nil, nil }
