package postcss

// The node classes the parser builds: lib/root.js, lib/rule.js, lib/at-rule.js, lib/declaration.js and
// lib/comment.js, over lib/node.js and lib/container.js, reduced to their fields.
//
// A node is plain data while the parser runs, with offsets in UTF-16 units, and becomes an estree.Node
// in parse.go. keys records the order upstream creates its own enumerable fields in, after raws (which
// Node's constructor creates first) and type (the estree.Node's type), so the estree.Node's properties
// come out in the same order a for-in over the JavaScript object visits them. parent is kept for the
// parser and not handed out.
//
// Node's methods (clone, remove, toString, the proxy, the visitor) and Container's (each, walk,
// insertBefore, normalize) are not here: the parser calls only Container's push.

type node struct {
	nodeType string
	raws     map[string]rawEntry
	keys     []string

	parent *node
	nodes  []*node
	source *source

	// Rule.
	selector string
	// AtRule.
	name       string
	parameters string
	// Declaration.
	property  string
	value     string
	important bool
	// Comment.
	text string
	// postcss-scss's NestedDeclaration (nested_declaration.go), a decl with nodes.
	isNested bool
}

// source is node.source without input: { start, end }, end unset until the parser finds it.
type source struct {
	start *position
	end   *position
}

// position is what Parser.getPosition returns: { column, line, offset }, offset in UTF-16 units.
type position struct {
	column int
	line   int
	offset int
}

// rawValue is what Parser.raw stores in raws[prop] when the clean value differs from the source text.
//
// scss is the field postcss-scss's raw adds, the text with `//` comments as written, where its raw
// (the same comments rewritten as /* */) differs; hasScss says it is set.
type rawValue struct {
	raw   string
	value string

	scss    string
	hasScss bool
}

// rawEntry is one value of a node's raws: text (rawText), a flag (rawFlag), or the {raw, value} pair
// Parser.raw stores (*rawValue). parse.go hands each out as the string, bool or object postcss holds.
type rawEntry interface{ rawEntry() }

// rawText is a raw that is text: before, after, between, afterName, important, left, right and the like.
type rawText string

// rawFlag is a raw that is a boolean: semicolon, and postcss-scss's inline.
type rawFlag bool

func (rawText) rawEntry()   {}
func (rawFlag) rawEntry()   {}
func (*rawValue) rawEntry() {}

// mark records that the field exists, the first time it is assigned.
func (each *node) mark(key string) {
	for _, existing := range each.keys {
		if existing == key {
			return
		}
	}
	each.keys = append(each.keys, key)
}

// Root's constructor: `if (!this.nodes) this.nodes = []`.
func newRoot() *node {
	return &node{nodeType: "root", raws: map[string]rawEntry{}, keys: []string{"nodes"}, nodes: []*node{}}
}

// Rule's constructor: `if (!this.nodes) this.nodes = []`.
func newRule() *node {
	return &node{nodeType: "rule", raws: map[string]rawEntry{}, keys: []string{"nodes"}, nodes: []*node{}}
}

// AtRule's constructor leaves nodes unset; the parser creates them when a block opens.
func newAtRule() *node {
	return &node{nodeType: "atrule", raws: map[string]rawEntry{}}
}

func newDeclaration() *node {
	return &node{nodeType: "decl", raws: map[string]rawEntry{}}
}

func newComment() *node {
	return &node{nodeType: "comment", raws: map[string]rawEntry{}}
}

// push is Container's push: the child joins this node's nodes with this node as its parent.
//
// Mutates: child its parent becomes this node, as Container's push sets it on the node it is handed.
func (each *node) push(child *node) {
	child.parent = each
	each.nodes = append(each.nodes, child)
}

// hasNodes is `this.nodes` being truthy: a container whose nodes array exists, empty or not.
func (each *node) hasNodes() bool {
	return each.nodes != nil
}

// rawString is `this.raws[key] || ""` read as a string: the empty string where it is unset.
func (each *node) rawString(key string) string {
	text, _ := each.raws[key].(rawText)
	return string(text)
}

// setField is `node[prop] = value` for the three fields Parser.raw writes.
func (each *node) setField(property string, value string) {
	switch property {
	case "selector":
		each.selector = value
	case "params":
		each.parameters = value
	case "value":
		each.value = value
	default:
		// Invariant: Parser.raw, the only caller, is handed "selector" by rule, "params" by atrule and
		// "value" by decl and postcss-scss's nested rule, and no other property; Parse recovers the panic
		// into an error.
		panic("postcss: raw writes selector, params or value, not " + property)
	}
	each.mark(property)
}

// emptyNodes is `[]`, for a parser method whose local variable is named node, as upstream's is.
func emptyNodes() []*node {
	return []*node{}
}
