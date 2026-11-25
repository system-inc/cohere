// Package foreign is another package's code, which promotedmethods imports and the test never analyzes. Vault has
// Store only by promotion, from an embedded field whose type is an interface no named type declares, so no package of
// the closure declares Store on any type.
package foreign

// storing is an alias of an interface written in place, which a struct may embed by its name.
type storing = interface {
	Store(value any)
}

// Vault is a concrete type whose method set holds Store, promoted from its embedded storing.
type Vault struct {
	storing
}
