package parity_test

// An external test package's types are its own members, judged in it.
type externalType struct{}

func (value *externalType) first() {}
func (other externalType) second() {}
