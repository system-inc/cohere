// Package printing is Prettier's print core, shared by every native printer in cohere: AstPath, the
// print driver that wraps each node's doc in its comments, comment attachment, and embedding.
//
// Every Prettier printer, the JavaScript, markdown and GraphQL ones alike, is written against this core
// rather than against its own. Three Circles porting three printers would otherwise grow three copies,
// and a drift between them shows up as nothing louder than a formatting diff. So there is one, and it
// is a port of src/common/ast-path.js, src/main/ast-to-doc.js and src/main/comments.
//
// # The contract with a language
//
// The core never reads a node's fields itself. A language's node type implements Node, and its printer
// supplies the rest as hooks on Printer, the way upstream's core asks options.printer and
// options.locStart. That keeps the shape of each AST with whoever owns it: the JavaScript printer uses a
// generic node and the markdown printer a typed one, and both work.
//
// Comments are nodes, attached to nodes, and the bookkeeping lives on the nodes themselves through an
// embedded CommentFields. Not in a side table: upstream prints comments by walking them as a property,
// path.map(print, "comments"), which puts each comment at path.node, so "comments" has to be a property a
// path can navigate to. A language's Field("comments") must answer with CommentData().Comments.
//
// Positions are byte offsets into the UTF-8 source, everywhere. They come from Printer.LocStart and
// LocEnd, not from the node, because languages override them: JavaScript's start moves to a decorator
// and its end has exceptions.
package printing

// Node is what the core walks. N is the language's own node pointer type.
//
// It must be a pointer type. The print cache and comment identity are keyed on the node, the way
// upstream keys on object identity, and a value type would copy a node and lose both.
type Node[N any] interface {
	comparable

	// Type is the node's type name, upstream's node.type.
	Type() string

	// Field is upstream's node[name]: a node, a slice of nodes, nil, or a scalar. It must answer
	// "comments" with the node's attached comments.
	Field(name string) any

	// CommentData is the node's comment bookkeeping, for the node and for a comment node alike.
	CommentData() *CommentFields[N]
}
