// Package generic declares a generic every build declares alike.
package generic

// Identity is its argument.
func Identity[Type any](value Type) Type { return value }
