// Package foreign stands in for a package we do not own, which promotedmethods imports and the test never analyzes.
// Runner has Cmd only by promotion, from an embedded field whose type is an interface no named type declares, so no
// package of the closure declares Cmd on any type.
package foreign

// commanding is an alias of an interface written in place, which a struct may embed by its name.
type commanding = interface {
	Cmd() string
}

// Runner is a concrete type whose method set holds Cmd, promoted from its embedded commanding.
type Runner struct {
	commanding
}
