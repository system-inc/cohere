package type_predicates

import "go/types"

// OnlyBasicValues is whether a value of valueType holds only basic values: a basic type, or a struct whose fields,
// unexported ones too, all are. function_purity asks it of each parameter of the function it judges, and the restore
// from the host renders it of another package's unexported field (see Observe).
func OnlyBasicValues(valueType types.Type) bool {
	switch underlying := valueType.Underlying().(type) {
	case *types.Basic:
		return true
	case *types.Struct:
		for field := range underlying.Fields() {
			if !OnlyBasicValues(field.Type()) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
