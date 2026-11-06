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
	typeId         = "id"
	typeComment    = "comment"
	typeCombinator = "combinator"
	typeClass      = "class"
	typeAttribute  = "attribute"
	typeUniversal  = "universal"
)

// newSelectorNode is node.js's constructor followed by the subclass's: options are the options object's
// keys and values in order.
func newSelectorNode(nodeType string, options []estree.Property) *estree.Node {
	node := estree.New(nodeType, 0, 0, options)
	// Prettier's parser never passes opts.spaces, so before and after are always the '' defaults.
	node.Set("spaces", estree.Record{"before": estree.String(""), "after": estree.String("")})
	switch nodeType {
	case typeRoot, typeSelector, typePseudo:
		// container.js: if (!this.nodes) this.nodes = [].
		if node.Get("nodes") == nil {
			node.Set("nodes", estree.Nodes{})
		}
	case typeNesting:
		node.Set("value", estree.String("&"))
	case typeUniversal:
		node.Set("value", estree.String("*"))
	case typeAttribute:
		node.Set("raws", estree.Record{})
	}
	return node
}

// sourceRange is the {start: {line, column}, end: {line, column}} object the parser passes as
// opts.source, which sourceRecord writes out as upstream's object. Every line and column is a token's
// number, except where pseudo reads a space token's [5], past its end: endColumnUndefined marks that
// undefined.
type sourceRange struct {
	startLine          int
	startColumn        int
	endLine            int
	endColumn          int
	endColumnUndefined bool
}

// source is upstream's opts.source object of a token's numbers.
func source(startLine int, startColumn int, endLine int, endColumn int) estree.Record {
	return sourceRecord(
		sourceRange{startLine: startLine, startColumn: startColumn, endLine: endLine, endColumn: endColumn},
	)
}

// sourceRecord is a sourceRange as upstream's object, its end column nil where it is undefined.
func sourceRecord(nodeSource sourceRange) estree.Record {
	end := estree.Record{"line": estree.Integer(nodeSource.endLine), "column": estree.Integer(nodeSource.endColumn)}
	if nodeSource.endColumnUndefined {
		end["column"] = nil
	}
	return estree.Record{
		"start": estree.Record{
			"line":   estree.Integer(nodeSource.startLine),
			"column": estree.Integer(nodeSource.startColumn),
		},
		"end": end,
	}
}

// setSourceEdge is `node.source[edge][key] = value`, the parser moving a start or an end in place.
func setSourceEdge(node *estree.Node, edge string, key string, value int) {
	nodeSource, _ := node.Get("source").(estree.Record)
	position, isRecord := nodeSource[edge].(estree.Record)
	if !isRecord {
		// Invariant: the parser moves an edge only on a combinator or pseudo it built with a source
		// record, which sets both edges to a record; Parse recovers the panic into an error.
		panic("css selector parser: node.source." + edge + " is not a {line, column} object")
	}
	position[key] = estree.Integer(value)
}

// setSpace is `node.spaces[side] = text`, side being before or after.
func setSpace(node *estree.Node, side string, text string) {
	spaces, _ := node.Get("spaces").(estree.Record)
	spaces[side] = estree.String(text)
}

// setRaw is `attribute.raws[key] = text`.
func setRaw(node *estree.Node, key string, text string) {
	raws, _ := node.Get("raws").(estree.Record)
	raws[key] = estree.String(text)
}

// lastOf is container.last, nil where JavaScript has undefined.
func lastOf(container *estree.Node) *estree.Node {
	nodes := container.List("nodes")
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}
