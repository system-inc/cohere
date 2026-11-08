package parity

// A dot import names errors.As unqualified, as As, in this file alone, so it has a file of its own.
import . "errors"

func dotImported(err error) {
	var number int
	As(err, number)
	As(err, new(*valueError))
	As(err, &err)
	As(err, new(valueError))
	(As)(err, number)
	_ = Is(err, nil)
}
