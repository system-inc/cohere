// Package parity holds the type switches the original and the port must judge alike: interfaces embedding others,
// aliases, the empty interface, pointer and value receivers, structurally identical interfaces, a clause subsumed by
// several before it, a `nil` beside types and alone, a default in every place, case lists over several lines, type
// switches nested, with an initializer, in closures and in generic functions, and type parameters on either side.
package parity

import (
	"errors"
	"fmt"
	"io"

	"example.com/parity/other"
)

type Reader struct{}

func (Reader) Read([]byte) (int, error) { return 0, nil }

type PointerReader struct{}

func (*PointerReader) Read([]byte) (int, error) { return 0, nil }

type ReadCloser struct{ Reader }

func (ReadCloser) Close() error { return nil }

type Stringer interface{ String() string }

type Named struct{}

func (Named) String() string { return "" }

type First error

type Second error

type ReaderAlias = io.Reader

type Wrapper interface {
	error
	Unwrap() error
}

type Local interface{ sealed() }

type localImplementation struct{}

func (localImplementation) sealed() {}

func embedded(value any) {
	switch value.(type) {
	case io.Reader:
	case io.ReadWriter:
	case io.ReadWriteCloser:
	case Reader:
	case *PointerReader:
	case PointerReader:
	}
}

func aliases(value any) {
	switch value.(type) {
	case ReaderAlias:
	case ReadCloser:
	}
	switch value.(type) {
	case Reader:
	case ReaderAlias:
	}
}

func empty(value any, err error) {
	switch value.(type) {
	case any:
	case int, string:
	case func():
	case struct{}:
	}
	switch value.(type) {
	case interface{}:
	case nil, error:
	}
	switch err.(type) {
	case error:
	case Wrapper:
	}
	switch err.(type) {
	case Wrapper:
	case error:
	}
}

func identical(err error, value any) {
	switch err.(type) {
	case First:
	case Second:
	case error:
	}
	switch value.(type) {
	case fmt.Stringer:
	case Stringer:
	case Named:
	}
}

func several(value any) {
	switch value.(type) {
	case io.Reader:
	case io.Closer:
	case io.ReadCloser:
	case ReadCloser:
	}
	switch value.(type) {
	case io.Reader, io.Closer:
	case ReadCloser, io.ReadCloser:
	}
}

func defaults(value any) {
	switch value.(type) {
	case io.Reader:
	default:
	case Reader:
	}
	switch value.(type) {
	case io.Reader:
	case Reader:
	default:
	}
	switch value.(type) {
	default:
	}
	switch value.(type) {
	case nil:
	}
	switch value.(type) {
	case nil:
	case io.Reader:
	}
}

func multiline(value any) {
	switch value.(type) {
	case io.Reader:
	case Named,
		ReadCloser,
		int:
	}
}

func shapes(value any) {
	switch bound := value.(type) {
	case io.Reader:
		_ = bound
	case ReadCloser:
		switch inner := value.(type) {
		case fmt.Stringer:
		case Named:
			_ = inner
		}
	}
	switch value := value; value.(type) {
	case error:
	case Wrapper:
	}
	func() {
		switch value.(type) {
		case any:
		case Reader:
		}
	}()
	_ = func(err error) {
		switch err.(type) {
		case Wrapper:
		case First:
		}
	}
}

func sealed(value any) {
	switch value.(type) {
	case other.Sealed:
	case other.Implementation:
	case Local:
	case localImplementation:
	}
	switch value.(type) {
	case Local:
	case other.Implementation:
	}
}

func typeParameters[T any, R io.Reader, E error](value any) {
	switch value.(type) {
	case T:
	case int:
	}
	switch value.(type) {
	case R:
	case Reader:
	}
	switch value.(type) {
	case io.Reader:
	case R:
	case T:
	}
	switch value.(type) {
	case any:
	case T:
	case E:
	}
	switch value.(type) {
	case error:
	case E:
	}
}

type Box[T any] struct{ Value T }

func (box Box[T]) Read([]byte) (int, error) { return 0, nil }

func (box Box[T]) method(value any) {
	switch value.(type) {
	case io.Reader:
	case Box[T]:
	case Box[int]:
	}
	switch any(box).(type) {
	case T:
	case io.Reader:
	}
}

func instantiated() {
	typeParameters[int, Reader, First](nil)
	Box[string]{}.method(errors.New(""))
}
