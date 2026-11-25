// Package paired writes a Pair's fields in order, unkeyed.
package paired

import "example.com/fieldwalks/platform"

// Make is a Pair.
func Make() *platform.Pair { return &platform.Pair{1, 2} }
