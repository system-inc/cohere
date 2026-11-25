package type_predicates

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// MayHoldFloat is whether a value of valueType may be or hold a float: a float, an array of them, a struct with one in
// any field, unexported ones too, or a type parameter any of whose types is such, or whose types are unrestricted.
// It is staticcheck's SA4000 walk, which that rule asks before judging `x == x`, and the restore from the host renders
// of another package's unexported field (see Observe).
func MayHoldFloat(valueType types.Type) bool {
	typeSet := typeutil.NewTypeSet(valueType)
	if len(typeSet.Terms) == 0 {
		// No terms, so a float is possible.
		return true
	}
	return typeSet.Any(func(term *types.Term) bool {
		switch underlying := term.Type().Underlying().(type) {
		case *types.Basic:
			return underlying.Kind() == types.Float32 || underlying.Kind() == types.Float64
		case *types.Array:
			return MayHoldFloat(underlying.Elem())
		case *types.Struct:
			for field := range underlying.Fields() {
				if MayHoldFloat(field.Type()) {
					return true
				}
			}
			return false
		default:
			return false
		}
	})
}
