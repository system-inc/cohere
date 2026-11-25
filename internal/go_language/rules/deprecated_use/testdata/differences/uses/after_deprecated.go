package uses

import "example.com/differences/library"

// Deprecated: its own use of a deprecated function is left alone.
func deprecatedBefore() int {
	return library.OldFunction()
}

// Found by the port alone: a package-level variable after a deprecated function in its file is no part of that
// function. The original keeps the last function declaration it walked until the next one, so it judges this as if
// inside deprecatedBefore and misses it; the port judges each declaration by its own.
var afterDeprecated = library.RetiredFunction()
