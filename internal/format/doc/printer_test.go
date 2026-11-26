package doc

import (
	"fmt"
	"math/rand"
)

/*
 * Random docs for the tests that compare two walks of one doc.
 *
 * One generator produces a random doc *spec*, and buildSpec builds it into a Go doc, as many times as a
 * test needs a fresh copy of the same tree.
 *
 * The generator reaches the cases a printer port gets wrong quietly: groups that fit and break,
 * conditional groups, fills, IfBreak and IndentIfBreak on group ids printed before, after or never,
 * line suffixes flushed by boundaries and by the end of the document, trim, literal lines under
 * markAsRoot, align by width, string and dedent, groups shared by two parents, and text containing wide
 * characters, emoji, trailing whitespace and the empty string.
 */

// spec is a doc in a form a test can build more than once. Only the fields a kind uses are set.
type spec struct {
	Kind      string
	Text      string
	Children  []*spec
	Number    int
	Flag      bool
	Id        string
	Reference int
}

type specGenerator struct {
	random   *rand.Rand
	groupIds []string
	shared   int
	depth    int
}

var specWords = []string{
	"a",
	"bc",
	"def",
	"word",
	"longer",
	"identifier",
	"x",
	"",
	"中文",
	"😀",
	"trailing  ",
	"tab\t",
	"é",
}

func (generator *specGenerator) text() *spec {
	return &spec{Kind: "text", Text: specWords[generator.random.Intn(len(specWords))]}
}

func (generator *specGenerator) line() *spec {
	return &spec{Kind: []string{
		"line",
		"softline",
		"hardline",
		"literalline",
		"hardlineWithoutBreakParent",
	}[generator.random.Intn(5)]}
}

func (generator *specGenerator) groupId() string {
	if len(generator.groupIds) == 0 || generator.random.Intn(3) == 0 {
		id := fmt.Sprintf("g%d", len(generator.groupIds))
		generator.groupIds = append(generator.groupIds, id)
		return id
	}
	return generator.groupIds[generator.random.Intn(len(generator.groupIds))]
}

func (generator *specGenerator) node() *spec {
	generator.depth++
	defer func() { generator.depth-- }()
	if generator.depth > 6 {
		if generator.random.Intn(3) == 0 {
			return generator.line()
		}
		return generator.text()
	}

	switch generator.random.Intn(17) {
	case 0, 1:
		return generator.text()
	case 2:
		return generator.line()
	case 3, 4:
		count := 1 + generator.random.Intn(4)
		children := make([]*spec, count)
		for index := range children {
			children[index] = generator.node()
		}
		return &spec{Kind: "concat", Children: children}
	case 5:
		return &spec{Kind: "indent", Children: []*spec{generator.node()}}
	case 6:
		switch generator.random.Intn(4) {
		case 0:
			return &spec{Kind: "alignWidth", Number: generator.random.Intn(5) - 1, Children: []*spec{generator.node()}}
		case 1:
			return &spec{
				Kind:     "alignString",
				Text:     []string{"> ", "- ", "  ", "", "→ ", "中 "}[generator.random.Intn(6)],
				Children: []*spec{generator.node()},
			}
		case 2:
			return &spec{Kind: "markAsRoot", Children: []*spec{generator.node()}}
		default:
			return &spec{Kind: "dedentToRoot", Children: []*spec{generator.node()}}
		}
	case 7, 8:
		group := &spec{Kind: "group", Flag: generator.random.Intn(6) == 0, Children: []*spec{generator.node()}}
		if generator.random.Intn(3) == 0 {
			group.Id = generator.groupId()
		}
		if generator.random.Intn(5) == 0 {
			generator.shared++
			group.Reference = generator.shared
			return &spec{Kind: "concat", Children: []*spec{group, {Kind: "sharedGroup", Reference: group.Reference}}}
		}
		return group
	case 9:
		count := 2 + generator.random.Intn(2)
		states := make([]*spec, count)
		for index := range states {
			states[index] = generator.node()
		}
		return &spec{Kind: "conditionalGroup", Children: states, Flag: generator.random.Intn(6) == 0}
	case 10:
		count := 1 + generator.random.Intn(5)
		parts := make([]*spec, 0, 2*count)
		for index := 0; index < count; index++ {
			if index > 0 {
				parts = append(parts, &spec{Kind: []string{"line", "softline", "hardline"}[generator.random.Intn(3)]})
			}
			parts = append(parts, generator.node())
		}
		return &spec{Kind: "fill", Children: parts}
	case 11:
		ifBreak := &spec{Kind: "ifBreak", Children: []*spec{generator.node(), generator.node()}}
		if generator.random.Intn(2) == 0 {
			ifBreak.Id = generator.groupId()
		}
		return ifBreak
	case 12:
		return &spec{
			Kind:     "indentIfBreak",
			Id:       generator.groupId(),
			Flag:     generator.random.Intn(2) == 0,
			Children: []*spec{generator.node()},
		}
	case 13:
		return &spec{Kind: "lineSuffix", Children: []*spec{generator.node()}}
	case 14:
		return &spec{Kind: []string{"lineSuffixBoundary", "breakParent", "trim"}[generator.random.Intn(3)]}
	case 15:
		return &spec{Kind: "label", Text: "member-chain", Children: []*spec{generator.node()}}
	default:
		return generator.text()
	}
}

// buildSpec constructs the Go doc for a spec.
func buildSpec(root *spec) Doc { return buildSpecAs(root, false) }

// buildSpecAs is buildSpec, building each concat as a *Sequence of its parts when asSequences is set.
func buildSpecAs(root *spec, asSequences bool) Doc {
	ids := map[string]*GroupId{}
	groups := map[int]*Group{}
	id := func(name string) *GroupId {
		if name == "" {
			return nil
		}
		if ids[name] == nil {
			ids[name] = NewGroupId(name)
		}
		return ids[name]
	}
	var build func(*spec) Doc
	children := func(node *spec) []Doc {
		built := make([]Doc, len(node.Children))
		for index, child := range node.Children {
			built[index] = build(child)
		}
		return built
	}
	build = func(node *spec) Doc {
		switch node.Kind {
		case "text":
			return Text(node.Text)
		case "line":
			return LineDoc
		case "softline":
			return Softline
		case "hardline":
			return Hardline
		case "literalline":
			return Literalline
		case "hardlineWithoutBreakParent":
			return HardlineWithoutBreakParent
		case "concat":
			if asSequences {
				return &Sequence{Parts: children(node)}
			}
			return Concat(children(node))
		case "indent":
			return NewIndent(build(node.Children[0]))
		case "alignWidth":
			return NewAlign(node.Number, build(node.Children[0]))
		case "alignString":
			return AlignWithString(node.Text, build(node.Children[0]))
		case "markAsRoot":
			return MarkAsRoot(build(node.Children[0]))
		case "dedentToRoot":
			return DedentToRoot(build(node.Children[0]))
		case "group":
			group := NewGroup(build(node.Children[0]), GroupOptions{Id: id(node.Id), ShouldBreak: node.Flag})
			if node.Reference != 0 {
				groups[node.Reference] = group
			}
			return group
		case "sharedGroup":
			return groups[node.Reference]
		case "conditionalGroup":
			return ConditionalGroup(children(node), GroupOptions{ShouldBreak: node.Flag})
		case "fill":
			return NewFill(children(node))
		case "ifBreak":
			return NewIfBreak(build(node.Children[0]), build(node.Children[1]), id(node.Id))
		case "indentIfBreak":
			return NewIndentIfBreak(build(node.Children[0]), id(node.Id), node.Flag)
		case "lineSuffix":
			return NewLineSuffix(build(node.Children[0]))
		case "lineSuffixBoundary":
			return LineSuffixBoundary
		case "breakParent":
			return BreakParent
		case "trim":
			return Trim
		case "label":
			return NewLabel(node.Text, build(node.Children[0]))
		}
		panic("unknown spec kind " + node.Kind)
	}
	return build(root)
}
