package mdast

import (
	"fmt"

	"github.com/system-inc/cohere/internal/format/arena"
	"github.com/system-inc/cohere/internal/format/markdown/micromark"
)

// compileContext is upstream's CompileContext, with the event's sliceSerialize bound per handler call.
type compileContext struct {
	stack         []*Node
	tokenStack    []tokenStackEntry
	configuration *configuration
	data          compileData

	tokenizer *micromark.TokenizeContext
	offsets   []int

	// nodes is where the tree's nodes come from, nil to allocate each (#93dpede).
	nodes *arena.Arena[Node]
}

func (context *compileContext) sliceSerialize(token *micromark.Token) string {
	return context.tokenizer.SliceSerialize(token, false)
}

func (context *compileContext) top() *Node {
	return context.stack[len(context.stack)-1]
}

// point converts a micromark point, whose offset counts UTF-16 units, to a tree point in bytes.
func (context *compileContext) point(point micromark.Point) Point {
	return Point{Line: point.Line, Column: point.Column, Offset: context.offsets[point.Offset]}
}

// enter is upstream's enter: node becomes the top's last child and the new top, and token is opened.
//
// Mutates: node it is given its start position here, as upstream's enter sets node.position.
func (context *compileContext) enter(
	node *Node,
	token *micromark.Token,
	errorHandler func(*compileContext, *micromark.Token, *micromark.Token),
) {
	parent := context.top()
	parent.Children = append(parent.Children, node)
	context.stack = append(context.stack, node)
	context.tokenStack = append(context.tokenStack, tokenStackEntry{token: token, errorHandler: errorHandler})
	node.Position = &Position{Start: context.point(token.Start)}
}

func (context *compileContext) exit(
	token *micromark.Token,
	onExitError func(*compileContext, *micromark.Token, *micromark.Token),
) {
	node := context.stack[len(context.stack)-1]
	context.stack = context.stack[:len(context.stack)-1]

	if len(context.tokenStack) == 0 {
		// Invariant: micromark's events are balanced, every exit after its enter, and each handler table
		// pairs an opener with the closer of its type, so a token is open whenever one closes.
		panic(fmt.Sprintf("Cannot close `%s`: it’s not open", token.Type))
	}
	open := context.tokenStack[len(context.tokenStack)-1]
	context.tokenStack = context.tokenStack[:len(context.tokenStack)-1]

	if open.token.Type != token.Type {
		if onExitError != nil {
			onExitError(context, token, open.token)
		} else {
			handler := open.errorHandler
			if handler == nil {
				handler = defaultOnError
			}
			handler(context, token, open.token)
		}
	}

	node.Position.End = context.point(token.End)
}

// resume pops the top node and returns its text.
func (context *compileContext) resume() string {
	node := context.stack[len(context.stack)-1]
	context.stack = context.stack[:len(context.stack)-1]
	return ToString(node)
}
