package methods

// Plain is a shared type that gains a method in linux's file only.
type Plain struct {
	Value int
}

// Describer is what Plain implements on linux.
type Describer interface {
	Describe() string
}

// Boxed converts a Plain to an interface, which carries its method set: dependent.
func Boxed() any { return Plain{} }

// IsDescriber asks whether a value implements Describer, which reads nothing of Plain: independent.
func IsDescriber(value any) bool {
	_, isDescriber := value.(Describer)
	return isDescriber
}

// Wrapper embeds Plain, so it promotes Plain's methods.
type Wrapper struct {
	Plain
}

// Wrapped hands on a Wrapper by pointer, whose method set Plain's moves: dependent.
func Wrapped(wrapper *Wrapper) *Wrapper { return wrapper }

// Count is a shared type no platform's file touches.
type Count int

// Counted uses Count: independent.
func Counted() Count { return 1 }
