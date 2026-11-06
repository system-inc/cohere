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

// toEstree hands a node out as an estree.Node, offsets crossing from UTF-16 units to bytes.
func toEstree(each *node, in *input) *estree.Node {
	start, end := 0, 0
	if each.source != nil && each.source.start != nil && each.source.end != nil {
		start = in.byteOffset(each.source.start.offset)
		end = in.byteOffset(each.source.end.offset)
	}
	result := estree.New(each.nodeType, start, end, []estree.Property{{Key: "raws", Value: rawsToRecord(each.raws)}})
	for _, key := range each.keys {
		switch key {
		case "nodes":
			children := make(estree.Nodes, len(each.nodes))
			for index, child := range each.nodes {
				children[index] = toEstree(child, in)
			}
			result.Set("nodes", children)
		case "source":
			result.Set("source", sourceToRecord(each.source, in))
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
			panic("postcss: no field " + key)
		}
	}
	return result
}

// rawsToRecord is the parser's raws as the node's: a string or a flag as itself, and a rawValue as its
// {raw, value} object.
func rawsToRecord(raws map[string]any) estree.Record {
	result := make(estree.Record, len(raws))
	for key, value := range raws {
		switch typed := value.(type) {
		case string:
			result[key] = estree.String(typed)
		case bool:
			result[key] = estree.Boolean(typed)
		case *rawValue:
			rawRecord := estree.Record{"raw": estree.String(typed.raw), "value": estree.String(typed.value)}
			if typed.hasScss {
				rawRecord["scss"] = estree.String(typed.scss)
			}
			result[key] = rawRecord
		default:
			// Invariant: the parsers write a raw only as a string, a flag (inline, semicolon) or a rawValue;
			// parseWith recovers the panic into an error.
			panic(fmt.Sprintf("postcss: raws.%s is a %T", key, value))
		}
	}
	return result
}

func sourceToRecord(each *source, in *input) estree.Record {
	result := estree.Record{"start": positionToRecord(each.start, in)}
	if each.end != nil {
		result["end"] = positionToRecord(each.end, in)
	}
	return result
}

// positionToRecord is a position with its offset in bytes; column stays in UTF-16 units, as postcss
// counts.
func positionToRecord(each *position, in *input) estree.Record {
	return estree.Record{
		"column": estree.Integer(each.column),
		"line":   estree.Integer(each.line),
		"offset": estree.Integer(in.byteOffset(each.offset)),
	}
}
