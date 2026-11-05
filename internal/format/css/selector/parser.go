package selector

// postcss-selector-parser 2.2.3, dist/parser.js, with the flatten, indexes-of, uniq and sortAscending
// helpers splitWord leans on.
//
// # The lossless option
//
// Prettier's processor passes no options, so input.options.lossless is undefined and this.lossy is
// false: parseNamespace, parseSpace, parseValue and parseParenthesisToken return their argument
// untouched, and the lossy branches (a trimmed tokenizer input, trimmed attribute values) never run.
// The helpers are kept so the call sites read like upstream; their lossy halves are not ported.
//
// # Throwing
//
// Upstream throws, and so does this port: throwError panics with a thrownError that Parse recovers into
// a returned error. Upstream also throws where it did not mean to, a TypeError from reading a property
// of undefined. Prettier catches those the same as the parser's own errors, so the port throws them
// too, with V8's message, at the line where upstream would.

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/estree"
)

// thrownError is what a throw in upstream becomes: input.error's new Error(message), or a TypeError.
type thrownError struct{ message string }

func throwError(message string) {
	// Invariant: every throw happens under Parse, whose deferred recover turns a thrownError into the
	// returned error, so this panic never leaves the package.
	panic(thrownError{message})
}

// throwTypeError is V8's TypeError for reading key on undefined.
func throwTypeError(key string) {
	throwError("Cannot read properties of undefined (reading '" + key + "')")
}

// loopsForever is thrown where upstream never returns; see ErrLoopsForever.
type loopsForever struct{}

type parser struct {
	position int
	root     *estree.Node
	current  *estree.Node
	tokens   []*token
	spaces   string

	// parents is each node's parent, which upstream keeps on the node and the port's tree leaves off.
	parents map[*estree.Node]*estree.Node
}

func newParser(css []uint16) *parser {
	parser := &parser{parents: map[*estree.Node]*estree.Node{}}
	parser.position = 0
	parser.root = newSelectorNode(typeRoot)

	selectors := newSelectorNode(typeSelector)
	parser.append(parser.root, selectors)

	parser.current = selectors
	parser.tokens = tokenize(css)

	return parser
}

// append is container.append(selector).
func (parser *parser) append(container *estree.Node, selector *estree.Node) *estree.Node {
	parser.parents[selector] = container
	container.Set("nodes", append(container.List("nodes"), selector))
	return container
}

// attributeOperator is /((?:[*~^$|]?=))([^]*)/ as attribute() splits on it.
var attributeOperator = regexp.MustCompile(`([*~^$|]?=)([\s\S]*)`)

func (parser *parser) attribute() {
	text := ""
	startingToken := parser.currToken()
	parser.position++
	for parser.position < len(parser.tokens) && parser.currToken().kind != "]" {
		text += parser.tokens[parser.position].value
		parser.position++
	}
	if parser.position == len(parser.tokens) && !strings.Contains(text, "]") {
		parser.error("Expected a closing square bracket.")
	}
	parts := splitAttribute(text)
	namespace := splitNamespace(parts[0])
	if parser.currToken() == nil {
		// end: {line: this.currToken[2], ...} when the brackets closed inside a string token.
		throwTypeError("2")
	}
	// operator: parts[1], value: parts[2], undefined both when the text has no operator, where
	// splitAttribute returns [text] alone.
	attributeProperties := []any{"operator", nil, "value", nil}
	if len(parts) > 2 {
		attributeProperties = []any{"operator", parts[1], "value", parts[2]}
	}
	attributeProperties = append(attributeProperties,
		"source", source(startingToken.at(2), startingToken.at(3), parser.currToken().at(2), parser.currToken().at(3)),
		"sourceIndex", startingToken.at(4),
	)
	if len(namespace) > 1 {
		attributeProperties = append(attributeProperties, "attribute", parser.parseValue(namespace[2]))
		// namespace[0] || true: a bar with nothing before it is namespace true.
		prefix := parser.parseNamespace(namespacePrefix{text: namespace[0], bare: namespace[0] == ""})
		if prefix.bare {
			attributeProperties = append(attributeProperties, "namespace", true)
		} else {
			attributeProperties = append(attributeProperties, "namespace", prefix.text)
		}
	} else {
		attributeProperties = append(attributeProperties, "attribute", parser.parseValue(parts[0]))
	}
	attr := newSelectorNode(typeAttribute, attributeProperties...)

	if len(parts) > 2 && parts[2] != "" {
		insensitive := splitInsensitive(parts[2])
		trimmedValue := jsTrim(insensitive[0])
		attr.Set("value", insensitive[0])
		if len(insensitive) > 1 && insensitive[1] != "" {
			attr.Set("insensitive", true)
			setRaw(attr, "insensitive", insensitive[1])
		}
		attr.Set("quoted", strings.HasPrefix(trimmedValue, "'") || strings.HasPrefix(trimmedValue, "\""))
		if attr.Bool("quoted") {
			setRaw(attr, "unquoted", jsSliceOneFromEach(trimmedValue))
		} else {
			setRaw(attr, "unquoted", trimmedValue)
		}
	}
	parser.newNode(attr, namespacePrefix{})
	parser.position++
}

func (parser *parser) combinator() {
	if parser.currToken().value == "|" {
		parser.namespace()
		return
	}
	node := newSelectorNode(
		typeCombinator,
		"value",
		"",
		"source",
		source(parser.currToken().at(2), parser.currToken().at(3), parser.currToken().at(2), parser.currToken().at(3)),
		"sourceIndex",
		parser.currToken().at(4),
	)
	for parser.position < len(parser.tokens) &&
		parser.currToken() != nil &&
		(parser.currToken().kind == "space" || parser.currToken().kind == "combinator") {
		if parser.nextToken() != nil && parser.nextToken().kind == "combinator" {
			setSpace(node, "before", parser.parseSpace(parser.currToken().value, ""))
			setSourceEdge(node, "start", "line", parser.nextToken().at(2))
			setSourceEdge(node, "start", "column", parser.nextToken().at(3))
			setSourceEdge(node, "end", "column", parser.nextToken().at(3))
			setSourceEdge(node, "end", "line", parser.nextToken().at(2))
			node.Set("sourceIndex", parser.nextToken().at(4))
		} else if parser.prevToken() != nil && parser.prevToken().kind == "combinator" {
			setSpace(node, "after", parser.parseSpace(parser.currToken().value, ""))
		} else if parser.currToken().kind == "combinator" {
			node.Set("value", parser.currToken().value)
		} else if parser.currToken().kind == "space" {
			node.Set("value", parser.parseSpace(parser.currToken().value, " "))
		}
		parser.position++
	}
	parser.newNode(node, namespacePrefix{})
}

func (parser *parser) comma() {
	if parser.position == len(parser.tokens)-1 {
		parser.root.Set("trailingComma", true)
		parser.position++
		return
	}
	selectors := newSelectorNode(typeSelector)
	parser.append(parser.parents[parser.current], selectors)
	parser.current = selectors
	parser.position++
}

func (parser *parser) comment() {
	node := newSelectorNode(
		typeComment,
		"value",
		parser.currToken().value,
		"source",
		source(parser.currToken().at(2), parser.currToken().at(3), parser.currToken().at(4), parser.currToken().at(5)),
		"sourceIndex",
		parser.currToken().at(6),
	)
	parser.newNode(node, namespacePrefix{})
	parser.position++
}

func (parser *parser) error(message string) {
	throwError(message)
}

func (parser *parser) missingBackslash() {
	parser.error("Expected a backslash preceding the semicolon.")
}

func (parser *parser) missingParenthesis() {
	parser.error("Expected opening parenthesis.")
}

func (parser *parser) missingSquareBracket() {
	parser.error("Expected opening square bracket.")
}

func (parser *parser) namespace() {
	// this.prevToken && this.prevToken[1] || true: a token's value is never empty.
	before := namespacePrefix{bare: true}
	if parser.prevToken() != nil && parser.prevToken().value != "" {
		before = namespacePrefix{text: parser.prevToken().value}
	}
	if parser.nextToken() == nil {
		throwTypeError("0")
	}
	if parser.nextToken().kind == "word" {
		parser.position++
		parser.word(before)
		return
	} else if parser.nextToken().kind == "*" {
		parser.position++
		parser.universal(before)
		return
	}
	// Upstream returns here without consuming the bar, and every caller comes straight back to it.
	// Invariant: Parse's deferred recover turns loopsForever into ErrLoopsForever, so this panic never
	// leaves the package.
	panic(loopsForever{})
}

func (parser *parser) nesting() {
	parser.newNode(newSelectorNode(
		typeNesting,
		"value",
		parser.currToken().value,
		"source",
		source(parser.currToken().at(2), parser.currToken().at(3), parser.currToken().at(2), parser.currToken().at(3)),
		"sourceIndex",
		parser.currToken().at(4),
	), namespacePrefix{})
	parser.position++
}

func (parser *parser) parentheses() {
	last := lastOf(parser.current)
	if last != nil && last.Type() == typePseudo {
		selector := newSelectorNode(typeSelector)
		cache := parser.current
		parser.append(last, selector)
		parser.current = selector
		balanced := 1
		parser.position++
		for parser.position < len(parser.tokens) && balanced != 0 {
			if parser.currToken().kind == "(" {
				balanced++
			}
			if parser.currToken().kind == ")" {
				balanced--
			}
			if balanced != 0 {
				parser.parse(false)
			} else {
				setSourceEdge(parser.parents[selector], "end", "line", parser.currToken().at(2))
				setSourceEdge(parser.parents[selector], "end", "column", parser.currToken().at(3))
				parser.position++
			}
		}
		if balanced != 0 {
			parser.error("Expected closing parenthesis.")
		}
		parser.current = cache
	} else {
		balanced := 1
		parser.position++
		if last == nil {
			throwTypeError("value")
		}
		last.Set("value", jsStringOf(last.Get("value"))+"(")
		for parser.position < len(parser.tokens) && balanced != 0 {
			if parser.currToken().kind == "(" {
				balanced++
			}
			if parser.currToken().kind == ")" {
				balanced--
			}
			last.Set("value", jsStringOf(last.Get("value"))+parser.parseParenthesisToken(parser.currToken()))
			parser.position++
		}
		if balanced != 0 {
			parser.error("Expected closing parenthesis.")
		}
	}
}

func (parser *parser) pseudo() {
	pseudoString := ""
	startingToken := parser.currToken()
	for parser.currToken() != nil && parser.currToken().kind == ":" {
		pseudoString += parser.currToken().value
		parser.position++
	}
	if parser.currToken() == nil {
		parser.error("Expected pseudo-class or pseudo-element")
		return
	}
	if parser.currToken().kind == "word" {
		parser.splitWord(namespacePrefix{}, func(first string, length int) {
			pseudoString += first
			// currToken may be a space token here (see token.loose): its [4] is its pos, and its [5] is
			// undefined.
			endColumn, hasEndColumn := parser.currToken().loose(5)
			pseudoSource := source(startingToken.at(2), startingToken.at(3), parser.currToken().at(4), endColumn)
			pseudoSource.endColumnUndefined = !hasEndColumn
			pseudo := newSelectorNode(typePseudo,
				"value", pseudoString,
				"source", pseudoSource,
				"sourceIndex", startingToken.at(4),
			)
			parser.newNode(pseudo, namespacePrefix{})
			if length > 1 && parser.nextToken() != nil && parser.nextToken().kind == "(" {
				parser.error("Misplaced parenthesis.")
			}
		})
	} else {
		parser.error("Unexpected \"" + parser.currToken().kind + "\" found.")
	}
}

func (parser *parser) space() {
	token := parser.currToken()
	// Handle space before and after the selector
	if parser.position == 0 || parser.prevToken().kind == "," || parser.prevToken().kind == "(" {
		parser.spaces = parser.parseSpace(token.value, "")
		parser.position++
	} else if parser.position == len(parser.tokens)-1 ||
		parser.nextToken().kind == "," ||
		parser.nextToken().kind == ")" {
		last := lastOf(parser.current)
		if last == nil {
			throwTypeError("spaces")
		}
		setSpace(last, "after", parser.parseSpace(token.value, ""))
		parser.position++
	} else {
		parser.combinator()
	}
}

func (parser *parser) string() {
	token := parser.currToken()
	parser.newNode(newSelectorNode(typeString,
		"value", parser.currToken().value,
		"source", source(token.at(2), token.at(3), token.at(4), token.at(5)),
		"sourceIndex", token.at(6),
	), namespacePrefix{})
	parser.position++
}

func (parser *parser) universal(namespace namespacePrefix) {
	nextToken := parser.nextToken()
	if nextToken != nil && nextToken.value == "|" {
		parser.position++
		parser.namespace()
		return
	}
	parser.newNode(newSelectorNode(
		typeUniversal,
		"value",
		parser.currToken().value,
		"source",
		source(parser.currToken().at(2), parser.currToken().at(3), parser.currToken().at(2), parser.currToken().at(3)),
		"sourceIndex",
		parser.currToken().at(4),
	), namespace)
	parser.position++
}

func (parser *parser) splitWord(namespace namespacePrefix, firstCallback func(first string, length int)) {
	nextToken := parser.nextToken()
	word := parser.currToken().value
	for nextToken != nil && nextToken.kind == "word" {
		parser.position++
		current := parser.currToken().value
		word += current
		if strings.LastIndex(current, "\\") == len(current)-1 {
			next := parser.nextToken()
			if next != nil && next.kind == "space" {
				word += parser.parseSpace(next.value, " ")
				parser.position++
			}
		}
		nextToken = parser.nextToken()
	}
	// The indexes below are UTF-16, as the column and sourceIndex arithmetic needs.
	units := wtf8ToUnits(word)
	hasClass := indexesOf(units, ".")
	hasID := indexesOf(units, "#")
	// Eliminate Sass interpolations from the list of id indexes
	interpolations := indexesOf(units, "#{")
	if len(interpolations) > 0 {
		filtered := []int{}
		for _, hashIndex := range hasID {
			if !containsInt(interpolations, hashIndex) {
				filtered = append(filtered, hashIndex)
			}
		}
		hasID = filtered
	}
	indices := sortAscending(uniq(flatten([]int{0}, hasClass, hasID)))
	for indexIndex, ind := range indices {
		index := len(units)
		if indexIndex+1 < len(indices) && indices[indexIndex+1] != 0 {
			index = indices[indexIndex+1]
		}
		value := units[ind:index]
		if indexIndex == 0 && firstCallback != nil {
			firstCallback(unitsToWTF8(value), len(indices))
			continue
		}
		// currToken may be a space token here (see token.loose), which has a [2], [3] and [4] (its pos) and
		// no [6]: undefined plus a number is NaN, which upstream stores as the sourceIndex like any number.
		currToken := parser.currToken()
		nodeSource := source(currToken.at(2), currToken.at(3)+ind, currToken.at(4), currToken.at(3)+index-1)
		position, hasPosition := currToken.loose(6)
		var node *estree.Node
		if containsInt(hasClass, ind) {
			node = newSelectorNode(typeClass,
				"value", unitsToWTF8(value[1:]),
				"source", nodeSource,
				"sourceIndex", position+indices[indexIndex],
			)
		} else if containsInt(hasID, ind) {
			node = newSelectorNode(typeID,
				"value", unitsToWTF8(value[1:]),
				"source", nodeSource,
				"sourceIndex", position+indices[indexIndex],
			)
		} else {
			node = newSelectorNode(typeTag,
				"value", unitsToWTF8(value),
				"source", nodeSource,
				"sourceIndex", position+indices[indexIndex],
			)
		}
		if !hasPosition {
			node.Set("sourceIndex", math.NaN())
		}
		parser.newNode(node, namespace)
	}
	parser.position++
}

func (parser *parser) word(namespace namespacePrefix) {
	nextToken := parser.nextToken()
	if nextToken != nil && nextToken.value == "|" {
		parser.position++
		parser.namespace()
		return
	}
	parser.splitWord(namespace, nil)
}

func (parser *parser) loop() *estree.Node {
	for parser.position < len(parser.tokens) {
		parser.parse(true)
	}
	return parser.root
}

func (parser *parser) parse(throwOnParenthesis bool) {
	switch parser.currToken().kind {
	case "space":
		parser.space()
	case "comment":
		parser.comment()
	case "(":
		parser.parentheses()
	case ")":
		if throwOnParenthesis {
			parser.missingParenthesis()
		}
	case "[":
		parser.attribute()
	case "]":
		parser.missingSquareBracket()
	case "at-word", "word":
		parser.word(namespacePrefix{})
	case ":":
		parser.pseudo()
	case ";":
		parser.missingBackslash()
	case ",":
		parser.comma()
	case "*":
		parser.universal(namespacePrefix{})
	case "&":
		parser.nesting()
	case "combinator":
		parser.combinator()
	case "string":
		parser.string()
	}
}

/**
 * Helpers
 */

// namespacePrefix is the namespace a word, universal or attribute carries, in upstream's
// `this.prevToken && this.prevToken[1] || true`: the text before the bar, or bare (namespace true) for a
// bar with nothing before it. The zero value is no namespace, undefined upstream.
type namespacePrefix struct {
	text string
	bare bool
}

// present is upstream's `if (namespace)`: true and a non-empty text are truthy, and a namespace that is
// not bare always has text.
func (namespace namespacePrefix) present() bool {
	return namespace.bare || namespace.text != ""
}

// parseNamespace returns namespace: its lossy trim never runs (see the file comment).
func (parser *parser) parseNamespace(namespace namespacePrefix) namespacePrefix {
	return namespace
}

// parseSpace returns space: its lossy replacement never runs.
func (parser *parser) parseSpace(space string, replacement string) string {
	return space
}

// parseValue returns value: its lossy trim never runs.
func (parser *parser) parseValue(value string) string {
	return value
}

// parseParenthesisToken returns token[1]: Prettier's parser is lossless.
func (parser *parser) parseParenthesisToken(token *token) string {
	return token.value
}

func (parser *parser) newNode(node *estree.Node, namespace namespacePrefix) *estree.Node {
	if namespace.present() {
		prefix := parser.parseNamespace(namespace)
		if prefix.bare {
			node.Set("namespace", true)
		} else {
			node.Set("namespace", prefix.text)
		}
	}
	if parser.spaces != "" {
		setSpace(node, "before", parser.spaces)
		parser.spaces = ""
	}
	return parser.append(parser.current, node)
}

// currToken, nextToken and prevToken are upstream's getters, nil past either end of the tokens.
func (parser *parser) currToken() *token { return parser.tokenAt(parser.position) }
func (parser *parser) nextToken() *token { return parser.tokenAt(parser.position + 1) }
func (parser *parser) prevToken() *token { return parser.tokenAt(parser.position - 1) }

func (parser *parser) tokenAt(index int) *token {
	if index < 0 || index >= len(parser.tokens) {
		return nil
	}
	return parser.tokens[index]
}

// splitAttribute is str.split(/((?:[*~^$|]?=))([^]*)/): [str] without an operator, else the text
// before it, the operator, the rest, and the empty text after the rest.
func splitAttribute(text string) []string {
	match := attributeOperator.FindStringSubmatchIndex(text)
	if match == nil {
		return []string{text}
	}
	return []string{text[:match[0]], text[match[2]:match[3]], text[match[4]:match[5]], text[match[1]:]}
}

// splitNamespace is str.split(/(\|)/g): the pieces between bars with the bars kept between them.
func splitNamespace(text string) []string {
	pieces := strings.Split(text, "|")
	result := []string{pieces[0]}
	for _, piece := range pieces[1:] {
		result = append(result, "|", piece)
	}
	return result
}

// splitInsensitive is str.split(/(\s+i\s*?)$/): [str] unless str ends in whitespace, an i, and
// optional whitespace, else the text before that whitespace, the rest, and "". The leftmost match
// starts at the first of the whitespace run before the final i. \s is JavaScript's, Unicode's. It walks
// back by character over the WTF-8 text, where a surrogate's bytes decode as no whitespace.
func splitInsensitive(text string) []string {
	end := len(text)
	for end > 0 {
		character, size := utf8.DecodeLastRuneInString(text[:end])
		if !jsIsWhitespace(character) {
			break
		}
		end -= size
	}
	if end == 0 || text[end-1] != 'i' {
		return []string{text}
	}
	start := end - 1
	for start > 0 {
		character, size := utf8.DecodeLastRuneInString(text[:start])
		if !jsIsWhitespace(character) {
			break
		}
		start -= size
	}
	if start == end-1 {
		return []string{text}
	}
	return []string{text[:start], text[start:], ""}
}

// jsIsWhitespace is JavaScript's \s and String.prototype.trim's set: WhiteSpace and LineTerminator.
func jsIsWhitespace(character rune) bool {
	switch character {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return character >= 0x2000 && character <= 0x200A
}

// jsTrim is String.prototype.trim.
func jsTrim(text string) string {
	return strings.TrimFunc(text, jsIsWhitespace)
}

// jsSliceOneFromEach is str.slice(1, -1), in UTF-16 units, which can cut a non-BMP last character in
// half (a lone surrogate, kept as WTF-8 until wellFormed).
func jsSliceOneFromEach(text string) string {
	units := wtf8ToUnits(text)
	if len(units) < 2 {
		return ""
	}
	return unitsToWTF8(units[1 : len(units)-1])
}

// jsStringOf is the string a value becomes in `value + '('`: an undefined value reads "undefined".
func jsStringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return "undefined"
	case bool:
		if typed {
			return "true"
		}
		return "false"
	}
	return ""
}

// indexesOf is the indexes-of package on a string: every index of an ASCII item, in UTF-16 units.
func indexesOf(units []uint16, item string) []int {
	itemUnits := wtf8ToUnits(item)
	indexes := []int{}
	for index := 0; index+len(itemUnits) <= len(units); index++ {
		matched := true
		for offset, unit := range itemUnits {
			if units[index+offset] != unit {
				matched = false
				break
			}
		}
		if matched {
			indexes = append(indexes, index)
		}
	}
	return indexes
}

// flatten is the flatten package on [[0], hasClass, hasId].
func flatten(lists ...[]int) []int {
	flat := []int{}
	for _, list := range lists {
		flat = append(flat, list...)
	}
	return flat
}

// uniq is the uniq package without a comparator: it sorts (as strings, which keeps equal numbers
// adjacent) and drops adjacent duplicates. sortAscending then orders the survivors numerically, so the
// string order in between never shows.
func uniq(list []int) []int {
	seen := map[int]bool{}
	unique := []int{}
	for _, value := range list {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

// sortAscending is sortAscending.js.
func sortAscending(list []int) []int {
	sort.Ints(list)
	return list
}

func containsInt(list []int, value int) bool {
	for _, each := range list {
		if each == value {
			return true
		}
	}
	return false
}
