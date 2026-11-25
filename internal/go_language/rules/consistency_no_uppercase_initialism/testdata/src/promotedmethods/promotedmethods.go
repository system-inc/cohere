// Package promotedmethods declares HomeURL on a type that implements foreign.Located, whose method set holds HomeURL
// by an embedding alone, and on a type that implements nothing, beside the HomeURL foreign.Runner has by promotion.
// The rule reads an interface's whole method set and never a concrete type's, so Located fixes the first name and
// nothing fixes the second.
package promotedmethods

import "promotedmethods/foreign"

type place struct{}

func (place) HomeURL() string { return "" }

func (place) Region() string { return "" }

var _ foreign.Located = place{}

type shop struct{}

func (shop) HomeURL() string { return "" } // want `"HomeURL" .* Use "HomeUrl"`

var _ = foreign.Runner{}
