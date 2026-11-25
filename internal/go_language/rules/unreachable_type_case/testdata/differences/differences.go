// Package differences holds the type switches whose reading the port could take apart from the original's: a case
// type in parentheses, an alias whose name the message prints, a clause subsumed by several earlier clauses, which is
// reported once for each, a type parameter as the earlier type, whose underlying type is its constraint, and a clause
// whose short range ends at a colon after a list over several lines. The port and the original find the same here, so
// the fixture records no difference.
package differences

import "io"

type Reader struct{}

func (Reader) Read([]byte) (int, error) { return 0, nil }

func (Reader) Close() error { return nil }

type ReaderAlias = io.Reader

func parenthesized(value any) {
	switch value.(type) {
	case (io.Reader):
	case (Reader):
	}
}

func aliased(value any) {
	switch value.(type) {
	case ReaderAlias:
	case Reader:
	}
}

func repeated(value any) {
	switch value.(type) {
	case io.Reader:
	case io.Closer:
	case Reader:
	}
}

func typeParameter[R io.Reader](value any) {
	switch value.(type) {
	case R:
	case Reader:
	}
}

func multiline(value any) {
	switch value.(type) {
	case io.Reader:
	case int,
		Reader:
	}
}
