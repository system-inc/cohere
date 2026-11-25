// Package lowering holds the places where the intermediate representation may lower a unit differently between two
// builds though the unit's own type information is the same in both: a composite literal of a struct whose unexported
// field differs, a call whose callee returns in one build and not in the other, and a method value or interface
// conversion of a type whose method set differs.
package lowering

// Record has an unexported field whose array length hangs on Slots, which differs by build, beside an exported one.
type Record struct {
	slots [Slots]int
	Name  string
}

// NewRecord builds a Record with a composite literal that never names slots, which the lowering zeroes all the same,
// with a zero value of slots' own type.
func NewRecord() Record { return Record{Name: "record"} }

// PointRecord takes a Record's address from a composite literal that never names slots.
func PointRecord() *Record { return &Record{Name: "pointed"} }

// Named only passes a Record's pointer on, never building one.
func Named(record *Record) *Record { return record }

// Halt calls stop, which each platform's file declares: on darwin stop never returns, so neither does Halt, and on
// linux both return.
func Halt() { stop() }

// Guard calls Halt from a shared file and names nothing a build changes, while the lowering after the call hangs on
// whether Halt returns.
func Guard(value int) int {
	if value < 0 {
		Halt()
	}
	return value
}

// Speaker gains Loud in linux's file only, so its method set differs by build.
type Speaker struct {
	Volume int
}

// Quiet is declared on Speaker in every build.
func (Speaker) Quiet() string { return "quiet" }

// Bound takes Quiet as a method value of a Speaker, which the lowering makes a bound method wrapper.
func Bound(speaker Speaker) func() string { return speaker.Quiet }

// Thunk takes Quiet as a method expression, which the lowering makes a thunk.
func Thunk() func(Speaker) string { return Speaker.Quiet }

// Converted converts a Speaker's pointer to an interface, which carries the method set.
func Converted(speaker *Speaker) any { return speaker }

// Loudness is an interface whose method only linux's Speaker has.
type Loudness interface {
	Loud() string
}

// IsLoud asks whether a value has Loud, which reads nothing of Speaker.
func IsLoud(value any) bool {
	_, isLoud := value.(Loudness)
	return isLoud
}

// Labels is initialized in the package initializer, beside platformLabel, which each platform's file declares, so
// the initializer's blocks and instructions shift between builds around Labels' own.
var Labels = []string{"first", "second"}

// Greet is initialized to a function literal, an anonymous function of the package initializer.
var Greet = func() string { return "hello " + Labels[0] }

// The blank identifier's initializer stores into no variable, after a blank one only linux's file declares.
var _ any = 2
