// Package hasher imports hash, so the build lists it.
package hasher

import "hash"

// Sum is what hasher sums into.
func Sum(into hash.Hash) []byte { return into.Sum(nil) }
