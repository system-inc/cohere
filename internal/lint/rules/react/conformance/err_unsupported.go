package react_conformance

// ErrUnsupported is what an Implementation returns for a fixture it cannot handle.
type ErrUnsupported struct{ Reason string }

func (unsupported *ErrUnsupported) Error() string { return "unsupported: " + unsupported.Reason }
