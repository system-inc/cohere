// Package foreign is another package's code, which promotedmethods imports and the test never analyzes.
package foreign

// Located has HomeURL only from an interface written in place that it embeds, and declares Region itself.
type Located interface {
	interface{ HomeURL() string }
	Region() string
}

// base declares HomeURL on a concrete type.
type base struct{}

func (base) HomeURL() string { return "" }

// Runner is a concrete type whose method set holds HomeURL, promoted from its embedded base.
type Runner struct {
	base
}
