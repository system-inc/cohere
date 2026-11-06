package lookalike

// Label is a type whose name and fields read like labels, and none of them is one.
type Label struct {
	loop  string
	outer int
}

// A composite literal's keys are followed by a colon, and are keys, not labels.
var defaults = Label{
	loop:  "loop",
	outer: 1,
}

// A map literal's keys are the same.
var counts = map[string]int{
	"loop": 1,
	"done": 2,
}

// A method named for a label, a case clause, a default clause and a slice expression all hold colons.
func (label Label) Loop(value int, values []int) []int {
	switch value {
	case 0:
		return values[:0]
	case 1:
		return values[1:]
	default:
	}
	// loop: a comment that opens like a label is a comment.
	_ = "done: a string that opens like a label is a string"
	return values[label.outer:]
}

// A select's case and default clauses hold colons too.
func poll(messages <-chan string) string {
	select {
	case message := <-messages:
		return message
	default:
		return ""
	}
}
