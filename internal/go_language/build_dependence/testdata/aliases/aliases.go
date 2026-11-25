package aliases

// Target is a shared type that gains a method in linux's file only, declared there on Alias.
type Target struct {
	Value int
}

// Alias names Target, so a method declared on it is Target's, and its bits are Target's.
type Alias = Target

// Boxed converts a Target to an interface, which carries its method set: dependent.
func Boxed() any { return Target{} }

// Other is a shared type no platform's file touches.
type Other struct {
	Value int
}

// Others converts an Other to an interface: independent.
func Others() any { return Other{} }
