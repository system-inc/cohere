// Package foreign stands in for a package we do not own, which aliasedconcrete imports and the test never analyzes.
// Cmd is declared by no named type here or anywhere: Runner, an alias of a struct written in place, has it only by
// promotion, and commanding, the interface it is promoted from, is an alias too.
package foreign

// commanding is an alias of an interface written in place, which a struct may embed by its name.
type commanding = interface {
	Cmd() string
}

// Runner is an alias of a concrete type, a struct written in place whose method set holds Cmd.
type Runner = struct {
	commanding
}
