// Package parity holds every character `ST1018` reads, each found and each near miss, in every kind of literal it
// judges and the kind it leaves, for the original and the port to judge alike. The characters are written as
// themselves, unseen, so each line's comment names what its literal holds.
package parity

var (
	// A bell, a control character, alone: one fix escapes it, one deletes it.
	bell = "ring"
	// A bell and a substitute, two control characters, each escaped or each deleted by one fix.
	twoControls = ""
	// A delete and a next line, the C1 control, both control characters.
	otherControls = "abc"
	// A tab, a newline in a raw string, and escape sequences are allowed.
	allowedControls = "tab	here"
	rawNewline      = `line
line`
	escapes = "\a\u200b\x1a"
	// A zero-width space, a format character, in an interpreted string and in a raw one.
	zeroWidth    = "zero​width"
	rawZeroWidth = `zero​width`
	// A soft hyphen is a format character too.
	softHyphen = "soft­hyphen"
	// Each bidirectional formatting character is reported, a right-to-left override and an isolate here.
	override   = "abc‮def‬"
	isolate    = "⁧isolated⁩"
	arabicMark = "؜"
	// Arabic format characters that are seen are allowed.
	arabicNumberSign = "؀؁؅࣢"
	// A flag spelled with tag characters is allowed.
	flag = "🏴󠁧󠁢󠁳󠁣󠁴󠁿"
	// A variation selector is allowed after anything.
	selector = "a️❤️"
	// A zero-width joiner between emoji is allowed, after a symbol and after a variation selector.
	family      = "👨‍👩‍👧"
	heartOnFire = "❤️‍🔥"
	// A zero-width joiner after a letter is reported.
	joinedLetters = "a‍b"
	// A zero-width joiner first in an interpreted string follows the opening quote, no symbol, and is reported.
	quotedJoiner = "‍"
	// First in a raw string, it follows the opening backquote, which is a symbol, so neither side reports it.
	rawJoiner = `‍`
	// A format character and a control character together.
	mixed = "​"
	// A control character and an allowed format character, one finding of the control alone.
	controlBesideEmoji = "👨‍👩"
)

// A rune literal is no string, and neither side judges it.
var runeLiteral = ''

// A struct tag is a string literal, judged as any other.
type tagged struct {
	field int `json:"field​"`
}

// A string in a constant and in a call are judged alike.
const constant = "constant"

func call(text string) {}

func calls() {
	call("argument​")
}
