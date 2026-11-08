// Package shadowed declares its own len and cap at package scope, so no call in it is of the builtins, and the
// pattern's Builtin refuses every one, as the port must.
package shadowed

func len(values []int) int { return -1 }

func cap(values []int) int { return -1 }

func compared(values []int) bool {
	_ = len(values) < 0
	_ = 0 > cap(values)
	return (len)(values) < 0
}
