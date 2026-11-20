package gate

// shape is a marker interface, declared in the file both builds compile.
type shape interface{ isShape() }

// Circle is a variant of shape, whose empty isShape the rule exempts only while no body selects a method of its name.
type Circle struct{}

func (Circle) isShape() {}

// Port selects no method.
func Port() int { return 80 }
