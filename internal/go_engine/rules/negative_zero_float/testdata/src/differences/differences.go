// Package differences holds the negative zeros where the port's messages and the original's could most plausibly
// render differently, and records no difference, since they part nowhere: expressions spread over lines, beside
// comments and inside parentheses.
package differences

func plausible() {
	_ = -float64(
		0)
	_ = -float64(0 /* zero */)
	_ = - /* sign */ float32(0.0)
	_ = float64(
		-0,
	)
	_ = -(float64)((0.0))
	_ = - // sign
	0.0
}
