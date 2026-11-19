// Package mid hands indirect a value of a type deep declares, so indirect holds the type without importing deep.
package mid

import "indirect/deep"

// New is a Core with no failure.
func New() *deep.Core { return &deep.Core{} }
