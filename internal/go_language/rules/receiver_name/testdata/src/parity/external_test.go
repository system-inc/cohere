package parity_test

// An external test package's types are its own members, judged in it.
type externalType struct{}

func (_ *externalType) external() {}
