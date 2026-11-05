package unist

// Ported from yaml-unist-parser 3.2.0, dist/transforms/: transform.mjs, alias.mjs, plain.mjs,
// quote-double.mjs, quote-single.mjs, quote-value.mjs, block-folded.mjs, block-literal.mjs,
// block-value.mjs, content.mjs and directive.mjs.

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/system-inc/cohere/internal/format/yaml/compose"
	"github.com/system-inc/cohere/internal/format/yaml/cst"
	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// transformNode is upstream's transformNode. props is upstream's { tokens }: the tag, anchor and comment
// tokens in front of the node.
func (context *context) transformNode(node *compose.Node, properties []*cst.Token) *Node {
	if node == nil {
		return nil
	}
	if compose.IsAlias(node) {
		return context.transformAlias(node, properties)
	}
	if compose.IsMap(node) {
		if node.Flow {
			return context.transformFlowMap(node, properties)
		}
		return context.transformMap(node, properties)
	}
	if compose.IsSeq(node) {
		if node.Flow {
			return context.transformFlowSeq(node, properties)
		}
		return context.transformSeq(node, properties)
	}
	if compose.IsScalar(node) {
		switch node.Type {
		case compose.BlockFolded:
			return context.transformBlockValue("blockFolded", node, properties)
		case compose.BlockLiteral:
			return context.transformBlockValue("blockLiteral", node, properties)
		case compose.Plain:
			return context.transformPlain(node, properties)
		case compose.QuoteDouble:
			return context.transformQuoteValue("quoteDouble", node, properties)
		case compose.QuoteSingle:
			return context.transformQuoteValue("quoteSingle", node, properties)
		}
		scalarType := node.Type
		if scalarType == "" {
			scalarType = "undefined"
		}
		throwError("Unexpected scalar type: %s", scalarType)
	}
	throwError("Unexpected unknown node type")
	return nil
}

// isEmptyNode is upstream's isEmptyNode:
// `!node || node.range[0] === node.range[1] && props.tokens.every((t) => t.type === "comment")`.
func isEmptyNode(node *compose.Node, properties []*cst.Token) bool {
	if node == nil {
		return true
	}
	if node.Range == nil {
		// The key resolvePairs makes from nothing has no range.
		throwTypeError("undefined", "0")
	}
	if node.Range[0] != node.Range[1] {
		return false
	}
	for _, token := range properties {
		if token.Type != "comment" {
			return false
		}
	}
	return true
}

// transformAlias is upstream's transformAlias.
func (context *context) transformAlias(alias *compose.Node, properties []*cst.Token) *Node {
	sourceToken := alias.SrcToken
	if sourceToken == nil {
		throwTypeError("undefined", "end")
	}
	for _, token := range context.extractComments(sourceToken.End) {
		throwError("Unexpected token type in alias end: %s", token.Type)
	}
	return context.createAlias(context.transformRange(alias.Range[0], alias.Range[1]),
		context.transformContentProperties(alias, properties), alias.Source)
}

// transformBlockValue is transformBlockFolded and transformBlockLiteral, which differ only in the
// factory they hand transformAstBlockValue's result to.
func (context *context) transformBlockValue(nodeType string, blockValue *compose.Node, properties []*cst.Token) *Node {
	sourceToken := blockValue.SrcToken
	if sourceToken == nil || sourceToken.Type != "block-scalar" {
		throwError("Expected block scalar srcToken")
	}
	return context.transformAstBlockValue(nodeType, blockValue, sourceToken, properties)
}

// transformAstBlockValue is upstream's transformAstBlockValue.
func (context *context) transformAstBlockValue(
	nodeType string,
	blockValue *compose.Node,
	sourceToken *cst.Token,
	properties []*cst.Token,
) *Node {
	var blockScalarHeaderToken *cst.Token
	var indicatorComment *Node
	for _, token := range tokens(sourceToken.Props) {
		if token.Type == "comment" {
			indicatorComment = context.transformComment(token)
		} else if token.Type == "block-scalar-header" {
			blockScalarHeaderToken = token
		} else {
			throwError("Unexpected token type in block value end: %s", token.Type)
		}
	}
	if blockScalarHeaderToken == nil {
		throwError("Expected block scalar header token")
	}
	chomping, indent := parseHeader(unitsToString(blockScalarHeaderToken.Source))
	return context.createBlockValue(
		nodeType,
		context.transformRange(blockValue.Range[0], blockValue.Range[1]),
		context.transformContentProperties(blockValue, properties),
		chomping,
		indent,
		blockValue.Source,
		indicatorComment,
	)
}

// headerPattern is /([+-]?)(\d*)([+-]?)$/u. Go's regexp finds the leftmost match and prefers what a
// backtracking engine prefers, as JavaScript's does, and \d is ASCII in both.
var headerPattern = regexp.MustCompile(`([+-]?)(\d*)([+-]?)$`)

// parseHeader is upstream's parseHeader: the chomping and the explicit indentation, nil for null.
func parseHeader(header string) (chomping string, indent *int) {
	parsed := headerPattern.FindStringSubmatch(header)
	chomping = "clip"
	if parsed != nil {
		if parsed[2] != "" {
			// Number(parsed[2]). The lexer reads one digit of indentation indicator, so this fits.
			// Ignored: the pattern matches only ASCII digits and the lexer gives at most one, so Atoi has
			// nothing it can fail on.
			value, _ := strconv.Atoi(parsed[2])
			indent = &value
		}
		// `parsed[3] || parsed[1]`
		chompingString := parsed[3]
		if chompingString == "" {
			chompingString = parsed[1]
		}
		switch chompingString {
		case "+":
			chomping = "keep"
		case "-":
			chomping = "strip"
		default:
			chomping = "clip"
		}
	}
	return chomping, indent
}

// transformPlain is upstream's transformPlain.
func (context *context) transformPlain(plain *compose.Node, properties []*cst.Token) *Node {
	if plain.Range[0] == plain.Range[1] {
		index := findLastCharIndex(context.text, plain.Range[0]-1, isNotWhitespace) + 1
		return context.createPlain(
			context.transformRange(index, index),
			context.transformContentProperties(plain, properties),
			"",
		)
	}
	sourceToken := plain.SrcToken
	if sourceToken == nil || sourceToken.Type != "scalar" {
		throwError("Expected plain scalar srcToken")
	}
	for _, token := range context.extractComments(sourceToken.End) {
		throwError("Unexpected token type in plain scalar end: %s", token.Type)
	}
	return context.createPlain(context.transformRange(plain.Range[0], plain.Range[1]),
		context.transformContentProperties(plain, properties), plain.Source)
}

// transformQuoteValue is transformQuoteDouble and transformQuoteSingle with transformAstQuoteValue.
func (context *context) transformQuoteValue(nodeType string, quoteValue *compose.Node, properties []*cst.Token) *Node {
	sourceToken := quoteValue.SrcToken
	expected := "single-quoted-scalar"
	message := "Expected single-quoted scalar srcToken"
	if nodeType == "quoteDouble" {
		expected = "double-quoted-scalar"
		message = "Expected double-quoted scalar srcToken"
	}
	if sourceToken == nil || sourceToken.Type != expected {
		throwError("%s", message)
	}
	for _, token := range context.extractComments(sourceToken.End) {
		throwError("Unexpected token type in quote value end: %s", token.Type)
	}
	return context.createQuoteValue(nodeType, context.transformRange(quoteValue.Range[0], quoteValue.Range[1]),
		context.transformContentProperties(quoteValue, properties), quoteValue.Source)
}

// transformContentProperties is upstream's transformContentProperties (content.mjs): the node's tag and
// anchor from their tokens, and the comments between the first of them and the node.
//
// node is null for the null key of a !!pairs item; upstream then fails reading node.tag, node.anchor or
// node.range, as here.
func (context *context) transformContentProperties(node *compose.Node, tokens []*cst.Token) content {
	middleComments := []*Node{}
	var firstTagOrAnchorRange []int
	var tag *Node
	var anchor *Node
	for _, token := range tokens {
		tokenRange := []int{token.Offset, token.Offset + len(token.Source)}
		switch token.Type {
		case "tag":
			// firstTagOrAnchorRange ??= tokenRange
			if firstTagOrAnchorRange == nil {
				firstTagOrAnchorRange = tokenRange
			}
			if node == nil {
				throwTypeError("null", "tag")
			}
			// `node.tag ?? token.source.slice(token.source.startsWith("!!") ? 2 : 1)`: the composer
			// never sets an empty tag, so "" is its undefined.
			resolvedTag := node.Tag
			if resolvedTag == "" {
				start := 1
				if len(token.Source) >= 2 && token.Source[0] == '!' && token.Source[1] == '!' {
					start = 2
				}
				resolvedTag = unitsToString(token.Source[min(start, len(token.Source)):])
			}
			if resolvedTag == "!" {
				resolvedTag = "tag:yaml.org,2002:str"
			}
			tag = context.createTag(context.transformRange(tokenRange[0], tokenRange[1]), resolvedTag)
		case "anchor":
			if firstTagOrAnchorRange == nil {
				firstTagOrAnchorRange = tokenRange
			}
			if node == nil {
				throwTypeError("null", "anchor")
			}
			// node.anchor: undefined, which the JSON leaves out, where the composer set none.
			anchor = context.createAnchor(context.transformRange(tokenRange[0], tokenRange[1]), node.Anchor)
		case "comment":
			comment := context.transformComment(token)
			if firstTagOrAnchorRange != nil && firstTagOrAnchorRange[0] <= tokenRange[0] {
				if node == nil {
					throwTypeError("null", "range")
				}
				if node.Range == nil {
					throwTypeError("undefined", "0")
				}
				if tokenRange[1] <= node.Range[0] {
					middleComments = append(middleComments, comment)
				}
			}
		default:
			throwError("Unexpected content property token type: %s", token.Type)
		}
	}
	return createContent(tag, anchor, middleComments)
}

// directiveSeparator is /[\t ]+/.
var directiveSeparator = regexp.MustCompile(`[\t ]+`)

// transformDirective is upstream's transformDirective (directive.mjs).
func (context *context) transformDirective(directive *cst.Token) *Node {
	parts := directiveSeparator.Split(
		strings.TrimFunc(unitsToString(directive.Source), ecmascripttext.IsWhitespace),
		-1,
	)
	// parts.shift().replace(/^%/, "")
	name := strings.TrimPrefix(parts[0], "%")
	return context.createDirective(context.transformRange(directive.Offset, directive.Offset+len(directive.Source)),
		name, parts[1:])
}

// isNotWhitespace is /\S/u tested on one code unit. A lone surrogate is not whitespace.
func isNotWhitespace(unit int) bool {
	return !ecmascripttext.IsWhitespace(rune(unit))
}

// findCharIndex is utils/find-char-index.mjs; -1 is its null.
func findCharIndex(text []uint16, from int, test func(unit int) bool) int {
	for index := max(from, 0); index < len(text); index++ {
		if test(int(text[index])) {
			return index
		}
	}
	return -1
}

// findLastCharIndex is utils/find-last-char-index.mjs. Past the end of the text, text[i] is undefined,
// and /\S/ tests the string "undefined", which matches.
func findLastCharIndex(text []uint16, from int, test func(unit int) bool) int {
	for index := from; index >= 0; index-- {
		if index >= len(text) {
			return index
		}
		if test(int(text[index])) {
			return index
		}
	}
	return -1
}
