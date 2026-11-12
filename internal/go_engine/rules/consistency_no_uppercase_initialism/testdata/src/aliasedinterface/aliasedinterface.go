// Package aliasedinterface declares HomeURL on a type that implements foreign.Located, an alias of an interface
// written in place and that interface's only name, and on a type that implements nothing. Located fixes the first
// name, and nothing fixes the second.
package aliasedinterface

import "aliasedinterface/foreign"

type place struct{}

func (place) HomeURL() string { return "" }

func (place) Region() string { return "" }

var _ foreign.Located = place{}

type shop struct{}

func (shop) HomeURL() string { return "" } // want `"HomeURL" .* Use "HomeUrl"`
