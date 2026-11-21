package p_test

import (
	"example.com/audit/p"
	"example.com/audit/q"
)

// F takes a p.W, which on windows is the test variant's, with M in its method set, and hands it to q's F.
func F(x p.W) { q.F(x) }
