// Port of postcss-values-parser 2.0.1 lib/node.js, lib/container.js and the node classes: root.js,
// value.js, atword.js, colon.js, comma.js, comment.js, function.js, number.js, operator.js, paren.js,
// string.js, unicode-range.js, word.js.
//
// Each constructor builds the estree node with the own enumerable fields its class leaves on the
// JavaScript object, in the order JavaScript assigns them: Node's constructor sets raws, then copies the
// options it was given (value, source, sourceIndex and the class's extras), then the class sets type
// and its own fields. type is the node's type rather than a property. parent is left out, and so is
// everything functional (toString, clone, walk and the rest), which the parser never calls.

package values

import (
	"math"

	"github.com/system-inc/cohere/internal/format/estree"
)

// sourceSpan is a node's source, { start: { line, column }, end: { line, column } }, as the parser
// computes it. Columns are in UTF-16 units, the library's own. The end column is NaN where upstream
// computes it from an undefined variable; no start column ever is.
type sourceSpan struct {
	startLine   int
	startColumn int
	endLine     int
	endColumn   int
	// endColumnIsNaN is an end column computed from an undefined `next`.
	endColumnIsNaN bool
}

// setSource writes nodeSource into node.source as the plain object the glue reads, each line and column
// an Integer and a NaN end column a Number. A constructor holds the key with nil in its place, so Set
// keeps it where JavaScript assigns it. The records are built only here, where they are handed to the
// node; the parser carries the typed sourceSpan.
func setSource(node *estree.Node, nodeSource sourceSpan) {
	end := estree.Record{"line": estree.Integer(nodeSource.endLine), "column": estree.Integer(nodeSource.endColumn)}
	if nodeSource.endColumnIsNaN {
		end["column"] = estree.Number(math.NaN())
	}
	node.Set("source", estree.Record{
		"start": estree.Record{
			"line":   estree.Integer(nodeSource.startLine),
			"column": estree.Integer(nodeSource.startColumn),
		},
		"end": end,
	})
}

// Root: Container with type 'root'.
func newRoot() *estree.Node {
	return estree.New("root", 0, 0, []estree.Property{
		{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
		{Key: "nodes", Value: estree.Nodes{}},
	})
}

// Value: Container with type 'value' and unbalanced 0.
func newValue() *estree.Node {
	return estree.New(
		"value",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "nodes", Value: estree.Nodes{}},
			{Key: "unbalanced", Value: estree.Number(0)},
		},
	)
}

// leaf builds the Node subclasses that take { value, source, sourceIndex }: colon, comma, operator,
// unicode-range and word.
func leaf(nodeType string, value string, nodeSource sourceSpan, sourceIndex int) *estree.Node {
	node := estree.New(
		nodeType,
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
		},
	)
	setSource(node, nodeSource)
	return node
}

// Paren: Node with type 'paren' and parenType "".
func newParen(value string, nodeSource sourceSpan, sourceIndex int) *estree.Node {
	node := leaf("paren", value, nodeSource, sourceIndex)
	node.Set("parenType", estree.String(""))
	return node
}

// Comment: Node with type 'comment' and inline from its options.
func newComment(value string, inline bool, nodeSource sourceSpan, sourceIndex int) *estree.Node {
	node := estree.New(
		"comment",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "inline", Value: estree.Boolean(inline)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
		},
	)
	setSource(node, nodeSource)
	return node
}

// AtWord: Container with type 'atword'.
func newAtWord(value string, nodeSource sourceSpan, sourceIndex int) *estree.Node {
	node := estree.New(
		"atword",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "nodes", Value: estree.Nodes{}},
		},
	)
	setSource(node, nodeSource)
	return node
}

// NumberNode: Node with type 'number' and unit from its options, or "".
func newNumber(value string, nodeSource sourceSpan, sourceIndex int, unit string) *estree.Node {
	node := estree.New(
		"number",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "unit", Value: estree.String(unit)},
		},
	)
	setSource(node, nodeSource)
	return node
}

// FunctionNode: Container with type 'func', unbalanced starting at -1 so the parser knows no parens
// have been added yet.
func newFunction(value string, nodeSource sourceSpan, sourceIndex int) *estree.Node {
	node := estree.New(
		"func",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "nodes", Value: estree.Nodes{}},
			{Key: "unbalanced", Value: estree.Number(-1)},
		},
	)
	setSource(node, nodeSource)
	return node
}

// StringNode: Node with type 'string' and quoted from its options.
func newString(value string, nodeSource sourceSpan, sourceIndex int, quoted bool) *estree.Node {
	node := estree.New(
		"string",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: estree.Record{"before": estree.String(""), "after": estree.String("")}},
			{Key: "value", Value: estree.String(value)},
			{Key: "source"},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "quoted", Value: estree.Boolean(quoted)},
		},
	)
	setSource(node, nodeSource)
	return node
}

// rawString is node.raws[key], which this package only ever holds as a string.
func rawString(node *estree.Node, key string) string {
	nodeRaws, isRecord := node.Get("raws").(estree.Record)
	if !isRecord {
		// Invariant: every constructor above sets raws to an object; Parse recovers the panic into an
		// error.
		panic("css values: node.raws is not an object")
	}
	raw, isString := nodeRaws[key].(estree.String)
	if !isString {
		// Invariant: the parser reads only before and after, which every constructor sets to "" and
		// every append keeps a string; Parse recovers the panic into an error.
		panic("css values: node.raws." + key + " is not a string")
	}
	return string(raw)
}

// setRaw is node.raws[key] = text.
func setRaw(node *estree.Node, key string, text string) {
	nodeRaws, isRecord := node.Get("raws").(estree.Record)
	if !isRecord {
		// Invariant: every constructor above sets raws to an object; Parse recovers the panic into an
		// error.
		panic("css values: node.raws is not an object")
	}
	nodeRaws[key] = estree.String(text)
}

// appendRaw is node.raws[key] += text.
func appendRaw(node *estree.Node, key string, text string) {
	setRaw(node, key, rawString(node, key)+text)
}

// appendNode is Container's append: node.parent = this; this.nodes.push(node).
func appendNode(container *estree.Node, node *estree.Node) {
	container.Set("nodes", estree.Nodes(append(container.List("nodes"), node)))
}

// last is Container's `get last`: the last child, or nil.
func last(container *estree.Node) *estree.Node {
	nodes := container.List("nodes")
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}

func unbalanced(node *estree.Node) float64 {
	value, _ := node.Get("unbalanced").(estree.Number)
	return float64(value)
}

func setUnbalanced(node *estree.Node, value float64) {
	node.Set("unbalanced", estree.Number(value))
}
