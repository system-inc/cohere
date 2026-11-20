// Package q imports p, so windows holds plain p by its ID beside p's test variant, and builds q again for p's tests,
// against the variant.
package q

import "example.com/audit/p"

// F takes a p.W and does nothing with it.
func F(p.W) {}
