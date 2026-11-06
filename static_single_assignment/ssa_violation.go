package static_single_assignment

// SSAViolation is one broken invariant.
type SSAViolation struct {
	// Kind is which invariant broke.
	Kind SSAViolationKind
	// Identifier is the value involved.
	Identifier IdentifierId
	// Block is where the violating use or duplicate definition sits.
	Block BlockId
	// Detail is a human-readable explanation naming both ends.
	Detail string
}

func (violation SSAViolation) String() string { return violation.Detail }
