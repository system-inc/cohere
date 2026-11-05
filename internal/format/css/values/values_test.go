package values

import (
	"math"
	"testing"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// parseNodes parses with Prettier's options and returns the Value's children.
func parseNodes(test *testing.T, text string) []*estree.Node {
	test.Helper()
	root, parseError := Parse(text, Options{Loose: true})
	if parseError != nil {
		test.Fatalf("Parse(%q): %v", text, parseError)
	}
	if !root.Is("root") || len(root.List("nodes")) != 1 || !root.List("nodes")[0].Is("value") {
		test.Fatalf("Parse(%q): want a root holding one value, got %v", text, root.Keys())
	}
	return root.List("nodes")[0].List("nodes")
}

// sourceColumn is node.source[edge].column, which the library holds as a number or NaN: the column, or
// isNaN for NaN. Any other value fails the test.
func sourceColumn(test *testing.T, node *estree.Node, edge string) (int, bool) {
	test.Helper()
	nodeSource, isMap := node.Get("source").(map[string]any)
	if !isMap {
		test.Fatalf("%s source %v, want a {start, end} object", node.Type(), node.Get("source"))
	}
	edgePosition, isMap := nodeSource[edge].(map[string]any)
	if !isMap {
		test.Fatalf("%s source %s %v, want a {line, column} object", node.Type(), edge, nodeSource[edge])
	}
	switch column := edgePosition["column"].(type) {
	case int:
		return column, false
	case float64:
		if math.IsNaN(column) {
			return 0, true
		}
	}
	test.Fatalf("%s source %s column %#v, want an int or NaN", node.Type(), edge, edgePosition["column"])
	return 0, false
}

// TestParseShapes checks, without node, the fields the oracle found that are easiest to get wrong:
// the shapes Prettier's printer and loc.js lean on, and upstream's quirks.
func TestParseShapes(test *testing.T) {
	test.Parallel()
	nodes := parseNodes(test, "-5px --foo calc(1px + 2px) #fff \"a\" /* c */")
	types := []string{"number", "word", "func", "word", "string", "comment"}
	if len(nodes) != len(types) {
		test.Fatalf("want %d nodes, got %d", len(types), len(nodes))
	}
	for index, nodeType := range types {
		if !nodes[index].Is(nodeType) {
			test.Errorf("node %d: want %s, got %s", index, nodeType, nodes[index].Type())
		}
	}

	// A signed number splits into value and unit, and its sourceIndex is the merged word's last token.
	if nodes[0].String("value") != "-5" || nodes[0].String("unit") != "px" || nodes[0].Get("sourceIndex") != 1 {
		test.Errorf(
			"-5px: got value %q unit %q sourceIndex %v",
			nodes[0].String("value"),
			nodes[0].String("unit"),
			nodes[0].Get("sourceIndex"),
		)
	}
	// --foo is "--" and "foo" merged, indexed at "foo"; loc.js's fixValueWordLoc undoes that.
	if nodes[1].String("value") != "--foo" || nodes[1].Get("sourceIndex") != 7 || rawString(nodes[1], "before") != " " {
		test.Errorf(
			"--foo: got value %q sourceIndex %v raws %v",
			nodes[1].String("value"),
			nodes[1].Get("sourceIndex"),
			nodes[1].Get("raws"),
		)
	}
	// A function holds its parens and arguments, and is balanced again at the end.
	function := nodes[2].List("nodes")
	if len(function) != 5 ||
		!function[0].Is("paren") ||
		!function[2].Is("operator") ||
		!function[4].Is("paren") ||
		nodes[2].Get("unbalanced") != float64(0) {
		test.Errorf("calc: got %d children, unbalanced %v", len(function), nodes[2].Get("unbalanced"))
	}
	// The operator's sourceIndex is its token's end line, 1, not its index.
	if function[2].Get("sourceIndex") != 1 {
		test.Errorf("operator sourceIndex: want 1 (the line), got %v", function[2].Get("sourceIndex"))
	}
	if !nodes[3].Bool("isHex") || !nodes[3].Bool("isColor") {
		test.Errorf("#fff: want isHex and isColor")
	}
	if nodes[4].String("value") != "a" || !nodes[4].Bool("quoted") || rawString(nodes[4], "quote") != "\"" {
		test.Errorf(
			"string: got %q quoted %v raws %v",
			nodes[4].String("value"),
			nodes[4].Get("quoted"),
			nodes[4].Get("raws"),
		)
	}
	if nodes[5].String("value") != " c " || nodes[5].Bool("inline") {
		test.Errorf("comment: got %q inline %v", nodes[5].String("value"), nodes[5].Get("inline"))
	}

	// A value opening with a paren ends that paren at column NaN; the next paren reuses a stale end.
	parens := parseNodes(test, "(a)")
	if column, isNaN := sourceColumn(test, parens[0], "end"); !isNaN {
		test.Errorf("first paren end column: want NaN, got %v", column)
	}
	if column, isNaN := sourceColumn(test, parens[2], "end"); isNaN || column != 2 {
		test.Errorf("closing paren end column: want 2, got %v (NaN %v)", column, isNaN)
	}

	// sourceIndex is bytes; columns stay UTF-16 units.
	multibyte := parseNodes(test, "\xc3\xa9 \xf0\x9f\x98\x80x b")
	if column, isNaN := sourceColumn(test, multibyte[2], "start"); multibyte[1].Get("sourceIndex") != 3 ||
		multibyte[2].Get("sourceIndex") != 9 ||
		isNaN ||
		column != 7 {
		test.Errorf(
			"non-ASCII: got sourceIndex %v and %v, column %v (NaN %v)",
			multibyte[1].Get("sourceIndex"),
			multibyte[2].Get("sourceIndex"),
			column,
			isNaN,
		)
	}

	// Inline comments only in loose mode.
	if comment := parseNodes(
		test,
		"a // b",
	)[1]; !comment.Is("comment") || !comment.Bool("inline") || comment.String("value") != " b" {
		test.Errorf("inline comment: got %s %q", comment.Type(), comment.String("value"))
	}
	// Outside loose mode url() arguments become one word.
	strict, parseError := Parse("url(a b)", Options{})
	if parseError != nil {
		test.Fatal(parseError)
	}
	if url := strict.List("nodes")[0].List("nodes")[0].List(
		"nodes",
	); len(url) != 3 || url[1].String("value") != "a b" || url[1].Has("isHex") {
		test.Errorf("strict url: got %d children", len(url))
	}
}

func TestParseRefusals(test *testing.T) {
	test.Parallel()
	for text, want := range map[string]string{
		"a(b":   "ParserError: Expected closing parenthesis at line: 1, column 2",
		"a) b":  "ParserError: Expected opening parenthesis at line: 1, column 2",
		"\"abc": "TokenizeError: Unclosed quote at line: 1, column: 1, token: 0",
		"a /*":  "TokenizeError: Unclosed comment at line: 1, column: 3, token: 2",
		" ":     "TypeError: Cannot read properties of undefined (reading 'raws')",
		"a + +": "TypeError: Cannot read properties of undefined (reading '0')",
	} {
		_, parseError := Parse(text, Options{Loose: true})
		if parseError == nil || !printing.IsSyntax(parseError) || parseError.Error() != want {
			test.Errorf("Parse(%q): want the syntax error %q, got %v", text, want, parseError)
		}
	}
	if _, parseError := Parse("calc(1 -2)", Options{}); parseError == nil {
		test.Errorf("calc(1 -2) without loose: want a refusal")
	}
	if _, parseError := Parse("calc(1 -2)", Options{Loose: true}); parseError != nil {
		test.Errorf("calc(1 -2) with loose: %v", parseError)
	}
}
