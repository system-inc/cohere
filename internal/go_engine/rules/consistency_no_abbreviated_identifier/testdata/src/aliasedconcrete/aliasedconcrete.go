// Package aliasedconcrete declares an interface method with the signature of the Cmd foreign.Runner has. Runner is an
// alias of a concrete type, and an alias is indexed where the type it names is declared, which for Runner is nowhere,
// so nothing fixes Cmd.
package aliasedconcrete

import "aliasedconcrete/foreign"

type commanding interface {
	Cmd() string // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`
}

var _ commanding = foreign.Runner{}
