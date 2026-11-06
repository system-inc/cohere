package media_query

import "github.com/system-inc/cohere/internal/format/estree"

// postcss-media-query-parser 0.2.3, dist/nodes/Node.js and dist/nodes/Container.js.
//
// Both are constructors whose only state is own fields, so here they build an *estree.Node with those
// fields as properties in the constructor's assignment order: after, before, value, sourceIndex, then
// nodes for a container. type is the node's type, "" where the library leaves it undefined (a node
// whose type the post-pass in parseMediaQuery could not decide; Prettier's addMissingType fills it).
//
// A field the caller passes as undefined is a nil here. Only the root, built by index.js without
// after, before or sourceIndex, does this, so newRootContainer builds it, and Container fills each in
// place, keeping its position the way assigning an existing JavaScript property does.
//
// Not ported: parent (the brief leaves it out), and Container's walk and each, which Prettier never
// calls on this tree.

// newNode is `new Node(opts)`: a very generic node. Pretty much any element of a media query.
func newNode(after string, before string, nodeType string, value string, sourceIndex int) *estree.Node {
	return estree.New(nodeType, 0, 0,
		[]estree.Property{
			{Key: "after", Value: estree.String(after)},
			{Key: "before", Value: estree.String(before)},
			{Key: "value", Value: estree.String(value)},
			{Key: "sourceIndex", Value: estree.Integer(sourceIndex)},
		},
	)
}

// newContainer is `new Container(opts)`: a node that contains other nodes. Every field is given, so
// Container derives none.
func newContainer(
	after string,
	before string,
	nodeType string,
	value string,
	sourceIndex int,
	nodes []*estree.Node,
) *estree.Node {
	container := newNode(after, before, nodeType, value, sourceIndex)

	container.Set("nodes", estree.Nodes(nodes))

	return container
}

// newRootContainer is `new Container(opts)` with after, before and sourceIndex undefined in opts, as
// index.js builds the media-query-list: Container derives each from the nodes.
func newRootContainer(nodeType string, value string, nodes []*estree.Node) *estree.Node {
	container := estree.New(nodeType, 0, 0,
		[]estree.Property{
			{Key: "after"},
			{Key: "before"},
			{Key: "value", Value: estree.String(value)},
			{Key: "sourceIndex"},
		},
	)

	container.Set("nodes", estree.Nodes(nodes))

	if len(nodes) > 0 {
		container.Set("after", nodes[len(nodes)-1].Get("after"))
	} else {
		container.Set("after", estree.String(""))
	}

	if len(nodes) > 0 {
		container.Set("before", nodes[0].Get("before"))
	} else {
		container.Set("before", estree.String(""))
	}

	// this.before.length: before is whitespace from the input, so its byte length is the byte offset.
	container.Set("sourceIndex", estree.Integer(len(container.String("before"))))

	return container
}
