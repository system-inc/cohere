package compose

// ScalarString is a JavaScript string as a Scalar's value, WTF-8 where it holds a lone surrogate (see
// the package comment, "Strings"): what the str tag resolves to.
type ScalarString string

func (ScalarString) scalarValue() {}
