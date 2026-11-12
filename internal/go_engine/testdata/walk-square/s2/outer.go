// Package s2 holds s1's Inner.
package s2

import "example.com/walksquare/s1"

// Outer holds an Inner.
type Outer struct{ Held s1.Inner }
