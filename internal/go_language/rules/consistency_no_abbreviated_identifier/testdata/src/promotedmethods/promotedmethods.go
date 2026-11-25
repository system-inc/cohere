// Package promotedmethods declares an interface method with the signature of the Cmd foreign.Runner has by promotion
// alone, and a concrete type with the same Cmd. The rule reads a concrete type's declared methods only, and Runner
// declares none, so neither Cmd is fixed.
package promotedmethods

import "promotedmethods/foreign"

type commanding interface {
	Cmd() string // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`
}

var _ commanding = foreign.Runner{}

type console struct{}

func (console) Cmd() string { return "" } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`
