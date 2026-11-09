package staticcheck_call

import (
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"go/constant"
)

// ExtractConstant is the constant value is, seen through phis whose edges are all that one constant and through a
// conversion to an interface, or nil when it is not one constant.
func ExtractConstant(value intermediate_representation.Value) *intermediate_representation.Constant {
	switch flattened := intermediate_representation.Flatten(value).(type) {
	case *intermediate_representation.Constant:
		return flattened
	case *intermediate_representation.MakeInterface:
		return ExtractConstant(flattened.X)
	}
	return nil
}

// ExtractConstantOfKind is ExtractConstant when the constant is of kind, and nil otherwise, nil too for a nil
// constant, which has no value and so no kind.
func ExtractConstantOfKind(
	value intermediate_representation.Value,
	kind constant.Kind,
) *intermediate_representation.Constant {
	extracted := ExtractConstant(value)
	if extracted == nil || extracted.Value == nil || extracted.Value.Kind() != kind {
		return nil
	}
	return extracted
}
