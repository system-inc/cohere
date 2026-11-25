// Package fresh declares fmt and fmt0 at its own level, so the first fix imports fmt as fmt1, the first name after
// them that nothing holds.
package fresh

var fmt, fmt0 = 1, 2

func fresh(number int) string {
	return string(number) + string(fmt) + string(fmt0)
}
