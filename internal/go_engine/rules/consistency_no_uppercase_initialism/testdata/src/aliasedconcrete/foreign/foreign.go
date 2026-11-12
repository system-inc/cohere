// Package foreign is another package's code, which aliasedconcrete imports and the test never analyzes.
package foreign

// base declares HomeURL on a concrete type.
type base struct{}

func (base) HomeURL() string { return "" }

// Runner is an alias of base, a concrete type, and the only name another package can reach it by.
type Runner = base
