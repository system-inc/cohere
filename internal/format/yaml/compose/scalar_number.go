package compose

// ScalarNumber is a JavaScript number as a Scalar's value: what the int and float tags resolve to, an
// integer too, since intAsBigInt is never set on this path.
type ScalarNumber float64

func (ScalarNumber) scalarValue() {}
