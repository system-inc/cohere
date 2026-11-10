package react_conformance

// UnsupportedError is what an Implementation returns for a fixture it cannot handle.
type UnsupportedError struct{ Reason string }

func (unsupported *UnsupportedError) Error() string { return "unsupported: " + unsupported.Reason }
