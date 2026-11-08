// Package differences holds the expressions where the port's test of syntax could most plausibly part from the
// patterns: parentheses nested at every level, a cgo name behind parentheses, a dereference of a call or of a
// conversion, and a chain of the two operators. The port and the original find the same here, so the fixture records
// no difference.
package differences

type record struct{}

func use(_ any) {}

func pointerTo() *record { return &record{} }

func parenthesized(value *record, plain record) {
	use(&(*(value)))
	use(*(&(plain)))
	_Cvar_name := value
	use(&*(_Cvar_name))
	use(&*pointerTo())
	use(&*(*record)(value))
	use(*&*&*&plain)
}
