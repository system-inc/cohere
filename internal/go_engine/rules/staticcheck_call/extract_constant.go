package staticcheck_call

import (
	"go/constant"

	"honnef.co/go/tools/go/ir"
	"honnef.co/go/tools/go/ir/irutil"
)

// ExtractConstant is the constant value is, seen through phis whose edges are all that one constant and through a
// conversion to an interface, or nil when it is not one constant.
func ExtractConstant(value ir.Value) *ir.Const {
	switch flattened := irutil.Flatten(value).(type) {
	case *ir.Const:
		return flattened
	case *ir.MakeInterface:
		return ExtractConstant(flattened.X)
	}
	return nil
}

// ExtractConstantOfKind is ExtractConstant when the constant is of kind, and nil otherwise, nil too for a nil
// constant, which has no value and so no kind.
func ExtractConstantOfKind(value ir.Value, kind constant.Kind) *ir.Const {
	extracted := ExtractConstant(value)
	if extracted == nil || extracted.Value == nil || extracted.Value.Kind() != kind {
		return nil
	}
	return extracted
}
