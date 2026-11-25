package lookalike

const prefix = "cohere"

// A named constant is no literal, though its value is known.
var named = prefix + "-go"

// A conversion is a call, not a literal.
var converted = string("a") + "b"

// Rune literals are numbers, so this is arithmetic.
var runes = 'a' + 'b'

// Numbers and a unary plus.
var sum = 1 + 2
var unary = +1 + 2

// An assignment operator joins a variable, not two literals.
func Append() string {
	text := "a"
	text += "b"
	return text
}

// A line comment after the operator puts the next literal on another line.
var commented = "a" + // the second half follows
	"b"

// A raw literal that ends on its own line, the operator ending the line after it.
var multiline = `first
second` +
	"third"

// A raw text with quotes and backslashes beside an escape: one literal could hold both only by escaping one of them.
var escaped = `say "hi" from C:\dir` + "\n"

// A backtick inside raw text, the reason Go joins the two kinds.
var backtick = `run "go vet" in \src, then ` + "`" + `go test \...`

// A //line directive between wrapped operands, which says the second is on the first's line. The file's own lines
// are what the rule reads. It is last in the file, since every position after it is reported as the directive says.
var directive = "a" +
//line lookalike.go:42
	"b"
