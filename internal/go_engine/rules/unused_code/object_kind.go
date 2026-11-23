package unused_code

import "github.com/system-inc/go-types/types"

// objectKind is which kind of object a finding names, a byte where a body's record holds one a local (#xeafx79).
type objectKind uint8

// The kinds kindOf tells apart, each worded by kindWords.
const (
	kindFunction objectKind = iota
	kindField
	kindVariable
	kindConstant
	kindTypeParameter
	kindType
	kindIdentifier
)

// kindWords is the word a finding names each kind with, the original's typString's, by the kind.
var kindWords = [...]string{
	kindFunction:      "func",
	kindField:         "field",
	kindVariable:      "var",
	kindConstant:      "const",
	kindTypeParameter: "type param",
	kindType:          "type",
	kindIdentifier:    "identifier",
}

// word is the word a finding names kind with. Every kind kindOf gives has one, and a record's local that names
// another is refused where a splice reads it (spliceBody).
func (kind objectKind) word() string {
	return kindWords[kind]
}

// kindOf is typesObject's kind, the original's typString.
func kindOf(typesObject types.Object) objectKind {
	switch typesObject := typesObject.(type) {
	case *types.Func:
		return kindFunction
	case *types.Var:
		if typesObject.IsField() {
			return kindField
		}
		return kindVariable
	case *types.Const:
		return kindConstant
	case *types.TypeName:
		if _, isParameter := typesObject.Type().(*types.TypeParam); isParameter {
			return kindTypeParameter
		}
		return kindType
	default:
		return kindIdentifier
	}
}
