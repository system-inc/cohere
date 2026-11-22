// Package stripped serves First and Second in one file, beside Variant, a marker, and declares types in a body, which
// the strip takes away.
package stripped

import "strings"

type First struct{}

type Second struct{}

// Variant's one method is an empty marker, so it serves nothing.
type Variant struct{}

func (First) label() string { return "first" }

func (Second) label() string { return "second" }

func (Variant) marker() {}

// describe declares a type that embeds First and so has its label, an interface that type satisfies, a method value
// of First's, and one of Other's, which nothing else in this file serves, all in the body the strip takes away, and
// only it uses strings.
func describe() string {
	type local struct{ First }
	type labeler interface{ label() string }
	var value labeler = local{}
	method := First{}.label
	otherMethod := Other{}.name
	return strings.TrimSpace(value.label() + method() + otherMethod())
}
