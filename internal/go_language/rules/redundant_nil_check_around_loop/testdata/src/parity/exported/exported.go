// Package exported declares package variables the parity fixture reaches both through a dot import and through the
// package's name, so the one object is spelled two ways.
package exported

// Values is ranged over by name where the condition checks it through the dot import.
var Values []int

// Counts is a map the parity fixture checks and ranges over the same two ways.
var Counts map[string]int
