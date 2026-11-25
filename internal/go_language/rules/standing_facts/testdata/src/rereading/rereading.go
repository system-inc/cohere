// Package rereading is the reader's A and C (#xeafx79, round 2's S2): a flow into a key that only a body records, and
// a body that fixes what it feeds from, with nothing else in the package to make the bound's first round grow.
package rereading

import "slices"

// L is []any by another name, so y is no candidate.
type L = []any

// held is a key that Assigns gives y, so y feeds it.
var held []any

var y L

// Assigns gives held y, at its own type, an edge only its body records.
func Assigns() { held = y }

// Reclones gives y another package's clone of it, which fixes y, and so held.
func Reclones() { y = slices.Clone(y) }
