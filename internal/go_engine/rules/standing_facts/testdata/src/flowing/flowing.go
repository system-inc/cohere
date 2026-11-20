// Package flowing is the build's package the noany flow test settles: each of the reader's four shapes (#xeafx79), a
// body that names only a declaration consistency-no-any's flow routes to a key, never the key itself.
package flowing

import (
	"fmt"
	"slices"

	"foreign"
)

// k is a key, and x is defined from it, so a place fixing x fixes k.
var k []any

var x = k

// Prints hands x to a variadic parameter of []any, which fixes x, and so k.
func Prints() { fmt.Println(x...) }

// values' result is a key, and cached is defined from it.
func values() []any { return nil }

var cached = values()

// PrintsCached hands cached on, which fixes values' result.
func PrintsCached() { fmt.Println(cached...) }

// compare is defined from a literal whose parameters are keys.
var compare = func(first any, second any) int { return 0 }

// item is any by another name, so a slice of it is no candidate.
type item = any

// Sorts hands compare to slices.SortFunc, which fixes it, and so the literal.
func Sorts(items []item) { slices.SortFunc(items, compare) }

// L is []any by another name, so g's parameter is no candidate.
type L = []any

// kept is a key that g's parameter feeds.
var kept []any

// g gives kept its parameter, at its own type.
func g(parameter L) { kept = parameter }

// Calls hands g another package's values, which fixes g's parameter, and so kept.
func Calls() { g(foreign.Values()) }

// Plain touches nothing the flow routes to a key.
func Plain() int { return 1 }

// P is written L, so it is no candidate, and Local's own local, written []any, is fed by it.
var P L

// Clones gives P another package's clone of it, which fixes P, and so the local P feeds.
func Clones() { P = slices.Clone(P) }

// Local writes a candidate of its own, which P feeds.
func Local() int {
	var held []any = P
	return len(held)
}

// Getter's Get is an interface method key, its result written any.
type Getter interface{ Get() any }

// source is a Getter, and fetch is defined from its Get, so a place fixing fetch fixes Get.
var source Getter

var fetch = source.Get

// getting is a function of Get's type by another name, so a parameter of it is no candidate.
type getting = func() item

// call takes a function of Get's type.
func call(function getting) bool { return function != nil }

// Fetches hands fetch to call, whose parameter's type is no interface, which fixes fetch, and so Get.
func Fetches() bool { return call(fetch) }
