// Package values names any for reader's declarations, which spell it by this name so that consistency-no-any's
// presence, which reads its own package's spellings of any, names none of them, and a body using them stays standing.
package values

// Value is any value.
type Value = any
