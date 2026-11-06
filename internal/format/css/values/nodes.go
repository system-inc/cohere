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

import "github.com/system-inc/cohere/internal/format/estree"

// newRaws is Node's `this.raws = { before: "", after: "" }`.
func newRaws() estree.Record {
	return estree.Record{"before": estree.String(""), "after": estree.String("")}
}

// position is a source start or end: { line, column }. column is in UTF-16 units, the library's own,
// and is NaN (a Number) where upstream computes it from an undefined variable, an Integer otherwise.
func position(line int, column estree.Value) estree.Record {
	return estree.Record{"line": estree.Integer(line), "column": column}
}

func source(start estree.Record, end estree.Record) estree.Record {
	return estree.Record{"start": start, "end": end}
}

// Root: Container with type 'root'.
func newRoot() *estree.Node {
	return estree.New("root", 0, 0, []estree.Property{
		{Key: "raws", Value: newRaws()},
		{Key: "nodes", Value: estree.Nodes{}},
	})
}

// Value: Container with type 'value' and unbalanced 0.
func newValue() *estree.Node {
	return estree.New("value", 0, 0, []estree.Property{
		{Key: "raws", Value: newRaws()},
		{Key: "nodes", Value: estree.Nodes{}},
		{Key: "unbalanced", Value: estree.Number(0)},
	})
}

// leaf builds the Node subclasses that take { value, source, sourceIndex }: colon, comma, operator,
// unicode-range and word.
func leaf(nodeType string, value string, nodeSource estree.Record, sourceIndex int) *estree.Node {
	return estree.New(
		nodeType,
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
		},
	)
}

// Paren: Node with type 'paren' and parenType "".
func newParen(value string, nodeSource estree.Record, sourceIndex int) *estree.Node {
	node := leaf("paren", value, nodeSource, sourceIndex)
	node.Set("parenType", estree.String(""))
	return node
}

// Comment: Node with type 'comment' and inline from its options.
func newComment(value string, inline bool, nodeSource estree.Record, sourceIndex int) *estree.Node {
	return estree.New(
		"comment",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "inline", Value: estree.Boolean(inline)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
		},
	)
}

// AtWord: Container with type 'atword'.
func newAtWord(value string, nodeSource estree.Record, sourceIndex int) *estree.Node {
	return estree.New(
		"atword",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "nodes", Value: estree.Nodes{}},
		},
	)
}

// NumberNode: Node with type 'number' and unit from its options, or "".
func newNumber(value string, nodeSource estree.Record, sourceIndex int, unit string) *estree.Node {
	return estree.New(
		"number",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "unit", Value: estree.String(unit)},
		},
	)
}

// FunctionNode: Container with type 'func', unbalanced starting at -1 so the parser knows no parens
// have been added yet.
func newFunc(value string, nodeSource estree.Record, sourceIndex int) *estree.Node {
	return estree.New(
		"func",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "nodes", Value: estree.Nodes{}},
			{Key: "unbalanced", Value: estree.Number(-1)},
		},
	)
}

// StringNode: Node with type 'string' and quoted from its options.
func newString(value string, nodeSource estree.Record, sourceIndex int, quoted bool) *estree.Node {
	return estree.New(
		"string",
		0,
		0,
		[]estree.Property{
			{Key: "raws", Value: newRaws()},
			{Key: "value", Value: estree.String(value)},
			{Key: "source", Value: nodeSource},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
			{Key: "quoted", Value: estree.Boolean(quoted)},
		},
	)
}

// raws is node.raws.
func raws(node *estree.Node) estree.Record {
	nodeRaws, isMap := node.Get("raws").(estree.Record)
	if !isMap {
		// Invariant: every constructor above sets raws to newRaws(); Parse recovers the panic into an
		// error.
		panic("css values: node.raws is not an object")
	}
	return nodeRaws
}

// appendRaw is node.raws[key] += text.
func appendRaw(node *estree.Node, key string, text string) {
	nodeRaws := raws(node)
	raw, isString := nodeRaws[key].(estree.String)
	if !isString {
		// Invariant: the parser appends only to before and after, which newRaws() sets to "" and every
		// append keeps a string; Parse recovers the panic into an error.
		panic("css values: node.raws." + key + " is not a string")
	}
	nodeRaws[key] = raw + estree.String(text)
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
