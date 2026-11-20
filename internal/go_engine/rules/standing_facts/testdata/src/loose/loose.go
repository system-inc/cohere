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
