package other

// Reader is an interface of another package, asserted to through its package name and through a dot import.
type Reader interface {
	Read() string
}

// Box is a generic interface, instantiated alike and apart by the parity fixture.
type Box[Element any] interface {
	Get() Element
}
