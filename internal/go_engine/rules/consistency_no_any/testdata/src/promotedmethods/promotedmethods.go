// Package promotedmethods declares Store with the signature foreign.Vault has by promotion alone. The rule reads a
// concrete type's declared methods only, and Vault declares none, so the any is ours.
package promotedmethods

import "promotedmethods/foreign"

type cache struct{}

func (cache) Store(value any) {} // want "as .any."

var _ = foreign.Vault{}
