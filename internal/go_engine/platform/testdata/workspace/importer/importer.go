// Package importer imports seed, whose export differs.
package importer

import "example.com/workspace/seed"

// Twice is seed's value twice.
func Twice() int { return seed.Value() * 2 }
