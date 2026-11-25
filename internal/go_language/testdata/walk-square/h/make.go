// Package h makes s2's Outer, and imports nothing of s1.
package h

import "example.com/walksquare/s2"

// Make is a new Outer.
func Make() *s2.Outer { return &s2.Outer{} }
