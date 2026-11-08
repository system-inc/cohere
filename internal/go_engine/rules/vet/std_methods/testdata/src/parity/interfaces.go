package parity

import (
	"fmt"
	"io"
)

// Interface methods are judged as declared ones are.
type reader interface {
	ReadByte() byte                  // reported
	ReadRune() (rune, int, error)    // canonical
	Scan(fmt.ScanState) error        // reported
	Seek(offset, whence int64) error // reported: its signal, the first int64, matches
	io.Reader                        // embedded, and declares nothing here
}

// An interface that is not an error leaves its Is, As and Unwrap alone.
type notAnErrorInterface interface {
	Is(int) int
	As() string
	Unwrap() int
}

// An interface that implements error, by its own method or by embedding error, has them judged.
type errorInterface interface {
	Error() string
	Is() bool
	As(any) bool
	Unwrap() int
}

type embeddingError interface {
	error
	Unwrap() []error
	As(int) bool
}

// An interface written in a function's body, in a variable's type, in a parameter and in a result.
func interfaces(parameter interface{ GobDecode() }) interface{ GobEncode() int } {
	type local interface {
		MarshalJSON() []byte
	}
	var variable interface {
		UnmarshalJSON([]byte) error
		WriteByte(int) error
	}
	_ = variable
	return nil
}

// A struct field of interface type, and an interface in a composite literal's type.
type holder struct {
	field interface{ WriteTo(io.Writer) int }
}

var held = []interface{ ReadFrom(io.Reader) int }{}

// A generic interface, whose results are type parameters.
type genericReader[T any] interface {
	ReadByte() (T, error)
	UnreadByte() T
}

// A constraint with a type union beside a method, and one written inline in a type parameter list.
type constraint interface {
	~int | ~string
	ReadByte() (byte, error)
	WriteByte(rune) error
}

func constrained[T interface{ UnreadRune() bool }, U constraint](first T, second U) {}

// grouped and the interfaces above are used, so nothing reads them as dead.
var _ = []any{(*reader)(nil), (*notAnErrorInterface)(nil), (*errorInterface)(nil)}
var _ = []any{(*embeddingError)(nil), holder{}, held, (*genericReader[int])(nil)}
