package tailwind

import (
	"fmt"
	"strings"
	"testing"
)

// These are the cases of upstream's `src/selector-parser.test.ts` at v4.3.3, every one of them, in
// upstream's order and under upstream's names. An `it` holding several expectations becomes several
// rows, numbered after the name. The expected trees and printed strings are upstream's, transcribed
// node for node.
//
// The one case not ported literally is upstream's `walk` block, which replaces a node through the
// shared `walk` helper's `WalkAction.Replace`. That helper is not ported here: walk.go walks the CSS
// AST, enter-only, with no Replace. TestSelectorTreeReprintsAnEdit asserts what the case pins, that
// an edited tree reprints the edit, with the replacement done by hand.
//
// The rows after upstream's pin the JavaScript-shaped behaviors listed at the top of
// selector_parser.go. Each expectation there was read off upstream's own source run under Node,
// not reasoned out, because a careful reader would get them wrong in the direction of a well-formed
// tree.

// The constructors below spell an expected tree compactly. A container always gets a non-nil Nodes
// slice, even with no children, matching what the parser builds.

func selectorLeaf(value string) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindSelector, Value: value}
}

func selectorCombinator(value string) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindCombinator, Value: value}
}

func selectorValue(value string) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindValue, Value: value}
}

func selectorFunction(value string, nodes ...SelectorNode) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindFunction, Value: value, Nodes: append([]SelectorNode{}, nodes...)}
}

func selectorCompound(nodes ...SelectorNode) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindCompound, Nodes: append([]SelectorNode{}, nodes...)}
}

func selectorComplex(nodes ...SelectorNode) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindComplex, Nodes: append([]SelectorNode{}, nodes...)}
}

func selectorList(nodes ...SelectorNode) SelectorNode {
	return SelectorNode{Kind: SelectorNodeKindList, Nodes: append([]SelectorNode{}, nodes...)}
}

// selectorParseCases are upstream's `describe('parse')`.
var selectorParseCases = []struct {
	name  string
	input string
	want  []SelectorNode
}{
	{"should parse a simple selector", ".foo", []SelectorNode{selectorLeaf(".foo")}},
	{"should parse a compound selector", ".foo.bar:hover#id", []SelectorNode{
		selectorCompound(selectorLeaf(".foo"), selectorLeaf(".bar"), selectorLeaf(":hover"), selectorLeaf("#id")),
	}},
	{"should parse a pseudo-element selector with double ::", ".foo::before", []SelectorNode{
		selectorCompound(selectorLeaf(".foo"), selectorLeaf("::before")),
	}},
	{"should parse a selector list", ".foo,.bar", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorLeaf(".bar")),
	}},
	// Upstream keeps an invalid list invalid on purpose: the empty item is an empty compound, so
	// reprinting reintroduces the error rather than silently repairing it, which is what browsers
	// and Lightning CSS saw before upstream parsed selectors at all.
	{"should safely parse an invalid selector list 1", ".foo,", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorCompound()),
	}},
	{"should safely parse an invalid selector list 2", ",.foo", []SelectorNode{
		selectorList(selectorCompound(), selectorLeaf(".foo")),
	}},
	{"should safely parse an invalid selector list 3", ",.foo,", []SelectorNode{
		selectorList(selectorCompound(), selectorLeaf(".foo"), selectorCompound()),
	}},
	{"should safely parse an invalid selector list 4", ".foo,,.bar", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorCompound(), selectorLeaf(".bar")),
	}},
	{"should parse selector lists with whitespace around the comma 1", ".foo, .bar", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorLeaf(".bar")),
	}},
	{"should parse selector lists with whitespace around the comma 2", ".foo , .bar", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorLeaf(".bar")),
	}},
	{"should parse selector lists with whitespace around the comma 3", ".foo ,.bar", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorLeaf(".bar")),
	}},
	{"should parse a list of attribute selectors", "[foo],[bar]", []SelectorNode{
		selectorList(selectorLeaf("[foo]"), selectorLeaf("[bar]")),
	}},
	{"should parse a list of selectors with just functions", ":is(.a),:is(.b)", []SelectorNode{
		selectorList(selectorFunction(":is", selectorLeaf(".a")), selectorFunction(":is", selectorLeaf(".b"))),
	}},
	{"should parse selector lists with more than two selectors", ".foo,.bar,.baz", []SelectorNode{
		selectorList(selectorLeaf(".foo"), selectorLeaf(".bar"), selectorLeaf(".baz")),
	}},
	{"should group selectors with combinators in a selector list", ".a+.b, .c .d[attr], .e", []SelectorNode{
		selectorList(
			selectorComplex(selectorLeaf(".a"), selectorCombinator("+"), selectorLeaf(".b")),
			selectorComplex(
				selectorLeaf(".c"),
				selectorCombinator(" "),
				selectorCompound(selectorLeaf(".d"), selectorLeaf("[attr]")),
			),
			selectorLeaf(".e"),
		),
	}},
	{"should parse complex selectors in a selector list", "#a.b > .c, .d", []SelectorNode{
		selectorList(
			selectorComplex(
				selectorCompound(selectorLeaf("#a"), selectorLeaf(".b")),
				selectorCombinator(">"),
				selectorLeaf(".c"),
			),
			selectorLeaf(".d"),
		),
	}},
	{"should combine everything within attribute selectors", `.foo[bar="baz"]`, []SelectorNode{
		selectorCompound(selectorLeaf(".foo"), selectorLeaf(`[bar="baz"]`)),
	}},
	{"should parse functions", ".foo:hover:not(.bar:focus)", []SelectorNode{
		selectorCompound(
			selectorLeaf(".foo"),
			selectorLeaf(":hover"),
			selectorFunction(":not", selectorCompound(selectorLeaf(".bar"), selectorLeaf(":focus"))),
		),
	}},
	{"should parse selector lists in functions", ":is(.foo, .bar)", []SelectorNode{
		selectorFunction(":is", selectorList(selectorLeaf(".foo"), selectorLeaf(".bar"))),
	}},
	{"should handle next-children combinator", ".foo + p", []SelectorNode{
		selectorComplex(selectorLeaf(".foo"), selectorCombinator("+"), selectorLeaf("p")),
	}},
	{"should normalize combinators 1", ".foo + .bar", []SelectorNode{
		selectorComplex(selectorLeaf(".foo"), selectorCombinator("+"), selectorLeaf(".bar")),
	}},
	{"should normalize combinators 2", ".foo  \n\t .bar", []SelectorNode{
		selectorComplex(selectorLeaf(".foo"), selectorCombinator(" "), selectorLeaf(".bar")),
	}},
	{"should handle escaped characters", `foo\.bar`, []SelectorNode{selectorLeaf(`foo\.bar`)}},
	// The `An+B` part is not a selector, so it stays an opaque value.
	{"parses :nth-child() 1", ":nth-child(n+1)", []SelectorNode{
		selectorFunction(":nth-child", selectorValue("n+1")),
	}},
	// The selector list after `of` is parsed as a selector.
	{"parses :nth-child() 2", ":nth-child(2 of &)", []SelectorNode{
		selectorFunction(":nth-child", selectorValue("2 of "), selectorLeaf("&")),
	}},
	{"parses :nth-child() 3", ":nth-child(2n + 1 of .foo, .bar)", []SelectorNode{
		selectorFunction(":nth-child",
			selectorValue("2n + 1 of "),
			selectorList(selectorLeaf(".foo"), selectorLeaf(".bar")),
		),
	}},
	{"parses &:has(.child:nth-child(2))", "&:has(.child:nth-child(2))", []SelectorNode{
		selectorCompound(
			selectorLeaf("&"),
			selectorFunction(":has", selectorCompound(
				selectorLeaf(".child"),
				selectorFunction(":nth-child", selectorValue("2")),
			)),
		),
	}},
	{"parses &:has(:nth-child(2))", "&:has(:nth-child(2))", []SelectorNode{
		selectorCompound(
			selectorLeaf("&"),
			selectorFunction(":has", selectorFunction(":nth-child", selectorValue("2"))),
		),
	}},
	{"parses nesting selector before attribute selector", "&[data-foo]", []SelectorNode{
		selectorCompound(selectorLeaf("&"), selectorLeaf("[data-foo]")),
	}},
	{"parses nesting selector after an attribute selector", "[data-foo]&", []SelectorNode{
		selectorCompound(selectorLeaf("[data-foo]"), selectorLeaf("&")),
	}},
	{"parses universal selector before attribute selector", "*[data-foo]", []SelectorNode{
		selectorCompound(selectorLeaf("*"), selectorLeaf("[data-foo]")),
	}},
	{"parses the universal selector after an attribute selector", "[data-foo]*", []SelectorNode{
		selectorCompound(selectorLeaf("[data-foo]"), selectorLeaf("*")),
	}},
	{"parses another attribute selector after an attribute selector", "[data-foo][data-bar]", []SelectorNode{
		selectorCompound(selectorLeaf("[data-foo]"), selectorLeaf("[data-bar]")),
	}},
	{"parses a type selector before an attribute selector", "div[data-foo]", []SelectorNode{
		selectorCompound(selectorLeaf("div"), selectorLeaf("[data-foo]")),
	}},
	{"parses a type selector after an attribute selector", "[data-foo]div", []SelectorNode{
		selectorCompound(selectorLeaf("[data-foo]"), selectorLeaf("div")),
	}},
	{"should parse selector lists as real selectors", ".foo[attr], .bar#id, .baz + .qux", []SelectorNode{
		selectorList(
			selectorCompound(selectorLeaf(".foo"), selectorLeaf("[attr]")),
			selectorCompound(selectorLeaf(".bar"), selectorLeaf("#id")),
			selectorComplex(selectorLeaf(".baz"), selectorCombinator("+"), selectorLeaf(".qux")),
		),
	}},
}

// TestParseSelector is upstream's `describe('parse')`.
func TestParseSelector(t *testing.T) {
	t.Parallel()
	for _, testCase := range selectorParseCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := ParseSelector(testCase.input)
			if difference := diffSelectorNodes(got, testCase.want, ""); difference != "" {
				t.Errorf("ParseSelector(%q):\n  %s\n  got:      %s\n  upstream: %s",
					testCase.input, difference, renderSelectorNodes(got), renderSelectorNodes(testCase.want))
			}
		})
	}
}

// selectorToCssCases are upstream's `describe('toCss')`, each a parse followed by a print.
var selectorToCssCases = []struct {
	name   string
	input  string
	minify bool
	want   string
}{
	{"should print a simple selector", ".foo", false, ".foo"},
	{"should print a compound selector", ".foo.bar:hover#id", false, ".foo.bar:hover#id"},
	{"should print a selector list", ".foo, .bar", false, ".foo, .bar"},
	{"should print a selector list with normalized commas 1", ".foo,.bar", false, ".foo, .bar"},
	{"should print a selector list with normalized commas 2", ".foo, .bar", false, ".foo, .bar"},
	{"should print a selector list with normalized commas 3", ".foo , .bar", false, ".foo, .bar"},
	{"should print a selector list with normalized commas 4", ".foo ,.bar", false, ".foo, .bar"},
	{"should print a selector list but minimized 1", ".foo, .bar", true, ".foo,.bar"},
	{"should print a selector list but minimized 2", ".foo , .bar", true, ".foo,.bar"},
	{"should print a selector list but minimized 3", ".foo ,.bar", true, ".foo,.bar"},
	{"should print an attribute selectors", `.foo[bar="baz"]`, false, `.foo[bar="baz"]`},
	{"should print a function", ".foo:hover:not(.bar:focus)", false, ".foo:hover:not(.bar:focus)"},
	{"should print escaped characters", `foo\.bar`, false, `foo\.bar`},
	// The `An+B` part is printed verbatim, and whitespace in the selector list after `of` is
	// normalized.
	{"should print :nth-child() 1", ":nth-child(n+1)", false, ":nth-child(n+1)"},
	{"should print :nth-child() 2", ":nth-child(+2)", false, ":nth-child(+2)"},
	{"should print :nth-child() 3", ":nth-child(2n + 1 of .foo,.bar)", false, ":nth-child(2n + 1 of .foo, .bar)"},
	{"should pretty print a complex selector", ".a+.b, .c .d[attr], .e", false, ".a + .b, .c .d[attr], .e"},
	// The one space that survives minifying is the descendant combinator, without which `.c` and
	// `.d[attr]` would print as one compound.
	{"should minify a complex selector during printing", ".a+.b, .c .d[attr], .e", true, ".a+.b,.c .d[attr],.e"},
}

// TestSelectorToCss is upstream's `describe('toCss')`.
func TestSelectorToCss(t *testing.T) {
	t.Parallel()
	for _, testCase := range selectorToCssCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if printed := SelectorToCss(ParseSelector(testCase.input), testCase.minify); printed != testCase.want {
				t.Errorf("SelectorToCss(ParseSelector(%q), %t) = %q, upstream prints %q",
					testCase.input, testCase.minify, printed, testCase.want)
			}
		})
	}
}

// TestSelectorTreeReprintsAnEdit stands in for upstream's `describe('walk')`, whose one case
// replaces `:not(…)` through `walk` and `WalkAction.Replace`. Neither is ported for this tree, so
// the replacement is done by hand. What the case pins survives: the parsed tree is an ordinary
// editable value, and SelectorToCss prints the edit rather than the original text.
func TestSelectorTreeReprintsAnEdit(t *testing.T) {
	t.Parallel()
	nodes := ParseSelector(".foo:hover:not(.bar:focus)")
	nodes = replaceSelectorNodes(nodes, func(node SelectorNode) (SelectorNode, bool) {
		if node.Kind == SelectorNodeKindFunction && node.Value == ":not" {
			return selectorLeaf(".inverted-bar"), true
		}
		return node, false
	})
	if printed := SelectorToCss(nodes, false); printed != ".foo:hover.inverted-bar" {
		t.Errorf("printed %q, upstream prints %q", printed, ".foo:hover.inverted-bar")
	}
}

// replaceSelectorNodes rebuilds a tree with every node the callback replaces swapped out, without
// descending into a replacement, as upstream's Replace does not.
func replaceSelectorNodes(nodes []SelectorNode, replace func(SelectorNode) (SelectorNode, bool)) []SelectorNode {
	if nodes == nil {
		return nil
	}
	replaced := make([]SelectorNode, 0, len(nodes))
	for _, node := range nodes {
		if replacement, ok := replace(node); ok {
			replaced = append(replaced, replacement)
			continue
		}
		node.Nodes = replaceSelectorNodes(node.Nodes, replace)
		replaced = append(replaced, node)
	}
	return replaced
}

// TestParseSelectorFollowsJavaScriptSemantics pins the behaviors listed at the top of
// selector_parser.go, none of which upstream's own tests reach. Every expectation here is
// upstream's output for the input, read off its source run under Node.
func TestParseSelectorFollowsJavaScriptSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		input   string
		want    []SelectorNode
		printed string
	}{
		// The insignificant-whitespace rule checks for end of input and a following comma, not a
		// `)`, so the space survives as a descendant combinator.
		{"whitespace before a close paren is a combinator", ":is(.a )", []SelectorNode{
			selectorFunction(":is", selectorComplex(selectorLeaf(".a"), selectorCombinator(" "))),
		}, ":is(.a )"},
		{"an unclosed value function parses its argument beside it", ":hover(abc", []SelectorNode{
			selectorCompound(selectorFunction(":hover", selectorValue("")), selectorLeaf("abc")),
		}, ":hover()abc"},
		{"an unclosed selector function keeps the remainder", ":is(.a", []SelectorNode{
			selectorFunction(":is", selectorLeaf(".a")),
		}, ":is(.a)"},
		{"an unclosed bracket is a selector of its own", "[foo", []SelectorNode{
			selectorCompound(selectorLeaf("["), selectorLeaf("foo")),
		}, "[foo"},
		{"a trailing backslash reads past the end", `.a\`, []SelectorNode{
			selectorLeaf(`.a\undefined`),
		}, `.a\undefined`},
		{"an unmatched close paren forgets the open list", ".a, .b) .c", []SelectorNode{
			selectorComplex(
				selectorList(selectorLeaf(".a"), selectorLeaf(".b")),
				selectorCombinator(" "),
				selectorLeaf(".c"),
			),
		}, ".a, .b .c"},
		{"an unterminated quote consumes only itself", `"a b`, []SelectorNode{
			selectorComplex(selectorLeaf(`"a`), selectorCombinator(" "), selectorLeaf("b")),
		}, `"a b`},
		// Outside brackets, since the attribute branch looks only for `]` and never sees a quote.
		{"a quote of the other kind does not close a string", `.a"x'y z"`, []SelectorNode{
			selectorLeaf(`.a"x'y z"`),
		}, `.a"x'y z"`},
		{"a leading explicit combinator is kept", "> .a", []SelectorNode{
			selectorComplex(selectorCombinator(">"), selectorLeaf(".a")),
		}, " > .a"},
		{"a carriage return and newline is one combinator", ".a\r\n.b", []SelectorNode{
			selectorComplex(selectorLeaf(".a"), selectorCombinator(" "), selectorLeaf(".b")),
		}, ".a .b"},
		{"of followed by a tab is not a clause", ":nth-child(2 of\t.a)", []SelectorNode{
			selectorFunction(":nth-child", selectorValue("2 of\t.a")),
		}, ":nth-child(2 of\t.a)"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := ParseSelector(testCase.input)
			if difference := diffSelectorNodes(got, testCase.want, ""); difference != "" {
				t.Errorf("ParseSelector(%q):\n  %s\n  got:      %s\n  upstream: %s",
					testCase.input, difference, renderSelectorNodes(got), renderSelectorNodes(testCase.want))
			}
			if printed := SelectorToCss(got, false); printed != testCase.printed {
				t.Errorf("SelectorToCss(ParseSelector(%q), false) = %q, upstream prints %q", testCase.input, printed, testCase.printed)
			}
		})
	}
}

// TestSelectorPredicates covers upstream's `is…Selector` helpers, which upstream's tests do not.
func TestSelectorPredicates(t *testing.T) {
	t.Parallel()
	predicates := []struct {
		name string
		test func(SelectorNode) bool
	}{
		{"universal", IsUniversalSelector},
		{"nesting", IsNestingSelector},
		{"class", IsClassSelector},
		{"id", IsIdSelector},
		{"pseudo", IsPseudoSelector},
		{"attribute", IsAttributeSelector},
		{"type", IsTypeSelector},
	}
	cases := []struct {
		node SelectorNode
		want string
	}{
		{selectorLeaf("*"), "universal"},
		{selectorLeaf("&"), "nesting"},
		{selectorLeaf(".foo"), "class"},
		{selectorLeaf("#foo"), "id"},
		{selectorLeaf("::before"), "pseudo"},
		{selectorLeaf("[data-foo]"), "attribute"},
		{selectorLeaf("div"), "type"},
		// upstream's `charCodeAt(0)` is `NaN` on an empty string, which only the type predicate's
		// default branch accepts.
		{selectorLeaf(""), "type"},
		// Not a selector node, so no predicate holds, whatever the value's first character.
		{selectorFunction(":is"), ""},
		{selectorValue(".foo"), ""},
	}
	for _, testCase := range cases {
		for _, predicate := range predicates {
			if got, want := predicate.test(testCase.node), predicate.name == testCase.want; got != want {
				t.Errorf("%s predicate on %s = %t, want %t", predicate.name, renderSelectorNodes([]SelectorNode{testCase.node}), got, want)
			}
		}
	}
}

// TestCloneSelectorNodeIsDeep covers upstream's `cloneAstNode`: a clone shares nothing with its
// original, and keeps only the fields its kind uses.
func TestCloneSelectorNodeIsDeep(t *testing.T) {
	t.Parallel()
	original := ParseSelector(".a:not(.b, .c)")[0]
	clone := CloneSelectorNode(original)
	if difference := diffSelectorNodes([]SelectorNode{clone}, []SelectorNode{original}, ""); difference != "" {
		t.Fatalf("clone differs from original: %s", difference)
	}
	clone.Nodes[1].Nodes[0].Nodes[0].Value = ".changed"
	if printed := SelectorToCss([]SelectorNode{original}, false); printed != ".a:not(.b, .c)" {
		t.Errorf("editing the clone changed the original to %q", printed)
	}

	stray := CloneSelectorNode(SelectorNode{Kind: SelectorNodeKindCompound, Value: "dropped", Nodes: []SelectorNode{}})
	if stray.Value != "" || stray.Nodes == nil {
		t.Errorf("compound clone = %+v, want no value and an empty non-nil Nodes", stray)
	}
}

// diffSelectorNodes returns the first structural disagreement between two trees, described by path,
// or the empty string when they match. A nil Nodes and an empty one differ, because upstream's
// `toEqual` tells `nodes: []` from an absent key.
func diffSelectorNodes(got, want []SelectorNode, path string) string {
	if len(got) != len(want) {
		return fmt.Sprintf("at %s: %d nodes, upstream has %d", selectorPathOrRoot(path), len(got), len(want))
	}
	for index := range want {
		here := fmt.Sprintf("%s[%d]", path, index)
		if got[index].Kind != want[index].Kind {
			return fmt.Sprintf("at %s: kind %q, upstream says %q", here, got[index].Kind, want[index].Kind)
		}
		if got[index].Value != want[index].Value {
			return fmt.Sprintf("at %s: value %q, upstream says %q", here, got[index].Value, want[index].Value)
		}
		if (got[index].Nodes == nil) != (want[index].Nodes == nil) {
			return fmt.Sprintf("at %s: nodes nil is %t, upstream's is %t", here, got[index].Nodes == nil, want[index].Nodes == nil)
		}
		if difference := diffSelectorNodes(got[index].Nodes, want[index].Nodes, here); difference != "" {
			return difference
		}
	}
	return ""
}

func selectorPathOrRoot(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

// renderSelectorNodes prints a tree compactly enough to read in a failure line.
func renderSelectorNodes(nodes []SelectorNode) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for index, node := range nodes {
		if index > 0 {
			builder.WriteString(" ")
		}
		switch node.Kind {
		case SelectorNodeKindList, SelectorNodeKindComplex, SelectorNodeKindCompound:
			fmt.Fprintf(&builder, "%s%s", node.Kind, renderSelectorNodes(node.Nodes))
		case SelectorNodeKindFunction:
			fmt.Fprintf(&builder, "function(%q)%s", node.Value, renderSelectorNodes(node.Nodes))
		default:
			fmt.Fprintf(&builder, "%s(%q)", node.Kind, node.Value)
		}
	}
	builder.WriteByte(']')
	return builder.String()
}
