package allowed

// A sealed union's marker is membership, not behavior: the interface's doc says why, once, and each variant's empty
// method only declares that the variant belongs. Nothing calls it; a type switch reads which variant a value is.

// expression is a literal, a reference or a call, and no type outside this package can be one.
type expression interface {
	expression()
}

type literal struct{}

type reference struct{}

type call struct{}

func (*literal) expression() {}

func (reference) expression() {}

func (*call) expression() {}

func describe(value expression) string {
	switch value.(type) {
	case *literal:
		return "literal"
	case reference:
		return "reference"
	default:
		return "call"
	}
}
