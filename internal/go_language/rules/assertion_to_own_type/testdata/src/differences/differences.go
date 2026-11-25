package differences

import "io"

type Named interface {
	Method()
}

type Wrapper[Element any] interface {
	Unwrap() Element
}

// A Named declared inside a function shadows the package's, a type of its own.
func shadowed(value Named) {
	type Named interface {
		Method()
	}
	_ = value.(Named)
}

// An interface embedding another, and the one it embeds, have different method sets.
func embedded(reader io.Reader, readCloser io.ReadCloser) {
	_ = readCloser.(io.ReadCloser)
	_ = readCloser.(io.Reader)
	_ = reader.(io.ReadCloser)
	_ = readCloser.(interface {
		io.Reader
		io.Closer
	})
	var spelled interface {
		io.Reader
		io.Closer
	}
	_ = spelled.(interface {
		io.Closer
		io.Reader
	})
	_ = spelled.(interface {
		Read([]byte) (int, error)
		Close() error
	})
}

// A generic function's own type, instantiated with its parameter, and a nested instantiation.
func nested[Element any](value Wrapper[Wrapper[Element]], flat Wrapper[Element]) {
	_ = value.(Wrapper[Wrapper[Element]])
	_ = flat.(Wrapper[Element])
	_ = value.(Wrapper[Element])
}

// A function literal's parameter, an assertion inside a closure, and an assertion used as an argument.
func literal() {
	check := func(value Named) bool {
		_, ok := value.(Named)
		return ok
	}
	_ = check
	use(Named(nil).(Named))
}

func use(Named) {}

// The universe's error through an alias of it.
type failure = error

func errors(value error, aliased failure) {
	_ = value.(failure)
	_ = aliased.(error)
}
