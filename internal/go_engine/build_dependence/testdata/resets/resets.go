// Package resets holds the shapes inside one package that a worker's scratch must reset between units: a function two
// units call, and value specs whose type expressions declare members right after a type spec declared its own.
package resets

// Shared is called from two units, First and Second, so each must carry it as a callee.
func Shared() int { return 1 }

// First calls Shared, the first of its two callers.
func First() int { return Shared() }

// Second calls Shared too, after First did.
func Second() int { return Shared() }

// Holder declares a field of its own, and is the owner of its members just before Loose.
type Holder struct {
	Count int
}

// Loose declares Field in its anonymous struct; Loose owns it, and Holder, the type before it, does not.
var Loose struct {
	Field int
}

// Plain declares no member, and is the owner of its members just before Speaker.
type Plain int

// Speaker declares Speak in its anonymous interface; Speaker owns it, and Plain, the type before it, does not.
var Speaker interface {
	Speak()
}
