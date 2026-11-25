// Package aliasedconcrete declares HomeURL with the signature foreign.Runner has. Runner is an alias of a concrete
// type, and only an interface fixes a name, so the name is ours.
package aliasedconcrete

import "aliasedconcrete/foreign"

type shop struct{}

func (shop) HomeURL() string { return "" } // want `"HomeURL" .* Use "HomeUrl"`

var _ = foreign.Runner{}
