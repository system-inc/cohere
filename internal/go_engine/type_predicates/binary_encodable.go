package type_predicates

import "go/types"

// BinaryEncodable is whether encoding/binary.Write can encode a value of judged: a number of fixed size, a bool when
// boolEncodable says the standard library encodes one, as Go 1.8's does, and an array or a struct of encodable values,
// every field judged, unexported ones too. An interface could hold anything, so it is taken as encodable, and so is a
// type that did not type-check. It is staticcheck's SA1003 walk, which that rule asks of the data it is given and the
// restore from the host renders of another package's unexported field, twice, once for each answer of boolEncodable
// (see Observe). boolEncodable is asked only on meeting a bool.
func BinaryEncodable(judged types.Type, boolEncodable func() bool) bool {
	switch underlying := judged.Underlying().(type) {
	case *types.Basic:
		switch underlying.Kind() {
		case types.Uint8, types.Uint16, types.Uint32, types.Uint64,
			types.Int8, types.Int16, types.Int32, types.Int64,
			types.Float32, types.Float64, types.Complex64, types.Complex128, types.Invalid:
			return true
		case types.Bool:
			return boolEncodable()
		}
		return false
	case *types.Struct:
		for index := 0; index < underlying.NumFields(); index++ {
			if !BinaryEncodable(underlying.Field(index).Type(), boolEncodable) {
				return false
			}
		}
		return true
	case *types.Array:
		return BinaryEncodable(underlying.Elem(), boolEncodable)
	case *types.Interface:
		return true
	}
	return false
}
