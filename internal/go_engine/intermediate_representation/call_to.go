package intermediate_representation

import (
	"go/types"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/function_name"
)

// IsCallTo reports whether common calls the function CallName spells as name. It is staticcheck's irutil.IsCallTo
// (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR, and answers through CallIndex.
func IsCallTo(common *CallCommon, name string) bool { return CallIndex(common, name) == 0 }

// IsCallToAny reports whether common calls any of the functions CallName spells as names. It is staticcheck's
// irutil.IsCallToAny (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR, and answers through
// CallIndex.
func IsCallToAny(common *CallCommon, names ...string) bool { return CallIndex(common, names...) >= 0 }

// CallIndex is which of names CallName(common) is, the first in their order, or -1 when it is none of them. It
// compares a function's full name in place (function_name), never building it, since a reader asks it of every
// call it walks and most are not the function it wants (#p92j24d); a builtin's name and the empty name of a call
// CallName can't name are compared as CallName gives them.
func CallIndex(common *CallCommon, names ...string) int {
	if common.IsInvoke() {
		return slices.Index(names, "")
	}
	switch callee := common.Value.(type) {
	case *Function:
		function, isFunction := callee.Object().(*types.Func)
		if !isFunction {
			return slices.Index(names, "")
		}
		return function_name.Index(function, names...)
	case *Builtin:
		return slices.Index(names, callee.Name())
	}
	return slices.Index(names, "")
}
