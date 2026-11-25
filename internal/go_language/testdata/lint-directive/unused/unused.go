// Package unused declares one unexported function nothing calls, which U1000 reports unless a //lint:ignore above it
// keeps it.
package unused

// kept is the function nothing calls.
func kept() {}
