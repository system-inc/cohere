// Package aliasedconcrete declares Store with the signature foreign.Vault has. Vault is an alias of a concrete type,
// and an alias is skipped, so the any is ours.
package aliasedconcrete

import "aliasedconcrete/foreign"

type cache struct{}

func (cache) Store(value any) {} // want "as .any."

var _ = foreign.Vault{}
