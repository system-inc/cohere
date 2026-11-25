// Package elsewhere is another package's code, which the fixtures import and the test never analyzes: its interfaces
// fix the names of the methods that implement them.
package elsewhere

// Identified is an interface of another package whose method name spells an initialism in capitals.
type Identified interface {
	UserID() string
}

// Located has two methods, so only a type with both implements it.
type Located interface {
	HomeURL() string
	Region() string
}

// Keyed is generic, so no method set satisfies it until it is instantiated.
type Keyed[Key comparable] interface {
	KeyID() Key
}
