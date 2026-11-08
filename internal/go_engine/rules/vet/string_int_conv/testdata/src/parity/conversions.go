// Package parity holds every shape of a conversion of an integer to a string the original judges, beside the clean
// ones next to each, for the differential to hold the port to it finding for finding and fix for fix. This file has
// no imports, and its first declaration has a doc comment, so the import of fmt the first fix adds goes before it.
package parity

// Text is a string type of its own, so a conversion to it keeps its conversion around the formatted number.
type Text string

type (
	Count     int
	Unsigned  uint64
	Small     int8
	Letter    = rune
	Octet     = byte
	Width     = int
	Label     = string
	LabelText = Text
	// Code has a String method, which may change what fmt.Sprint prints, so the first fix is not offered.
	Code int
	// Pointered has a method only on its pointer, outside its value's method set, so the first fix is offered.
	Pointered int
	Embedded  struct{ Count int }
	Holder    struct {
		Embedded
		value int
	}
)

// Wrapped is a generic integer type, whose instances are named types of their own.
type Wrapped[Element any] int

func (Code) String() string { return "code" }

func (*Pointered) Set() {}

func (holder Holder) Number() int { return holder.value }

func basics(number int, unsigned uint, wide int64, narrow int8, pointer uintptr, sixteen uint16, thirtyTwo uint32) {
	_ = string(number)
	_ = string(unsigned)
	_ = string(wide)
	_ = string(narrow)
	_ = string(pointer)
	_ = string(sixteen)
	_ = string(thirtyTwo)
	_ = string(65)
	_ = string(0x41)
	const typed int64 = 66
	_ = string(typed)
	const untyped = 67
	_ = string(untyped)
	_ = string(Count(3))
	_ = string(Small(3))
	_ = string(Unsigned(3))
}

func clean(octet byte, letter rune, eight uint8, signed int32, text string, floating float64, number int) {
	// A byte or a rune, under any of its names, converts to the string of its one character, as meant.
	_ = string(octet)
	_ = string(letter)
	_ = string(eight)
	_ = string(signed)
	_ = string('a')
	_ = string(Octet(number))
	_ = string(Letter(number))
	_ = string(rune(number))
	_ = string(text)
	_ = string([]byte(text))
	_ = string([]rune(text))
	// A conversion to a type that is not a string, or of a value that is not an integer.
	_ = Letter(number)
	_ = Count(number)
	_ = float64(number)
	_ = int(floating)
	_ = Text(text)
	// A parenthesized type is not a name the original reads, so the conversion is passed over.
	_ = (string)(number)
	_ = (Text)(number)
}

func targets(number int, count Count, width Width) {
	_ = Text(number)
	_ = Label(number)
	_ = LabelText(number)
	_ = Text(count)
	_ = Label(count)
	_ = Text(width)
	type local string
	_ = local(number)
	type localCount int
	_ = string(localCount(number))
}

func sources(count Count, width Width, code Code, pointered Pointered, holder Holder, wrapped Wrapped[string]) {
	_ = string(count)
	_ = string(width)
	_ = string(code)
	_ = Text(code)
	_ = string(pointered)
	_ = string(holder.Count)
	_ = string(holder.Embedded.Count)
	_ = string(holder.Number())
	numberOf := holder.Number
	_ = string(numberOf())
	_ = string(Holder.Number(holder))
	_ = string(wrapped)
	_ = string(Wrapped[int](3))
}

func arguments(number int, numbers []int, lookup map[string]int, pointer *int, channel chan int) {
	_ = string((number))
	_ = string(number << 1)
	_ = string(number + 1)
	_ = string(-number)
	_ = string(numbers[0])
	_ = string(lookup["key"])
	_ = string(*pointer)
	_ = string(<-channel)
	_ = string(len(numbers))
	_ = len(string(number))
	_ = []byte(string(number))
	_ = string(Count(number)) + Label(number)
	_ = string(
		number,
	)
}

func closures(number int) func() string {
	run := func() string { return string(number) }
	_ = run()
	defer func() { _ = Text(number) }()
	go func(inner int) { _ = string(inner) }(number)
	return func() string {
		return string(number)
	}
}

// calls is calls of one argument that convert nothing: a function, a method expression, a function value, and a
// value named for a type that shadows it.
func calls(number int, holder Holder) {
	_ = basics
	_ = Holder.Number(holder)
	double := func(value int) int { return value * 2 }
	_ = double(number)
	{
		string := func(value int) int { return value }
		_ = string(number)
	}
	_ = make([]int, number)
	_ = new(int)
	_ = string(rune(number)) + string(byte(number))
}

var seed = 70

// Converted at the package's level, outside every function, where the innermost scope is the file's.
var packageLevel = string(seed)

var packageLevelTyped = Text(seed)

var packageLevelClosure = func() string { return string(seed) }

func (holder Holder) method() string {
	return string(holder.value) + string(holder.Count)
}

func (code Code) method(number int) Text {
	return Text(number) + Text(code)
}
