package check

import "github.com/system-inc/go-types/types"

// methodSetEmbedded is one type the method set's walk reaches at a depth, go/types' embeddedType (lookup.go:279 at
// go1.27) without its index path, which only a selection reads: the type, whether a pointer was followed on the way
// to it, and whether it appears more than once at its depth, every name it brings then a collision.
type methodSetEmbedded struct {
	embedded  types.Type
	indirect  bool
	multiples bool
}
