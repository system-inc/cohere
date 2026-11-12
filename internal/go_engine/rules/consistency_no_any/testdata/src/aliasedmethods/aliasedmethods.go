// Package aliasedmethods declares Store on a concrete type that has all of foreign.Storing and on an interface of its
// own with Storing's exact signature. An alias is skipped, and Storing names no declared type, so neither any is
// fixed.
package aliasedmethods

import "aliasedmethods/foreign"

type cache struct{}

func (cache) Store(value any) {} // want "as .any."

var _ foreign.Storing = cache{}

type storing interface {
	Store(value any) // want "as .any."
}
