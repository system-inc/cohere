package static_single_assignment

// StaticSingleAssignmentViolation is one broken invariant.
type StaticSingleAssignmentViolation struct {
	// Kind is which invariant broke.
	Kind StaticSingleAssignmentViolationKind
	// Identifier is the value involved.
	Identifier IdentifierId
	// Block is where the violating use or duplicate definition sits.
	Block BlockId
	// Detail is a human-readable explanation naming both ends.
	Detail string
}

func (violation StaticSingleAssignmentViolation) String() string { return violation.Detail }
