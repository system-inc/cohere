// Package u imports h alone, and copies the Outer h's Make returns, which on windows copies Inner's lock.
package u

import "example.com/walksquare/h"

// Copy copies what Make returns.
func Copy() any {
	value := *h.Make()
	return value
}
