// Package last is package first's functions with the three callers declared after the functions they reach. Each
// function reads as it does there, each caller's slice reaches the check through its stand-in as it does there, and
// odd's three are reported as they are there (#8d9r2rh). staticcheck's walk reports them here: it has marked generic
// and method through pairs before it meets a caller, so each stand-in reads a mark already made.
package last

// holder holds the method.
type holder struct{}

// pairs panics unless its values come in pairs.
func pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// generic passes its slice on to pairs. Its type parameter is unused.
func generic[Unused any](values []string) {
	pairs(values)
}

// method passes its slice on to pairs.
func (holder) method(values []string) {
	pairs(values)
}

// throughInstance passes its slice to an instantiation of generic.
func throughInstance(values []string) {
	generic[int](values)
}

// throughMethodValue passes its slice to a method value, a closure over the bound method that stands for method.
func throughMethodValue(values []string) {
	bound := holder{}.method
	bound(values)
}

// throughMethodExpression passes its slice to a method expression, a thunk that stands for method.
func throughMethodExpression(values []string) {
	holder.method(holder{}, values)
}

// odd passes each caller a slice of one.
func odd() {
	throughInstance([]string{"one"})         //@ diag(`argument "values" is expected to have even number of elements, but has 1 elements`)
	throughMethodValue([]string{"one"})      //@ diag(`argument "values" is expected to have even number of elements, but has 1 elements`)
	throughMethodExpression([]string{"one"}) //@ diag(`argument "values" is expected to have even number of elements, but has 1 elements`)
}
