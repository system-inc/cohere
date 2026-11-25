package gate

// Circle has an empty isShape, which the rule exempts only where a marker interface of that name is declared.
type Circle struct{}

func (Circle) isShape() {}

// Port selects no method.
func Port() int { return 80 }
