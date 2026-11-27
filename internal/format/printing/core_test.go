package printing

import (
	"fmt"
	"math/rand"
	"strings"
)

/*
 * A toy language for exercising the print core without a real printer: parenthesised lists of atoms, an
 * optional label on a list, and block and line comments at random gaps. The path tests walk the trees it
 * generates, so their shape and nesting come from a seed rather than by hand.
 */

// toySource builds text and AST together, so positions are exact by construction.
type toySource struct {
	random   *rand.Rand
	text     strings.Builder
	comments []*toyNode
	counter  int
}

// gap writes one to three items: comments, newlines, blank lines, commas and spaces, so a run can put two
// comments on one line or a blank line next to a comment.
func (source *toySource) gap() {
	for count := 1 + source.random.Intn(3); count > 0; count-- {
		source.gapItem()
	}
}

func (source *toySource) gapItem() {
	switch source.random.Intn(9) {
	case 0:
		source.counter++
		start := source.text.Len()
		source.text.WriteString(fmt.Sprintf("/*b%d*/", source.counter))
		source.comments = append(source.comments, &toyNode{Kind: "Block", From: start, To: source.text.Len()})
		source.text.WriteString([]string{" ", "\n", ""}[source.random.Intn(3)])
	case 1:
		source.counter++
		start := source.text.Len()
		source.text.WriteString(fmt.Sprintf("//l%d", source.counter))
		source.comments = append(source.comments, &toyNode{Kind: "Line", From: start, To: source.text.Len()})
		source.text.WriteString("\n")
	case 2:
		source.text.WriteString("\n")
	case 3:
		source.text.WriteString("\n\n")
	case 4:
		source.text.WriteString(", ")
	case 5:
		source.text.WriteString(" ")
	}
}

func (source *toySource) node(depth int) *toyNode {
	if depth > 3 || source.random.Intn(3) == 0 {
		start := source.text.Len()
		value := []string{"a", "bb", "ccc", "identifier", "x"}[source.random.Intn(5)]
		source.text.WriteString(value)
		return &toyNode{Kind: "Atom", From: start, To: source.text.Len(), Value: value}
	}
	start := source.text.Len()
	source.text.WriteString("(")
	node := &toyNode{Kind: "List", From: start}
	// The label is a node held in its own property rather than in the children slice, so a walk of the
	// tree has to reach it both ways.
	if source.random.Intn(3) == 0 {
		labelStart := source.text.Len()
		source.text.WriteString("#tag")
		node.Label = &toyNode{Kind: "Atom", From: labelStart, To: source.text.Len(), Value: "#tag"}
		source.text.WriteString(" ")
	}
	count := source.random.Intn(4)
	for index := 0; index < count; index++ {
		source.gap()
		if index > 0 {
			source.text.WriteString(" ")
		}
		node.Children = append(node.Children, source.node(depth+1))
	}
	source.gap()
	source.text.WriteString(")")
	node.To = source.text.Len()
	return node
}

func toyProgram(random *rand.Rand) (*toyNode, []*toyNode, string) {
	source := &toySource{random: random}
	program := &toyNode{Kind: "Program"}
	count := 1 + random.Intn(3)
	for index := 0; index < count; index++ {
		source.gap()
		program.Children = append(program.Children, source.node(0))
		source.text.WriteString("\n")
	}
	source.gap()
	program.To = source.text.Len()
	return program, source.comments, source.text.String()
}

// toySettings is the toy language's per-print state, which it has none of.
type toySettings struct{}
