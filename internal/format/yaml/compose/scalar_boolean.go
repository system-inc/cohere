package compose

// ScalarBoolean is a JavaScript boolean as a Scalar's value: what the bool tags resolve to.
type ScalarBoolean bool

func (ScalarBoolean) scalarValue() {}
