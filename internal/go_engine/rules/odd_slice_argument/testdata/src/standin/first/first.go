// Package first declares three callers before the functions they reach, each through a function of no package that
// stands for one of the package's own: an instantiation of a generic function, a method value's bound method, and a
// method expression's thunk. Each caller passes its slice on to the stand-in, which passes it on to the function it
// stands for, which passes it on to pairs, which checks it, so each caller's slice reaches the check and odd's three
// are reported (#8d9r2rh). Nothing here calls itself, and no call closes a cycle. Package last, beside this one, is
// the same functions with the callers declared after what they reach. staticcheck's walk enters a stand-in and never
// the function it stands for, whose marks it reads as far as the walk has come, so it reports odd's three there and
// none here, where it meets each caller before generic and method have passed their slices to pairs.
package first

// holder holds the method.
type holder struct{}

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

// generic passes its slice on to pairs. Its type parameter is unused.
func generic[Unused any](values []string) {
	pairs(values)
}

// method passes its slice on to pairs.
func (holder) method(values []string) {
	pairs(values)
}

// pairs panics unless its values come in pairs.
func pairs(values []string) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// odd passes each caller a slice of one. The records are ruled here: the port rightly reports all three, since each
// caller's slice reaches pairs's check through the function its stand-in stands for, as package last's same functions
// are reported by both. staticcheck misses them here only by meeting the callers before generic and method.
func odd() {
	throughInstance([]string{"one"})         // port finds: argument "values" is expected to have even number of elements, but has 1 elements
	throughMethodValue([]string{"one"})      // port finds: argument "values" is expected to have even number of elements, but has 1 elements
	throughMethodExpression([]string{"one"}) // port finds: argument "values" is expected to have even number of elements, but has 1 elements
}
