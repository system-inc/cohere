// Package q reads p's constant, and once a test adds Drop, which discards p.Other's result, asks p.Other's purity,
// which p's entry, written with only the answers asked, does not hold.
package q

import "example.com/storeanswered/p"

// Limit is p's.
func Limit() int {
	return p.Limit
}
