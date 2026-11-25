// Package parity holds every branch of ifaceassert's judgment beside a clean case, and the shapes most likely to
// differ between the original and its port. The comment on a line says what the original finds there; the parity
// test holds the port to the same.
package parity

import (
	"fmt"
	"io"
	"strings"

	"parity/other"
)

// Reader holds a Read whose signature differs from io.Reader's.
type Reader interface {
	Read()
}

// Pair holds two methods whose signatures differ from Conflicting's.
type Pair interface {
	First()
	Second()
}

// Conflicting conflicts with Pair in both methods, so the first of them is the one named.
type Conflicting interface {
	First() int
	Second() int
}

// Embeds holds Reader's Read through embedding.
type Embeds interface {
	Reader
	io.Closer
}

// ReaderAlias and LocalAlias are aliases, which the message names as written.
type ReaderAlias = io.Reader

type LocalAlias = interface{ Read() }

// hiddenHere holds a method of the same name as other.Hidden's, which, unexported in another package, is another
// method.
type hiddenHere interface {
	hidden() int
}

var globalReader Reader

// An assertion in a package-level initializer.
var _, _ = globalReader.(io.Reader) // finding: Read

// An assertion in a closure a package-level initializer calls.
var _ = func() bool {
	_, isReader := globalReader.(io.Reader) // finding: Read
	return isReader
}()

func Plain(reader io.Reader, local Reader, value any, stringer fmt.Stringer, pair Pair) {
	_ = local.(io.Reader)                                 // finding: Read
	_ = reader.(Reader)                                   // finding: Read, the other way
	_ = local.(interface{ Read(p []byte) (int, error) })  // finding: Read, against a literal interface
	_ = reader.(interface{ Read(b []byte) (int, error) }) // clean: the same signature, its names aside
	_ = reader.(io.ReadCloser)                            // clean: Close is missing, not conflicting
	_ = local.(interface{ Write() })                      // clean: no method in common
	_ = local.(any)                                       // clean
	_ = local.(error)                                     // clean
	_ = value.(Reader)                                    // clean: any holds no method
	_ = value.(int)                                       // clean: the target is no interface
	_ = reader.(*strings.Reader)                          // clean: the target is no interface
	_, isReader := local.(io.Reader)                      // finding: Read, with its second result
	_ = isReader
	_ = (local).(io.Reader)                     // finding: Read, the asserted value in parentheses
	_ = local.((io.Reader))                     // finding: Read, at the target's opening parenthesis
	_ = stringer.(interface{ String() int })    // finding: String
	_ = stringer.(interface{ String() string }) // clean
	_ = pair.(Conflicting)                      // finding: First, the first of two
	_ = pair.(interface {                       // finding: Second, the only one, at the target's start
		Second() int
		First()
	})
}

func Embedded(embeds Embeds, closer io.Closer) {
	_ = embeds.(io.ReadCloser)        // finding: Read, held through embedding
	_ = closer.(interface{ Close() }) // finding: Close
	_ = closer.(interface {           // clean: Close agrees, and Read is missing, not conflicting
		io.Closer
		Reader
	})
}

func Aliases(aliased ReaderAlias, localAliased LocalAlias) {
	_ = aliased.(Reader)         // finding: Read, the asserted value's type an alias
	_ = localAliased.(io.Reader) // finding: Read
	_ = aliased.(LocalAlias)     // finding: Read, the target an alias
	_ = aliased.(ReaderAlias)    // clean
}

func AcrossPackages(hidden other.Hidden, exported other.Exported, reader io.Reader) {
	_ = hidden.(hiddenHere)            // clean: two methods named hidden, in two packages
	_ = exported.(io.Reader)           // finding: Read
	_ = reader.(other.Exported)        // finding: Read
	_ = exported.(Reader)              // clean
	_ = hidden.(interface{ hidden() }) // clean: this package's hidden, which other.Hidden lacks
}

func Switches(local Reader, reader io.Reader, value any) {
	switch local.(type) {
	case io.Reader, io.Writer, Reader: // finding: Read, on io.Reader alone
	case nil:
	case interface{ Read() int }: // finding: Read
	default:
	}

	switch named := reader.(type) {
	case Reader: // finding: Read
		_ = named
	case io.ReadCloser:
		_ = named
	}

	// An empty switch, and one with an initializer.
	switch reader.(type) {
	}
	switch first := 1; (local).(type) {
	case io.Reader: // finding: Read
		_ = first
	}

	// A switch on a call's result, and on an any, whose cases conflict with nothing.
	switch makeReader().(type) {
	case io.Reader: // finding: Read
	}
	switch value.(type) {
	case io.Reader, Reader, int:
	}

	// A switch nested in a case of another, each judged on its own value.
	switch local.(type) {
	case io.Writer:
		switch reader.(type) {
		case Reader: // finding: Read
		}
	}
}

func makeReader() Reader {
	return globalReader
}

// holder reaches an interface through a field, a method, a method value and an embedded struct.
type holder struct {
	local Reader
}

func (receiver holder) assertsInAMethod() {
	_ = receiver.local.(io.Reader) // finding: Read
}

func (receiver holder) get() Reader {
	return receiver.local
}

type outer struct {
	holder
}

func Reached(reached outer) func() {
	_ = reached.local.(io.Reader) // finding: Read, through an embedded field
	get := reached.get
	_ = get().(io.Reader)                      // finding: Read, from a method value
	_ = holder.get(reached.holder).(io.Reader) // finding: Read, from a method expression
	return func() {
		_ = reached.get().(io.Reader) // finding: Read, in a closure
		defer func() {
			switch reached.local.(type) {
			case io.Reader: // finding: Read, in a closure within a closure
			}
		}()
	}
}
