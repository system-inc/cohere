//go:build linux

package promoted

// Extra has a Name on linux, shallower than Base's under Inner.
type Extra struct {
	Name string
}

// Second has a Count on linux, as deep as First's under Both.
type Second struct {
	Count int
}
