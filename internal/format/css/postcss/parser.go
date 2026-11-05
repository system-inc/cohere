package postcss

import (
	"strings"

	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// postcss 8.5.16, lib/parser.js.
//
// Upstream throws CssSyntaxError from deep inside the parser; here the parser panics with the
// *CssSyntaxError and Parse recovers it into the error it returns, so no panic leaves Parse.
//
// Token arrays are Go slices. The helpers upstream calls to pop, shift or splice an array the caller
// keeps using take a pointer to the slice; everywhere upstream copies (slice, concat) the port copies too,
// so no two slices share elements a later write could reach.

// safeCommentNeighbor is SAFE_COMMENT_NEIGHBOR.
func safeCommentNeighbor(kind string) bool {
	return kind == "empty" || kind == "space"
}

// findLastWithPosition is the last token's `token[3] || token[2]` that is truthy, -1 for undefined.
func findLastWithPosition(tokens []token) int {
	for index := len(tokens) - 1; index >= 0; index-- {
		each := tokens[index]
		position := each.endOrStart()
		if position > 0 {
			return position
		}
	}
	return -1
}

func tokensToString(tokens []token, from int, to int) string {
	var result strings.Builder
	for index := from; index < to; index++ {
		result.WriteString(tokens[index].value)
	}
	return result.String()
}

// copyTokens is tokens.slice(from, to): a new array.
func copyTokens(tokens []token, from int, to int) []token {
	result := make([]token, to-from)
	copy(result, tokens[from:to])
	return result
}

// parserMethods are the methods of Parser that postcss-scss's ScssParser overrides (scss_parser.go).
// Go has no inheritance, so where upstream calls one of them on `this` the port calls it through
// parser.this, which is the parser itself for css and the scssParser around it for scss; a subclass's
// `super.method()` is a call on the embedded *parser. Every other method is called on the parser directly,
// as no subclass the port carries replaces it.
type parserMethods interface {
	atrule(atToken token)
	comment(commentToken token)
	createTokenizer()
	raw(each *node, property string, tokens []token, customProperty bool)
	rule(tokens []token)
}

type parser struct {
	input *input

	// this is upstream's `this` for the overridable methods.
	this parserMethods

	root      *node
	current   *node
	spaces    string
	semicolon bool

	tokenizer tokenStream
}

func newParser(in *input) *parser {
	p := &parser{}
	p.this = p
	p.construct(in)
	return p
}

// construct is Parser's constructor, split from newParser so a subclass sets this first and its own
// createTokenizer is the one the constructor calls.
func (parser *parser) construct(in *input) {
	parser.input = in

	parser.root = newRoot()
	parser.current = parser.root
	parser.spaces = ""
	parser.semicolon = false

	parser.this.createTokenizer()
	parser.root.source = &source{start: &position{column: 1, line: 1, offset: 0}}
	parser.root.mark("source")
}

func (parser *parser) atrule(atToken token) {
	node := newAtRule()
	node.name = atToken.value[1:]
	node.mark("name")
	if node.name == "" {
		parser.unnamedAtrule(node, atToken)
	}
	parser.init(node, atToken.start)

	var kind string
	var previous *token
	var shift int
	last := false
	open := false
	parameters := []token{}
	brackets := []string{}

	for !parser.tokenizer.endOfFile() {
		each, _ := parser.tokenizer.nextToken()
		kind = each.kind

		if kind == "(" || kind == "[" {
			if kind == "(" {
				brackets = append(brackets, ")")
			} else {
				brackets = append(brackets, "]")
			}
		} else if kind == "{" && len(brackets) > 0 {
			brackets = append(brackets, "}")
		} else if len(brackets) > 0 && kind == brackets[len(brackets)-1] {
			brackets = brackets[:len(brackets)-1]
		}

		if len(brackets) == 0 {
			if kind == ";" {
				node.source.end = parser.getPosition(each.start)
				node.source.end.offset++
				parser.semicolon = true
				break
			} else if kind == "{" {
				open = true
				break
			} else if kind == "}" {
				if len(parameters) > 0 {
					shift = len(parameters) - 1
					previous = &parameters[shift]
					for previous != nil && previous.kind == "space" {
						shift--
						if shift >= 0 {
							previous = &parameters[shift]
						} else {
							previous = nil
						}
					}
					if previous != nil {
						node.source.end = parser.getPosition(previous.endOrStart())
						node.source.end.offset++
					}
				}
				parser.end(each)
				break
			} else {
				parameters = append(parameters, each)
			}
		} else {
			parameters = append(parameters, each)
		}

		if parser.tokenizer.endOfFile() {
			last = true
			break
		}
	}

	node.raws["between"] = parser.spacesAndCommentsFromEnd(&parameters)
	if len(parameters) > 0 {
		node.raws["afterName"] = parser.spacesAndCommentsFromStart(&parameters)
		parser.this.raw(node, "params", parameters, false)
		if last {
			each := parameters[len(parameters)-1]
			node.source.end = parser.getPosition(each.endOrStart())
			node.source.end.offset++
			parser.spaces = node.rawString("between")
			node.raws["between"] = ""
		}
	} else {
		node.raws["afterName"] = ""
		node.params = ""
		node.mark("params")
	}

	if open {
		node.nodes = emptyNodes()
		node.mark("nodes")
		parser.current = node
	}
}

func (parser *parser) checkMissedSemicolon(tokens []token) {
	colon := parser.colon(tokens)
	if colon < 0 {
		return
	}

	founded := 0
	var each token
	for j := colon - 1; j >= 0; j-- {
		each = tokens[j]
		if each.kind != "space" {
			founded += 1
			if founded == 2 {
				break
			}
		}
	}
	// If the token is a word, e.g. `!important`, `red` or any other valid
	// property's value. Then we need to return the colon after that word
	// token. [3] is the "end" colon of that word. And because we need it
	// after that one we do +1 to get the next one.
	offset := each.start
	if each.kind == "word" {
		offset = each.end + 1
	}
	panic(parser.input.errorAt("Missed semicolon", offset))
}

// colon is the index of the first top-level ":", -1 where upstream returns false.
func (parser *parser) colon(tokens []token) int {
	brackets := 0
	var previous *token
	for index := range tokens {
		each := &tokens[index]
		kind := each.kind

		if kind == "(" {
			brackets += 1
		}
		if kind == ")" {
			brackets -= 1
		}
		if brackets == 0 && kind == ":" {
			if previous == nil {
				parser.doubleColon(*each)
			} else if previous.kind == "word" && previous.value == "progid" {
				continue
			} else {
				return index
			}
		}

		previous = each
	}
	return -1
}

func (parser *parser) comment(commentToken token) {
	node := newComment()
	parser.init(node, commentToken.start)
	node.source.end = parser.getPosition(commentToken.endOrStart())
	node.source.end.offset++

	text := commentToken.value[2 : len(commentToken.value)-2]
	if strings.TrimFunc(text, ecmascripttext.IsWhitespace) == "" {
		node.text = ""
		node.raws["left"] = text
		node.raws["right"] = ""
	} else {
		// text.match(/^(\s*)([^]*\S)(\s*)$/): the leading whitespace, the rest up to the last
		// non-whitespace character, and the trailing whitespace.
		trimmedStart := strings.TrimLeftFunc(text, ecmascripttext.IsWhitespace)
		trimmed := strings.TrimRightFunc(trimmedStart, ecmascripttext.IsWhitespace)
		node.text = trimmed
		node.raws["left"] = text[:len(text)-len(trimmedStart)]
		node.raws["right"] = trimmedStart[len(trimmed):]
	}
	node.mark("text")
}

func (parser *parser) createTokenizer() {
	parser.tokenizer = newTokenizer(parser.input)
}

func (parser *parser) decl(tokens []token, customProperty bool) {
	node := newDeclaration()
	parser.init(node, tokens[0].start)

	last := tokens[len(tokens)-1]
	if last.kind == ";" {
		parser.semicolon = true
		tokens = tokens[:len(tokens)-1]
	}

	endOffset := last.endOrStart()
	if endOffset <= 0 {
		endOffset = findLastWithPosition(tokens)
	}
	node.source.end = parser.getPosition(endOffset)
	node.source.end.offset++

	start := 0
	for tokens[start].kind != "word" {
		if start == len(tokens)-1 {
			parser.unknownWord([]token{tokens[start]})
		}
		start++
	}
	node.raws["before"] = node.rawString("before") + tokensToString(tokens, 0, start)
	node.source.start = parser.getPosition(tokens[start].start)

	propertyStart := start
	for start < len(tokens) {
		kind := tokens[start].kind
		if kind == ":" || kind == "space" || kind == "comment" {
			break
		}
		start++
	}
	node.prop = tokensToString(tokens, propertyStart, start)
	node.mark("prop")

	betweenStart := start
	var each token
	for start < len(tokens) {
		each = tokens[start]
		start++
		if each.kind == ":" {
			break
		}
		if each.kind == "word" && hasWordCharacter(each.value) {
			parser.unknownWord([]token{each})
		}
	}
	node.raws["between"] = tokensToString(tokens, betweenStart, start)

	if node.prop[0] == '_' || node.prop[0] == '*' {
		node.raws["before"] = node.rawString("before") + node.prop[:1]
		node.prop = node.prop[1:]
	}

	firstSpacesStart := start
	for start < len(tokens) {
		next := tokens[start].kind
		if next != "space" && next != "comment" {
			break
		}
		start++
	}
	firstSpaces := copyTokens(tokens, firstSpacesStart, start)

	tokens = copyTokens(tokens, start, len(tokens))

	// precheckMissedSemicolon is a hook for postcss-safe-parser; the css parser's does nothing.

	for index := len(tokens) - 1; index >= 0; index-- {
		each = tokens[index]
		lowered := asciiLowerCase(each.value)
		if lowered == "!important" {
			node.important = true
			node.mark("important")
			text := parser.stringFrom(&tokens, index)
			text = parser.spacesFromEnd(&tokens) + text
			if text != " !important" {
				node.raws["important"] = text
			}
			break
		} else if lowered == "important" {
			cache := copyTokens(tokens, 0, len(tokens))
			text := ""
			for j := index; j > 0; j-- {
				kind := cache[j].kind
				if strings.HasPrefix(strings.TrimFunc(text, ecmascripttext.IsWhitespace), "!") && kind != "space" {
					break
				}
				text = cache[len(cache)-1].value + text
				cache = cache[:len(cache)-1]
			}
			if strings.HasPrefix(strings.TrimFunc(text, ecmascripttext.IsWhitespace), "!") {
				node.important = true
				node.mark("important")
				node.raws["important"] = text
				tokens = cache
			}
		}

		if each.kind != "space" && each.kind != "comment" {
			break
		}
	}

	hasWord := false
	for _, candidate := range tokens {
		if candidate.kind != "space" && candidate.kind != "comment" {
			hasWord = true
			break
		}
	}

	if hasWord {
		node.raws["between"] = node.rawString("between") + tokensToString(firstSpaces, 0, len(firstSpaces))
		firstSpaces = []token{}
	}
	valueTokens := make([]token, 0, len(firstSpaces)+len(tokens))
	valueTokens = append(valueTokens, firstSpaces...)
	valueTokens = append(valueTokens, tokens...)
	parser.this.raw(node, "value", valueTokens, customProperty)

	if strings.Contains(node.value, ":") && !customProperty {
		parser.checkMissedSemicolon(tokens)
	}
}

func (parser *parser) doubleColon(each token) {
	panic(parser.input.errorBetween("Double colon", each.start, each.start+utf16Length(each.value)))
}

func (parser *parser) emptyRule(each token) {
	node := newRule()
	parser.init(node, each.start)
	node.selector = ""
	node.mark("selector")
	node.raws["between"] = ""
	parser.current = node
}

func (parser *parser) end(each token) {
	if parser.current.hasNodes() && len(parser.current.nodes) > 0 {
		parser.current.raws["semicolon"] = parser.semicolon
	}
	parser.semicolon = false

	parser.current.raws["after"] = parser.current.rawString("after") + parser.spaces
	parser.spaces = ""

	if parser.current.parent != nil {
		parser.current.source.end = parser.getPosition(each.start)
		parser.current.source.end.offset++
		parser.current = parser.current.parent
	} else {
		parser.unexpectedClose(each)
	}
}

func (parser *parser) endFile() {
	if parser.current.parent != nil {
		parser.unclosedBlock()
	}
	if parser.current.hasNodes() && len(parser.current.nodes) > 0 {
		parser.current.raws["semicolon"] = parser.semicolon
	}
	parser.current.raws["after"] = parser.current.rawString("after") + parser.spaces
	parser.root.source.end = parser.getPosition(parser.tokenizer.position())
}

func (parser *parser) freeSemicolon(each token) {
	parser.spaces += each.value
	if parser.current.hasNodes() {
		var previous *node
		if len(parser.current.nodes) > 0 {
			previous = parser.current.nodes[len(parser.current.nodes)-1]
		}
		if previous != nil && previous.nodeType == "rule" && previous.rawString("ownSemicolon") == "" {
			previous.raws["ownSemicolon"] = parser.spaces
			parser.spaces = ""
			previous.source.end = parser.getPosition(each.start)
			previous.source.end.offset += utf16Length(previous.rawString("ownSemicolon"))
		}
	}
}

// Helpers

func (parser *parser) getPosition(offset int) *position {
	lineAndColumn := parser.input.fromOffset(offset)
	return &position{
		column: lineAndColumn.column,
		line:   lineAndColumn.line,
		offset: offset,
	}
}

func (parser *parser) init(each *node, offset int) {
	parser.current.push(each)
	each.source = &source{start: parser.getPosition(offset)}
	each.mark("source")
	each.raws["before"] = parser.spaces
	parser.spaces = ""
	if each.nodeType != "comment" {
		parser.semicolon = false
	}
}

func (parser *parser) other(start token) {
	end := false
	kind := ""
	colon := false
	var bracket *token
	brackets := []string{}
	customProperty := strings.HasPrefix(start.value, "--")

	tokens := []token{}
	each := start
	present := true
	for present {
		kind = each.kind
		tokens = append(tokens, each)

		if kind == "(" || kind == "[" {
			if bracket == nil {
				opened := each
				bracket = &opened
			}
			if kind == "(" {
				brackets = append(brackets, ")")
			} else {
				brackets = append(brackets, "]")
			}
		} else if customProperty && colon && kind == "{" {
			if bracket == nil {
				opened := each
				bracket = &opened
			}
			brackets = append(brackets, "}")
		} else if len(brackets) == 0 {
			if kind == ";" {
				if colon {
					parser.decl(tokens, customProperty)
					return
				} else {
					break
				}
			} else if kind == "{" {
				parser.this.rule(tokens)
				return
			} else if kind == "}" {
				parser.tokenizer.back(tokens[len(tokens)-1])
				tokens = tokens[:len(tokens)-1]
				end = true
				break
			} else if kind == ":" {
				colon = true
			}
		} else if kind == brackets[len(brackets)-1] {
			brackets = brackets[:len(brackets)-1]
			if len(brackets) == 0 {
				bracket = nil
			}
		}

		each, present = parser.tokenizer.nextToken()
	}

	if parser.tokenizer.endOfFile() {
		end = true
	}
	if len(brackets) > 0 {
		parser.unclosedBracket(*bracket)
	}

	if end && colon {
		if !customProperty {
			for len(tokens) > 0 {
				kind = tokens[len(tokens)-1].kind
				if kind != "space" && kind != "comment" {
					break
				}
				parser.tokenizer.back(tokens[len(tokens)-1])
				tokens = tokens[:len(tokens)-1]
			}
		}
		parser.decl(tokens, customProperty)
	} else {
		parser.unknownWord(tokens)
	}
}

func (parser *parser) parse() {
	for !parser.tokenizer.endOfFile() {
		each, _ := parser.tokenizer.nextToken()

		switch each.kind {
		case "space":
			parser.spaces += each.value

		case ";":
			parser.freeSemicolon(each)

		case "}":
			parser.end(each)

		case "comment":
			parser.this.comment(each)

		case "at-word":
			parser.this.atrule(each)

		case "{":
			parser.emptyRule(each)

		default:
			parser.other(each)
		}
	}
	parser.endFile()
}

func (parser *parser) raw(each *node, property string, tokens []token, customProperty bool) {
	length := len(tokens)
	var value strings.Builder
	clean := true

	for index := 0; index < length; index++ {
		current := tokens[index]
		kind := current.kind
		if kind == "space" && index == length-1 && !customProperty {
			clean = false
		} else if kind == "comment" {
			previous := "empty"
			if index-1 >= 0 {
				previous = tokens[index-1].kind
			}
			next := "empty"
			if index+1 < length {
				next = tokens[index+1].kind
			}
			if !safeCommentNeighbor(previous) && !safeCommentNeighbor(next) {
				if strings.HasSuffix(value.String(), ",") {
					clean = false
				} else {
					value.WriteString(current.value)
				}
			} else {
				clean = false
			}
		} else {
			value.WriteString(current.value)
		}
	}
	if !clean {
		raw := tokensToString(tokens, 0, length)
		each.raws[property] = &rawValue{raw: raw, value: value.String()}
	}
	each.setField(property, value.String())
}

func (parser *parser) rule(tokens []token) {
	tokens = tokens[:len(tokens)-1]

	node := newRule()
	parser.init(node, tokens[0].start)

	node.raws["between"] = parser.spacesAndCommentsFromEnd(&tokens)
	parser.this.raw(node, "selector", tokens, false)
	parser.current = node
}

func (parser *parser) spacesAndCommentsFromEnd(tokens *[]token) string {
	spaces := ""
	for len(*tokens) > 0 {
		lastTokenType := (*tokens)[len(*tokens)-1].kind
		if lastTokenType != "space" && lastTokenType != "comment" {
			break
		}
		spaces = (*tokens)[len(*tokens)-1].value + spaces
		*tokens = (*tokens)[:len(*tokens)-1]
	}
	return spaces
}

// Errors

func (parser *parser) spacesAndCommentsFromStart(tokens *[]token) string {
	spaces := ""
	for len(*tokens) > 0 {
		next := (*tokens)[0].kind
		if next != "space" && next != "comment" {
			break
		}
		spaces += (*tokens)[0].value
		*tokens = (*tokens)[1:]
	}
	return spaces
}

func (parser *parser) spacesFromEnd(tokens *[]token) string {
	spaces := ""
	for len(*tokens) > 0 {
		lastTokenType := (*tokens)[len(*tokens)-1].kind
		if lastTokenType != "space" {
			break
		}
		spaces = (*tokens)[len(*tokens)-1].value + spaces
		*tokens = (*tokens)[:len(*tokens)-1]
	}
	return spaces
}

func (parser *parser) stringFrom(tokens *[]token, from int) string {
	result := tokensToString(*tokens, from, len(*tokens))
	*tokens = (*tokens)[:from]
	return result
}

func (parser *parser) unclosedBlock() {
	position := parser.current.source.start
	panic(parser.input.errorAtLineAndColumn("Unclosed block", position.line, position.column))
}

func (parser *parser) unclosedBracket(bracket token) {
	panic(parser.input.errorBetween("Unclosed bracket", bracket.start, bracket.start+1))
}

func (parser *parser) unexpectedClose(each token) {
	panic(parser.input.errorBetween("Unexpected }", each.start, each.start+1))
}

func (parser *parser) unknownWord(tokens []token) {
	panic(parser.input.errorBetween(
		"Unknown word "+tokens[0].value,
		tokens[0].start,
		tokens[0].start+utf16Length(tokens[0].value),
	))
}

func (parser *parser) unnamedAtrule(_ *node, each token) {
	panic(parser.input.errorBetween("At-rule without name", each.start, each.start+utf16Length(each.value)))
}

// hasWordCharacter is /\w/.test(text): an ASCII letter, digit or underscore.
func hasWordCharacter(text string) bool {
	for index := 0; index < len(text); index++ {
		character := text[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' {
			return true
		}
	}
	return false
}

// asciiLowerCase stands in for toLowerCase where the result is only compared with "!important" and
// "important": no character outside ASCII lowercases to one of those strings' letters in JavaScript
// (U+0130 becomes "i" plus a combining dot, two units), so lowering ASCII alone decides the same.
// Go's strings.ToLower would not: it maps U+0130 to a plain "i".
func asciiLowerCase(text string) string {
	lowered := []byte(text)
	for index, character := range lowered {
		if character >= 'A' && character <= 'Z' {
			lowered[index] = character + 'a' - 'A'
		}
	}
	return string(lowered)
}

// utf16Length is a string's JavaScript length: its UTF-16 units, a byte that is not UTF-8 counting one.
func utf16Length(text string) int {
	length := 0
	for _, character := range text {
		if character > 0xFFFF {
			length += 2
		} else {
			length++
		}
	}
	return length
}
