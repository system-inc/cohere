package uses

import (
	"math/rand"

	"example.com/differences/library"
)

// localOld is the package's own deprecated type.
//
// Deprecated: use another.
type localOld struct{}

type holder struct{}

// Deprecated: a deprecated method may use deprecated objects, as a deprecated function may.
func (holder) deprecatedMethod() int { return library.OldFunction() }

// Found by the port alone: a package-level variable after a deprecated method is no part of it.
var afterMethod = library.RetiredFunction()

// Deprecated: a deprecated function before a variable whose initializer is a function literal.
func deprecatedBeforeLiteral() int { return 0 }

// Found by the port alone: a function literal in a package-level variable after a deprecated function is no part of
// that function either.
var literalAfter = func() int { return library.RetiredFunction() }

// Found by both: a deprecated object of the standard library, in a function that is not deprecated, under Go 1.22.
func standard() { rand.Seed(1) }

// Found by neither: a package may use its own deprecated type.
var local localOld

// Found by both, once: the one spec of a group whose doc deprecates it, and not its sibling.
func grouped() int { return library.GroupedOld + library.GroupedNew }
