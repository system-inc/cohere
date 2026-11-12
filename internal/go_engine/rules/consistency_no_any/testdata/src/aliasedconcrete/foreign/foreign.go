// Package foreign is another package's code, which aliasedconcrete imports and the test never analyzes. Store is
// declared by no named type here or anywhere: Vault, an alias of a struct written in place, has it only by promotion,
// and storing, the interface it is promoted from, is an alias too.
package foreign

// storing is an alias of an interface written in place, which a struct may embed by its name.
type storing = interface {
	Store(value any)
}

// Vault is an alias of a concrete type, a struct written in place whose method set holds Store.
type Vault = struct {
	storing
}
