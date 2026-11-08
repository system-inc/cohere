package parity_test

// An external test package's functions are its own.
func external() (error, int) { return nil, 0 }

var _ = external
