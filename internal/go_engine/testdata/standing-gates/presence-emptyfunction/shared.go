package gate

// shape is a marker interface, declared in the file both builds compile.
type shape interface{ isShape() }

// Circle is a variant of shape.
type Circle struct{}

func (Circle) isShape() { /* A marker: membership, not behavior. */ }

// Check selects shape's method, so emptyfunction reads it.
func Check(value shape) { value.isShape() }

// Port selects no method.
func Port() int { return 80 }
