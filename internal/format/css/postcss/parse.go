package postcss

import (
	"errors"
	"fmt"

	"github.com/system-inc/cohere/internal/format/estree"
	"github.com/system-inc/cohere/internal/format/printing"
)

// postcss 8.5.16, lib/parse.js, called the way Prettier's parser-postcss.js parseWithParser calls it:
// postcssParse.default(text, { map: false }), with no `from`, so the hint parse appends to a
// CssSyntaxError's message for a .scss, .sass or .less file never applies.

// Parse is postcss's parse: the Root of the tree, or the CssSyntaxError the parser threw, marked as a
// parse failure with printing.Syntax. Recover with errors.As(err, new(*CssSyntaxError)) for the name,
// reason, line and column Prettier builds its error from.
//
// The tree is postcss's, as estree.Nodes. A node's type is postcss's type ("root", "rule", "atrule",
// "decl", "comment") and its properties are the object's own enumerable fields in the order postcss
// creates them, leaving out parent and source.input:
//
//   - raws, an estree.Record: before, after, between, semicolon, afterName, important, ownSemicolon,
//     left, right, and selector, params or value as {raw, value} maps where Parser.raw kept the source
//     text apart from the clean value.
//   - nodes, an estree.Nodes, on the root, rules and at-rules with a block.
//   - source, an estree.Record of start and end, each {offset, line, column}: offset in bytes into the
//     text after any byte order mark, line and column postcss's (column one-based, in UTF-16 units). An
//     at-rule ending the file with nothing after its name has no end, as in postcss.
//   - selector; name and params; prop, important and value; text.
//
// A node's Range is [source.start.offset, source.end.offset] in bytes where postcss set both, and
// [0, 0] where it set no end. Prettier's positions are loc.js's, which the glue computes from source.
func Parse(text string) (*estree.Node, error) {
	return parseWith(text, newParser)
}

// parseWith runs a parser, postcss's or postcss-scss's, over the text: new Input(text), new Parser(input),
// parser.parse(), parser.root.
func parseWith(text string, construct func(in *input) *parser) (root *estree.Node, parseWithError error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			root = nil
			if syntaxError, isSyntaxError := recovered.(*CssSyntaxError); isSyntaxError {
				// Marked as a parse failure, so the format phase skips the file rather than reporting a
				// broken formatter.
				parseWithError = printing.Syntax(syntaxError)
				return
			}
			if typeError, isTypeError := recovered.(*TypeError); isTypeError {
				// postcss-scss reaching past its tokens, a TypeError upstream that Prettier lets escape: the
				// file does not parse, so it is marked the same way.
				parseWithError = printing.Syntax(typeError)
				return
			}
			// Anything else is a bug in the port, where upstream would have thrown a TypeError; it is
			// reported, not marked as a syntax error, and never escapes as a panic.
			parseWithError = errors.New(fmt.Sprint("postcss: ", recovered))
		}
	}()

	in := newInput(text)
	parser := construct(in)
	parser.parse()

	return toEstree(parser.root, in), nil
}

// toEstree hands a node out as an estree.Node, offsets crossing from UTF-16 units to bytes. raws and
// source become the plain objects the css glue reads, built only here, where they are handed to the node;
// the parser carries them typed.
func toEstree(each *node, in *input) *estree.Node {
	start, end := 0, 0
	if each.source != nil && each.source.start != nil && each.source.end != nil {
		start = in.byteOffset(each.source.start.offset)
		end = in.byteOffset(each.source.end.offset)
	}
	// raws holds each raw as postcss does: text as a string, a flag as a boolean, and Parser.raw's pair as
	// {raw, value}, with postcss-scss's scss beside them where it set one.
	raws := make(estree.Record, len(each.raws))
	for key, entry := range each.raws {
		switch typed := entry.(type) {
		case rawText:
			raws[key] = estree.String(typed)
		case rawFlag:
			raws[key] = estree.Boolean(typed)
		case *rawValue:
			pair := estree.Record{"raw": estree.String(typed.raw), "value": estree.String(typed.value)}
			if typed.hasScss {
				pair["scss"] = estree.String(typed.scss)
			}
			raws[key] = pair
		}
	}
	result := estree.New(each.nodeType, start, end, []estree.Property{{Key: "raws", Value: raws}})
	for _, key := range each.keys {
		switch key {
		case "nodes":
			children := make(estree.Nodes, len(each.nodes))
			for index, child := range each.nodes {
				children[index] = toEstree(child, in)
			}
			result.Set("nodes", children)
		case "source":
			// Each position's offset is in bytes; its column stays in UTF-16 units, as postcss counts.
			nodeSource := estree.Record{
				"start": estree.Record{
					"column": estree.Integer(each.source.start.column),
					"line":   estree.Integer(each.source.start.line),
					"offset": estree.Integer(in.byteOffset(each.source.start.offset)),
				},
			}
			if each.source.end != nil {
				nodeSource["end"] = estree.Record{
					"column": estree.Integer(each.source.end.column),
					"line":   estree.Integer(each.source.end.line),
					"offset": estree.Integer(in.byteOffset(each.source.end.offset)),
				}
			}
			result.Set("source", nodeSource)
		case "selector":
			result.Set("selector", estree.String(each.selector))
		case "name":
			result.Set("name", estree.String(each.name))
		case "params":
			result.Set("params", estree.String(each.params))
		case "prop":
			result.Set("prop", estree.String(each.prop))
		case "important":
			result.Set("important", estree.Boolean(each.important))
		case "value":
			result.Set("value", estree.String(each.value))
		case "text":
			result.Set("text", estree.String(each.text))
		case "isNested":
			result.Set("isNested", estree.Boolean(each.isNested))
		default:
			// Invariant: keys holds only the names the node constructors list and the parser marks, and
			// each of them has an arm here; Parse recovers the panic into an error.
			panic("postcss: no field " + key)
		}
	}
	return result
}
