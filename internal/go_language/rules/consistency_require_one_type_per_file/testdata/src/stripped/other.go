package stripped

// Other is the one type this file serves, and stripped.go takes a method value of it in a body the strip takes away.
type Other struct{}

func (Other) name() string { return "other" }
