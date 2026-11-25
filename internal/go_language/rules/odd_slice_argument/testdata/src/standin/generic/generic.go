// Package generic holds a method of a generic type that checks its slice of the type's element itself, and three
// callers that reach the method through an instance of the type: a call of the instance's method, a method value's
// bound method, and a method expression's thunk. Each stands for the generic method, whose object for an instance is
// the instance's own, and whose parameter there is the instance's, of type []string where the method's own is of type
// []Element. A walk that names the instance's object rather than its origin finds no function of the package there,
// and a pass that names the instance's parameter finds no mark on it (#8d9r2rh). Each caller's slice reaches the
// check, so each is marked.
package generic

// holder holds the method, for any element type.
type holder[Element any] struct{}

// method panics unless its values come in pairs.
func (holder[Element]) method(values []Element) {
	if len(values)%2 != 0 {
		panic("values come in pairs")
	}
}

// throughInstance passes its slice to the method of an instance of holder.
func throughInstance(values []string) {
	holder[string]{}.method(values)
}

// throughMethodValue passes its slice to a method value of an instance of holder, a closure over the bound method.
func throughMethodValue(values []string) {
	bound := holder[string]{}.method
	bound(values)
}

// throughMethodExpression passes its slice to a method expression of an instance of holder, a thunk.
func throughMethodExpression(values []string) {
	holder[string].method(holder[string]{}, values)
}
