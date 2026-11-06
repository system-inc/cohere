package estree

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/printing"
)

// ParseJson is Prettier's json parsers, src/language-json/parse/json.js: Babel's parseExpression over
// the text, checked by assertJsonNode, wrapped in a JsonRoot. allowComments false is json-stringify,
// which refuses a comment.
//
// Babel parses any JavaScript expression and assertJsonNode then refuses everything that is not JSON
// (with JSON5's identifiers and signs). This parses only that subset directly, and refuses whatever
// else it meets, which is the same set of accepted inputs: any construct beyond the subset would
// have been refused by assertJsonNode after Babel parsed it.
//
// The nodes are Babel's shapes, because that is what Prettier prints for JSON: ObjectProperty,
// StringLiteral, NumericLiteral, BooleanLiteral, NullLiteral. Babel keeps a literal's source text in
// extra.raw; here it is the raw property, which is where the printer's getRaw reads it for every
// tree. Comments are Line and Block nodes, the typescript-estree spelling, where Babel says
// CommentLine and CommentBlock; the printer treats the two spellings identically everywhere.
//
// allowEmpty (jsonc) is not supported: no parser our files resolve to sets it.
//
// Every refusal is marked printing.Syntax, so the format phase skips a file that is not JSON rather
// than reporting a broken formatter.
func ParseJson(text string, allowComments bool) (*Node, []*Node, error) {
	root, comments, parseJsonError := parseJson(text, allowComments)
	return root, comments, printing.Syntax(parseJsonError)
}

func parseJson(text string, allowComments bool) (*Node, []*Node, error) {
	reader := &jsonReader{text: text}
	reader.skipTrivia()
	if reader.position >= len(text) {
		return nil, nil, fmt.Errorf("json: empty input")
	}
	expression, parseValueError := reader.parseValue()
	if parseValueError != nil {
		return nil, nil, parseValueError
	}
	reader.skipTrivia()
	if reader.err != nil {
		return nil, nil, reader.err
	}
	if reader.position < len(text) {
		return nil, nil, reader.errorf("unexpected %q", reader.text[reader.position:min(reader.position+10, len(text))])
	}
	if !allowComments && len(reader.comments) > 0 {
		return nil, nil, fmt.Errorf("json: Comment is not allowed in JSON")
	}
	root := New("JsonRoot", 0, len(text), []Property{{"node", expression}})
	return root, reader.comments, nil
}

// unexpectedNumericSeparator is Babel's refusal of a separator out of place, Errors.UnexpectedNumericSeparator.
const unexpectedNumericSeparator = "A numeric separator is only allowed between two digits."

// separatorsBetweenDigits is Babel's check on each numeric separator in text[start:end], a literal's digits
// in radix (its tokenizer's readInt): a separator stands only where the next character is a digit of the
// radix, and neither neighbour is a prefix letter, a dot, an exponent or another separator. The neighbours
// are read from the whole text, so the prefix before a radix literal's first digit counts (#w6vtsrn).
func separatorsBetweenDigits(text string, start int, end int, radix int) bool {
	forbidden := ".BEO_beo"
	if radix == 16 {
		forbidden = ".X_x"
	}
	for index := start; index < end; index++ {
		if text[index] != '_' {
			continue
		}
		if index+1 >= len(text) ||
			digitValue(text[index+1]) >= radix ||
			strings.IndexByte(forbidden, text[index+1]) >= 0 {
			return false
		}
		if index > 0 && strings.IndexByte(forbidden, text[index-1]) >= 0 {
			return false
		}
	}
	return true
}

func isRangeError(parseError error) bool {
	numberError, isNumberError := parseError.(*strconv.NumError)
	return isNumberError && numberError.Err == strconv.ErrRange
}

func digitValue(character byte) int {
	switch {
	case character >= '0' && character <= '9':
		return int(character - '0')
	case character >= 'a' && character <= 'f':
		return int(character-'a') + 10
	case character >= 'A' && character <= 'F':
		return int(character-'A') + 10
	}
	return math.MaxInt
}

func isIdentifierPart(text string, position int) bool {
	character, _ := utf8.DecodeRuneInString(text[position:])
	return character == '$' || character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

// decodeJavaScriptString is a string literal's value from its raw content: JavaScript's escapes.
func decodeJavaScriptString(content string) (string, error) {
	if !strings.Contains(content, "\\") {
		return content, nil
	}
	var builder strings.Builder
	for index := 0; index < len(content); index++ {
		character := content[index]
		if character != '\\' {
			builder.WriteByte(character)
			continue
		}
		index++
		if index >= len(content) {
			return "", fmt.Errorf("a string ends in a backslash")
		}
		switch escaped := content[index]; escaped {
		case 'n':
			builder.WriteByte('\n')
		case 't':
			builder.WriteByte('\t')
		case 'r':
			builder.WriteByte('\r')
		case 'b':
			builder.WriteByte('\b')
		case 'f':
			builder.WriteByte('\f')
		case 'v':
			builder.WriteByte('\v')
		case '0':
			if index+1 < len(content) && content[index+1] >= '0' && content[index+1] <= '9' {
				return "", fmt.Errorf("an octal escape in a string")
			}
			builder.WriteByte(0)
		case '\r':
			// A line continuation, \r\n counting as one.
			if index+1 < len(content) && content[index+1] == '\n' {
				index++
			}
		case '\n':
		case 'x':
			if index+2 >= len(content) {
				return "", fmt.Errorf("an incomplete \\x escape")
			}
			code, parseError := strconv.ParseUint(content[index+1:index+3], 16, 8)
			if parseError != nil {
				return "", fmt.Errorf("an invalid \\x escape")
			}
			builder.WriteRune(rune(code))
			index += 2
		case 'u':
			code, length, readUnicodeEscapeError := readUnicodeEscape(content[index+1:])
			if readUnicodeEscapeError != nil {
				return "", readUnicodeEscapeError
			}
			index += length
			// A surrogate pair spelled as two escapes is one character.
			if code >= 0xD800 && code <= 0xDBFF && strings.HasPrefix(content[index+1:], "\\u") {
				if low, lowLength, readUnicodeEscapeError := readUnicodeEscape(
					content[index+3:],
				); readUnicodeEscapeError == nil && low >= 0xDC00 && low <= 0xDFFF {
					code = (code-0xD800)<<10 + (low - 0xDC00) + 0x10000
					index += 2 + lowLength
				}
			}
			builder.WriteRune(rune(code))
		default:
			if escaped >= '1' && escaped <= '9' {
				return "", fmt.Errorf("an octal escape in a string")
			}
			// Any other character escapes to itself, including a multi-byte one.
			_, size := utf8.DecodeRuneInString(content[index:])
			if strings.HasPrefix(content[index:], "\u2028") || strings.HasPrefix(content[index:], "\u2029") {
				index += size - 1
				continue
			}
			builder.WriteString(content[index : index+size])
			index += size - 1
		}
	}
	return builder.String(), nil
}

// readUnicodeEscape reads the hex after `\u`: four digits or a braced code point. It returns the code
// and how many bytes it consumed.
func readUnicodeEscape(text string) (int, int, error) {
	if strings.HasPrefix(text, "{") {
		closing := strings.IndexByte(text, '}')
		if closing < 0 {
			return 0, 0, fmt.Errorf("an incomplete \\u{} escape")
		}
		code, parseError := strconv.ParseUint(text[1:closing], 16, 32)
		if parseError != nil || code > 0x10FFFF {
			return 0, 0, fmt.Errorf("an invalid \\u{} escape")
		}
		return int(code), closing + 1, nil
	}
	if len(text) < 4 {
		return 0, 0, fmt.Errorf("an incomplete \\u escape")
	}
	code, parseError := strconv.ParseUint(text[:4], 16, 16)
	if parseError != nil {
		return 0, 0, fmt.Errorf("an invalid \\u escape")
	}
	return int(code), 4, nil
}
