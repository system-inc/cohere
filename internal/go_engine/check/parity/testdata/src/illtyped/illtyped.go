// Package illtyped holds a call both sides find beside a type error, so a comparison over it would count only what
// of it type-checked.
package illtyped

func shared() {}

func calls() {
	shared()
}

var count int = "many"
