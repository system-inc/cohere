package selector

// postcss-selector-parser 2.2.3, dist/selectors/*.js: the node classes, reduced to what their
// constructors leave on the object. The methods (toString, walk, clone, remove...) are not ported:
// Prettier reads the tree the parser builds and never calls them on it.
//
// Every class's constructor runs node.js's first: it copies the options object's own keys onto the
// node in order (an undefined value included, as an own property that is undefined, which is a nil
// property here), then sets spaces. container.js adds nodes, the subclass sets type, and nesting.js and
// universal.js overwrite value, attribute.js adds raws. The JS object's type is the estree node's type.
//
// parent is left off the nodes, as the brief has it; the parser keeps it on the side (see parser.parents).

import "github.com/system-inc/cohere/internal/format/estree"

// types.js.
const (
	typeTag        = "tag"
	typeString     = "string"
	typeSelector   = "selector"
	typeRoot       = "root"
	typePseudo     = "pseudo"
	typeNesting    = "nesting"
	typeID         = "id"
	typeComment    = "comment"
	typeCombinator = "combinator"
	typeClass      = "class"
	typeAttribute  = "attribute"
	typeUniversal  = "universal"
)

// newSelectorNode is node.js's constructor followed by the subclass's: opts are the options object's
// keys and values in order. A sourceRange given as opts.source becomes upstream's object in its place.
func newSelectorNode(nodeType string, options ...any) *estree.Node {
	node := estree.New(nodeType, 0, 0, options...)
	if nodeSource, isRange := node.Get("source").(sourceRange); isRange {
		// Set keeps the key where opts put it, so the node's keys stay in upstream's order.
		end := map[string]any{"line": nodeSource.endLine, "column": nodeSource.endColumn}
		if nodeSource.endColumnUndefined {
			end["column"] = nil
		}
		node.Set("source", map[string]any{
			"start": map[string]any{"line": nodeSource.startLine, "column": nodeSource.startColumn},
			"end":   end,
		})
	}
	// Prettier's parser never passes opts.spaces, so before and after are always the '' defaults.
	node.Set("spaces", map[string]any{"before": "", "after": ""})
	switch nodeType {
	case typeRoot, typeSelector, typePseudo:
		// container.js: if (!this.nodes) this.nodes = [].
		if node.Get("nodes") == nil {
			node.Set("nodes", []*estree.Node{})
		}
	case typeNesting:
		node.Set("value", "&")
	case typeUniversal:
		node.Set("value", "*")
	case typeAttribute:
		node.Set("raws", map[string]any{})
	}
	return node
}

// sourceRange is the {start: {line, column}, end: {line, column}} object the parser passes as
// opts.source, which newSelectorNode writes out as upstream's object. Every line and column is a
// token's number, except where pseudo reads a space token's [5], past its end: endColumnUndefined
// marks that undefined.
type sourceRange struct {
	startLine          int
	startColumn        int
	endLine            int
	endColumn          int
	endColumnUndefined bool
}

// source is the sourceRange of a token's numbers.
func source(startLine int, startColumn int, endLine int, endColumn int) sourceRange {
	return sourceRange{startLine: startLine, startColumn: startColumn, endLine: endLine, endColumn: endColumn}
}

// setSourceEdge is `node.source[edge][key] = value`, the parser moving a start or an end in place.
func setSourceEdge(node *estree.Node, edge string, key string, value int) {
	nodeSource, _ := node.Get("source").(map[string]any)
	position, isMap := nodeSource[edge].(map[string]any)
	if !isMap {
		// Invariant: the parser moves an edge only on a combinator or pseudo newSelectorNode built
		// from a sourceRange, which sets both edges to a map; Parse recovers the panic into an error.
		panic("css selector parser: node.source." + edge + " is not a {line, column} object")
	}
	position[key] = value
}

// setSpace is `node.spaces[side] = text`, side being before or after.
func setSpace(node *estree.Node, side string, text string) {
	spaces, _ := node.Get("spaces").(map[string]any)
	spaces[side] = text
}

// setRaw is `attribute.raws[key] = text`.
func setRaw(node *estree.Node, key string, text string) {
	raws, _ := node.Get("raws").(map[string]any)
	raws[key] = text
}

// lastOf is container.last, nil where JavaScript has undefined.
func lastOf(container *estree.Node) *estree.Node {
	nodes := container.List("nodes")
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}
