package flagged

// An empty method with a marker's shape that is no sealed union's marker still says why it is empty.

// closer has a marker's shape, but its method is called, so an empty close is a no-op that wants a reason.
type closer interface {
	close()
}

type handle struct{}

func (*handle) close() {} // want "body is empty"

func release(value closer) {
	value.close()
}

// An exported method seals nothing, since a type in any package can declare it.
type Marker interface {
	Mark()
}

type exported struct{}

func (exported) Mark() {} // want "body is empty"

// A method that takes something is an operation, whatever interface asks for it.
type acceptor interface {
	accept(visitor int)
}

type visited struct{}

func (visited) accept(visitor int) {} // want "body is empty"

// An interface with a second method asks for behavior, so neither empty method is a marker.
type shape interface {
	area()
	perimeter()
}

type square struct{}

func (square) area() {} // want "body is empty"

func (square) perimeter() {} // want "body is empty"

// Nor is one that embeds another.
type labeled interface {
	shape
	label()
}

func (square) label() {} // want "body is empty"

// A method that only shares a marker's name, on a type that does not implement the marker's interface, marks nothing.
type kinded interface {
	kind() int
}

type unkinded struct{}

func (unkinded) kind() {} // want "body is empty"

// A sealed marker taken as a method value is used, so its empty body runs for whoever calls that value. Taking it
// from one variant uses the membership every variant declares.
type statement interface {
	statement()
}

type assignment struct{}

type jump struct{}

func (*assignment) statement() {} // want "body is empty"

func (*jump) statement() {} // want "body is empty"

func markerOf(value *jump) func() {
	marker := value.statement
	return marker
}
