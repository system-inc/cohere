// Package lonely imports nothing, so a rule's Closure lookup of hash.Hash, which every package records, is all it reads
// of another package.
package lonely

// One is one.
func One() int { return 1 }
