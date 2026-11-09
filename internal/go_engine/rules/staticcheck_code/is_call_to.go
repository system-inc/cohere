package staticcheck_code

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsCallTo is whether node is a call of the function CallName names name, such as "fmt.Println" or
// "(*sync.Mutex).Lock". It is staticcheck's code.IsCallTo, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// A node that is no call is no call to anything, even asked for "", the name CallName gives a call of nothing it
// names.
func IsCallTo(context *rule.Context, node ast.Node, name string) bool {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return false
	}
	return CallIndex(context, call, name) == 0
}

// IsCallToAny is whether node is a call of a function CallName names one of names. It is staticcheck's
// code.IsCallToAny, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
func IsCallToAny(context *rule.Context, node ast.Node, names ...string) bool {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return false
	}
	return CallIndex(context, call, names...) >= 0
}
