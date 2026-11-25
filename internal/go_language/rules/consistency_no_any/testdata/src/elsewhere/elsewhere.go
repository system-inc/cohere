// Package elsewhere is another package's code, which the fixtures import and the test never analyzes: its
// signatures fix the types of what the fixtures hand to it, take from it, and implement for it.
package elsewhere

// Store is an interface of another package whose methods take and return any.
type Store interface {
	Load(key string) (any, bool)
	Save(key string, value any)
}

// Printer is a concrete type of another package, whose method an interface written to accept it copies.
type Printer struct{}

// Printf is the method a mirroring interface copies.
func (printer *Printer) Printf(format string, arguments ...any) {
	// Prints nothing: only its signature matters to the fixtures.
}

// Registry is a concrete type of another package. A concrete method of ours with its method's name and signature
// copies nothing of it: no rule of Go relates two concrete methods, so the signature stays the declaring package's.
type Registry struct{}

// Lookup is the method a concrete method of ours may share a name and signature with, by coincidence.
func (registry *Registry) Lookup(key string) any {
	return key
}

// Shelf is a generic concrete type of another package, whose method a concrete one of ours may share a shape with.
type Shelf[Item any] struct{}

// Place takes one item, of the shelf's type parameter.
func (shelf Shelf[Item]) Place(item Item) {
	// Places nothing: only its signature matters to the fixtures.
}

// Holder is generic, so a method set matches it only through an instantiation the rule cannot know.
type Holder[Value any] interface {
	Hold(value Value)
}

// VisitFunc is a named function type of another package, which a conversion hands a function literal to.
type VisitFunc func(value any) bool

// Visit hands every value to visitor until it returns false.
func Visit(values []any, visitor func(value any) bool) {
	for _, value := range values {
		if !visitor(value) {
			return
		}
	}
}

// Plugin is a struct of another package whose field's type fixes the type of a function given to it.
type Plugin struct {
	Run func(options any) error
}

// Fetch returns a value whose type another package chose.
func Fetch(key string) any {
	return key
}

// Arguments returns values another package built.
func Arguments() []any {
	return nil
}

// Query takes its arguments the way database/sql does.
func Query(query string, arguments ...any) {
	// Runs nothing: only its signature matters to the fixtures.
}

// Value is another package's name for any, as driver.Value is, so a declaration spelling it names that package's
// choice rather than writing any.
type Value = any
