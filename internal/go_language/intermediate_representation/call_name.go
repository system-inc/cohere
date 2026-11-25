package intermediate_representation

import (
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// CallName is the name of the function common calls statically: a function's full name, as typeutil.FuncName
// spells it ("strings.Contains", "(*sync.Mutex).Lock"), or a builtin's ("len"). It is the empty string for an
// interface invocation, a call through a value, and a function with no types.Func behind it, as a closure has. It
// is staticcheck's irutil.CallName (honnef.co/go/tools@v0.8.1 go/ir/irutil/util.go), translated onto our IR, so a
// reader moving from go/ir names the same callee through it.
func CallName(common *CallCommon) string {
	if common.IsInvoke() {
		return ""
	}
	switch callee := common.Value.(type) {
	case *Function:
		function, isFunction := callee.Object().(*types.Func)
		if !isFunction {
			return ""
		}
		return typeutil.FuncName(function)
	case *Builtin:
		return callee.Name()
	}
	return ""
}
