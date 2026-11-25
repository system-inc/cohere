// Package far declares a function for near to call across the package boundary, whose body calls on into deep, a
// package near never imports.
package far

import "cut/deep"

// Nothing is trivial where its body and deep's can be read.
func Nothing() error { return deep.Value() }
