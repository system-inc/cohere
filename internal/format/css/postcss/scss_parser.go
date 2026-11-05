package postcss

import (
	"strings"

	ecmascripttext "github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// postcss-scss 4.0.9, lib/scss-parser.js: `class ScssParser extends Parser`, overriding atrule, comment,
// createTokenizer, raw and rule.
//
// Go has no inheritance: scssParser embeds the *parser and is installed as its `this` (parser.go's
// parserMethods), so the base parser's calls to those five methods reach these, and `super.method()` is
// s.parser.method(). Everything else ScssParser calls on `this` (init, spacesAndCommentsFromStart,
// stringFrom, checkMissedSemicolon, ...) is the base parser's, reached through the embedding.

type scssParser struct {
	*parser
}

// newScssParser is new ScssParser(input): Parser's constructor with this ScssParser.
func newScssParser(in *input) *parser {
	s := &scssParser{parser: &parser{}}
	s.this = s
	s.construct(in)
	return s.parser
}

// TypeError is the JavaScript TypeError postcss-scss throws where its rule reads tokens[0] of an empty
// array (a nested property with no word in it, `:"x" {}`). Prettier only turns errors with a line into its
// own and lets this one escape, so the file does not format; ParseSCSS marks it with printing.Syntax.
type TypeError struct {
	// Message is the error's message as V8 words it.
	Message string
}

func (typeError *TypeError) Error() string {
	return "TypeError: " + typeError.Message
}

func (parser *scssParser) atrule(atToken token) {
	name := atToken.value
	previous := atToken
	for !parser.tokenizer.endOfFile() {
		next, _ := parser.tokenizer.nextToken()
		if next.kind == "word" && next.start == previous.end+1 {
			name += next.value
			previous = next
		} else {
			parser.tokenizer.back(next)
			break
		}
	}

	parser.parser.atrule(token{kind: "at-word", value: name, start: atToken.start, end: previous.end})
}

func (parser *scssParser) comment(commentToken token) {
	if commentToken.inline {
		node := newComment()
		parser.init(node, commentToken.start)
		node.raws["inline"] = rawFlag(true)
		lineAndColumn := parser.input.fromOffset(commentToken.end)
		node.source.end = &position{
			column: lineAndColumn.column,
			line:   lineAndColumn.line,
			offset: commentToken.end + 1,
		}

		text := commentToken.value[2:]
		if strings.TrimFunc(text, ecmascripttext.IsWhitespace) == "" {
			node.text = ""
			node.raws["left"] = rawText(text)
			node.raws["right"] = rawText("")
		} else {
			// text.match(/^(\s*)([^]*\S)(\s*)$/), as Parser.comment reads it.
			trimmedStart := strings.TrimLeftFunc(text, ecmascripttext.IsWhitespace)
			trimmed := strings.TrimRightFunc(trimmedStart, ecmascripttext.IsWhitespace)
			fixed := replaceCommentDelimiters(trimmed)
			node.text = fixed
			node.raws["left"] = rawText(text[:len(text)-len(trimmedStart)])
			node.raws["right"] = rawText(trimmedStart[len(trimmed):])
			node.raws["text"] = rawText(trimmed)
		}
		node.mark("text")
	} else {
		parser.parser.comment(commentToken)
	}
}

func (parser *scssParser) createTokenizer() {
	parser.tokenizer = newScssTokenizer(parser.input)
}

func (parser *scssParser) raw(each *node, property string, tokens []token, customProperty bool) {
	parser.parser.raw(each, property, tokens, customProperty)
	if raws, isRaw := each.raws[property].(*rawValue); isRaw {
		scss := raws.raw
		var all strings.Builder
		for _, token := range tokens {
			if token.kind == "comment" && token.inline {
				text := replaceCommentDelimiters(token.value[2:])
				all.WriteString("/*" + text + "*/")
			} else {
				all.WriteString(token.value)
			}
		}
		raws.raw = all.String()
		if scss != raws.raw {
			raws.scss = scss
			raws.hasScss = true
		}
	}
}

func (parser *scssParser) rule(tokens []token) {
	withColon := false
	brackets := 0
	value := ""
	for _, token := range tokens {
		if withColon {
			if token.kind != "comment" && token.kind != "{" {
				value += token.value
			}
		} else if token.kind == "space" && strings.Contains(token.value, "\n") {
			break
		} else if token.kind == "(" {
			brackets += 1
		} else if token.kind == ")" {
			brackets -= 1
		} else if brackets == 0 && token.kind == ":" {
			withColon = true
		}
	}

	if !withColon || strings.TrimFunc(value, ecmascripttext.IsWhitespace) == "" || startsLikeAProperty(value) {
		parser.parser.rule(tokens)
	} else {
		// remaining is the tokens not yet read, starting without the "{" that opens the block.
		remaining := tokens[:len(tokens)-1]
		node := newNestedDeclaration()
		parser.init(node, remaining[0].start)

		// The last token that is not a space; there is one, the colon.
		var last token
		for index := len(remaining) - 1; index >= 0; index-- {
			if remaining[index].kind != "space" {
				last = remaining[index]
				break
			}
		}
		// `if (last[3])`: an end of 0, or none, falls to the start.
		if last.end > 0 {
			lineAndColumn := parser.input.fromOffset(last.end)
			node.source.end = &position{
				column: lineAndColumn.column,
				line:   lineAndColumn.line,
				offset: last.end + 1,
			}
		} else {
			lineAndColumn := parser.input.fromOffset(last.start)
			node.source.end = &position{
				column: lineAndColumn.column,
				line:   lineAndColumn.line,
				offset: last.start + 1,
			}
		}

		for {
			if len(remaining) == 0 {
				// tokens[0][0] on an empty array.
				// Invariant: this is upstream's TypeError, which a nested property with no word reaches; Parse
				// recovers every *TypeError and returns it as a syntax error, so the panic never leaves the
				// package.
				panic(&TypeError{Message: "Cannot read properties of undefined (reading '0')"})
			}
			if remaining[0].kind == "word" {
				break
			}
			node.raws["before"] = rawText(node.rawString("before") + remaining[0].value)
			remaining = remaining[1:]
		}

		// `if (tokens[0][2])`: a start of 0 keeps the one init set, which is the same offset.
		if remaining[0].start > 0 {
			lineAndColumn := parser.input.fromOffset(remaining[0].start)
			node.source.start = &position{
				column: lineAndColumn.column,
				line:   lineAndColumn.line,
				offset: remaining[0].start,
			}
		}

		node.prop = ""
		node.mark("prop")
		for len(remaining) > 0 {
			kind := remaining[0].kind
			if kind == ":" || kind == "space" || kind == "comment" {
				break
			}
			node.prop += remaining[0].value
			remaining = remaining[1:]
		}

		node.raws["between"] = rawText("")

		var each token
		for len(remaining) > 0 {
			each = remaining[0]
			remaining = remaining[1:]

			if each.kind == ":" {
				node.raws["between"] = rawText(node.rawString("between") + each.value)
				break
			} else {
				node.raws["between"] = rawText(node.rawString("between") + each.value)
			}
		}

		if node.prop != "" && (node.prop[0] == '_' || node.prop[0] == '*') {
			node.raws["before"] = rawText(node.rawString("before") + node.prop[:1])
			node.prop = node.prop[1:]
		}
		node.raws["between"] = rawText(node.rawString("between") + parser.spacesAndCommentsFromStart(&remaining))
		// precheckMissedSemicolon is a hook for postcss-safe-parser; the base parser's does nothing.

		for i := len(remaining) - 1; i > 0; i-- {
			each = remaining[i]
			if each.value == "!important" {
				node.important = true
				node.mark("important")
				text := parser.stringFrom(&remaining, i)
				text = parser.spacesFromEnd(&remaining) + text
				if text != " !important" {
					node.raws["important"] = rawText(text)
				}
				break
			} else if each.value == "important" {
				cache := copyTokens(remaining, 0, len(remaining))
				text := ""
				for j := i; j > 0; j-- {
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
					node.raws["important"] = rawText(text)
					remaining = cache
				}
			}

			if each.kind != "space" && each.kind != "comment" {
				break
			}
		}

		parser.raw(node, "value", remaining, false)

		if strings.Contains(node.value, ":") {
			parser.checkMissedSemicolon(remaining)
		}

		parser.current = node
	}
}

// startsLikeAProperty is /^[#:A-Za-z-]/.test(value).
func startsLikeAProperty(value string) bool {
	if value == "" {
		return false
	}
	first := value[0]
	return first == '#' ||
		first == ':' ||
		first == '-' ||
		(first >= 'A' && first <= 'Z') ||
		(first >= 'a' && first <= 'z')
}

// replaceCommentDelimiters is text.replace(/(\*\/|\/\*)/g, '*//*'): every "*/" and "/*", found left to
// right without overlap, becomes "*//*".
func replaceCommentDelimiters(text string) string {
	var result strings.Builder
	for index := 0; index < len(text); index++ {
		if index+1 < len(text) &&
			((text[index] == '*' && text[index+1] == '/') || (text[index] == '/' && text[index+1] == '*')) {
			result.WriteString("*//*")
			index++
			continue
		}
		result.WriteByte(text[index])
	}
	return result.String()
}
