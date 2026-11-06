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

// source is the {start: {line, column}, end: {line, column}} object the parser passes as opts.source,
// from a token's fields.
func source(startLine int, startColumn int, endLine int, endColumn int) estree.Record {
	return looseSource(estree.Integer(startLine), estree.Integer(startColumn), estree.Integer(endLine),
		estree.Integer(endColumn))
}

// looseSource is source where splitWord or pseudo reads past a space token: each field an Integer, nil for
// undefined, or NaN.
func looseSource(
	startLine estree.Value,
	startColumn estree.Value,
	endLine estree.Value,
	endColumn estree.Value,
) estree.Record {
	return estree.Record{
		"start": estree.Record{"line": startLine, "column": startColumn},
		"end":   estree.Record{"line": endLine, "column": endColumn},
	}
}

// sourceOf is node.source, mutated in place where the parser moves a start or an end.
func sourceOf(node *estree.Node) estree.Record {
	value, _ := node.Get("source").(estree.Record)
	return value
}

// spacesOf is node.spaces.
func spacesOf(node *estree.Node) estree.Record {
	value, _ := node.Get("spaces").(estree.Record)
	return value
}

// rawsOf is attribute.raws.
func rawsOf(node *estree.Node) estree.Record {
	value, _ := node.Get("raws").(estree.Record)
	return value
}

// lastOf is container.last, nil where JavaScript has undefined.
func lastOf(container *estree.Node) *estree.Node {
	nodes := container.List("nodes")
	if len(nodes) == 0 {
		return nil
	}
	return nodes[len(nodes)-1]
}
