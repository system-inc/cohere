package nil_ness

import "honnef.co/go/tools/go/ir"

// A fact records that a block is dominated by the condition value == nil or value != nil.
type fact struct {
	value   ir.Value
	nilness nilState
}

// negate is the fact the other successor of the comparison that taught this one learns.
func (known fact) negate() fact { return fact{known.value, -known.nilness} }

// negated is each of known negated.
func negated(known []fact) []fact {
	negatedFacts := make([]fact, len(known))
	for index, each := range known {
		negatedFacts[index] = each.negate()
	}
	return negatedFacts
}
