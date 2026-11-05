package flagged

var name = "world"

// Two interpreted literals.
var greeting = "hello, " + "world" // want "literals are joined"

// Two raw literals.
var pattern = `^\d+` + `\s*$` // want "literals are joined"

// The outer + stands beside "a" and "b", though its left operand is the whole sum name + "a".
var chained = name + "a" + "b" // want "literals are joined"

// Each + of an all-literal chain is its own join.
var tripled = "a" + "b" + "c" // want "literals are joined" "literals are joined"

// Parentheses are seen through, on either side and inside a descent.
var wrappedLeft = ("a") + "b"                       // want "literals are joined"
var wrappedRight = "a" + ("b")                      // want "literals are joined"
var nestedRight = "a" + ("b" + name)                // want "literals are joined"
var parenthesizedChain = (name + "a") + ("b" + "c") // want "literals are joined" "literals are joined"

// A constant and a named string type join the same way.
const prefix = "cohere" + "-go" // want "literals are joined"

type Path string

var root Path = "/usr" + "/local" // want "literals are joined"

// A generic function's join is no different.
func Suffixed[Text ~string](value Text) Text {
	return value + ".go" + ".txt" // want "literals are joined"
}

// An argument.
func Use() {
	println("left" + "right") // want "literals are joined"
}

// A comment between the operands keeps them on one line.
var commented = "a" + /* a note */ "b" // want "literals are joined"

// A raw literal that ends on the operator's line is beside the literal after it.
var multiline = `first
second` + "third" // want "literals are joined"

// A raw literal that starts on the operator's line is beside the literal before it, which pastes into it.
// want +1 "literals are joined"
var opening = "first" + `second
third`

// Multi-byte runes paste like any other text.
var multibyte = "héllo, " + "wörld ✓" // want "literals are joined"

// A raw and an interpreted literal whose texts paste into one: the interpreted text into raw, then the raw text into
// interpreted.
var intoRaw = `C:\dir\` + "file"         // want "literals are joined"
var intoInterpreted = "line\n" + `plain` // want "literals are joined"

// A /*line*/ directive moves the next literal to another line by its reported position, not in the file, so the two
// still sit on one line. It is last in the file, since every position after it is reported as the directive says.
// want +1 "literals are joined"
var directive = "a" + /*line flagged.go:900:1*/ "b"
