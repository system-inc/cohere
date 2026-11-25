// Package other declares interfaces in a second package, so parity asserts across a package boundary, where an
// unexported method of one package is never the same method as one of the same name in another.
package other

// Hidden holds an unexported method, which only this package's interfaces share.
type Hidden interface {
	hidden()
}

// Exported holds an exported method whose signature differs from io.Reader's.
type Exported interface {
	Read()
}

// Getter is generic in what it gets.
type Getter[Value any] interface {
	Get() Value
}
