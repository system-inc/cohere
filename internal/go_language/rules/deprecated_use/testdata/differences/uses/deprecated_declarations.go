package uses

import "example.com/differences/library"

// Found by the original alone: a deprecated constant, variable or type may use another package's deprecated objects,
// as a deprecated function may, the way a deprecated re-export of a deprecated name does. The original exempts only a
// function declaration, so it reports each; the port judges each declaration by its own deprecation.

// Deprecated: re-exports a deprecated constant.
const deprecatedConstant = library.RetiredConstant

// Deprecated: holds a deprecated function's result.
var deprecatedVariable = library.RetiredFunction()

// Deprecated: aliases a deprecated type.
type deprecatedType = library.RetiredType

// Found by both: the same uses in declarations that are not deprecated.
const plainConstant = library.RetiredConstant

var plainVariable = library.RetiredFunction()

type plainType = library.RetiredType
