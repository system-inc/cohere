// Package loose is the build's package the noany presence test settles: a key of each kind, a candidate that keeps no
// declaration, and a function that holds no candidate.
package loose

// Handle's parameter is a key.
func Handle(value any) int { return 0 }

// Record's Payload is a field key.
type Record struct {
	Payload any
	Name    string
}

// Loose is a variable key.
var Loose interface{}

// Getter's Get is an interface method key.
type Getter interface{ Get() any }

// Box has a method of Get's name.
type Box struct{}

// Get is of the interface method key's name.
func (Box) Get() int { return 1 }

// Inner's Note is a field key, which a literal reaches through Rows without naming Inner.
type Inner struct{ Note any }

// Rows holds Inners.
type Rows []Inner

// settings is a variable key, of an anonymous struct whose rows field is a key too.
var settings struct{ rows []any }

// Anything keeps no declaration, so no body can fix it.
type Anything any

// Strict holds no candidate.
func Strict(count int) int { return count }

// Calls names Handle.
func Calls() int { return Handle(1) }

// Builds names Record.
func Builds() Record { return Record{Name: "built"} }

// Reads selects Payload.
func Reads(record Record) bool { return record.Payload == nil }

// Sets names Loose.
func Sets() { Loose = 1 }

// Gets selects Box's Get.
func Gets(box Box) int { return box.Get() }

// Holds names Anything.
func Holds(value Anything) bool { return value == nil }

// Counts names Strict.
func Counts() int { return Strict(1) }

// Fills builds Rows, whose unkeyed element marks Inner's Note, though it names neither Inner nor Note.
func Fills() Rows { return Rows{{nil}} }

// Touches names settings.
func Touches() { settings.rows = nil }

// Fresh builds a struct of settings' shape written in place, whose fields are its own and no key's.
func Fresh() bool {
	value := struct{ rows []any }{nil}
	return value.rows == nil
}
