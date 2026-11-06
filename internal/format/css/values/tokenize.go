// Port of postcss-values-parser 2.0.1 lib/tokenize.js.
//
// The tokenizer walks UTF-16 code units, as upstream's charCodeAt and slice do, so every column and
// every index it reports is in upstream's unit. The parser converts indexes to bytes when it builds
// nodes; columns stay in UTF-16 units.

package values

import "math"

const (
	openBracket  = '{'
	closeBracket = '}'
	openParen    = '('
	closeParen   = ')'
	singleQuote  = '\''
	doubleQuote  = '"'
	backslash    = '\\'
	slash        = '/'
	period       = '.'
	comma        = ','
	colon        = ':'
	asterisk     = '*'
	minus        = '-'
	plus         = '+'
	pound        = '#'
	newline      = '\n'
	space        = ' '
	feed         = '\f'
	tab          = '\t'
	cr           = '\r'
	at           = '@'
	lowerE       = 'e'
	upperE       = 'E'
	digit0       = '0'
	digit9       = '9'
	lowerU       = 'u'
	upperU       = 'U'
)

// undefinedNext is upstream's `next` before anything assigns it. The bracket and paren tokens report
// `next - offset` as their end column without assigning `next` first, so the first token of a value
// that opens with one of them ends at column NaN, and later ones end where the previous token did.
const undefinedNext = math.MinInt

// atEnd is upstream's /[ \n\t\r\{\(\)'"\\;,/]/g.
func atEnd(code uint16) bool {
	switch code {
	case ' ', '\n', '\t', '\r', '{', '(', ')', '\'', '"', '\\', ';', ',', '/':
		return true
	}
	return false
}

// wordEnd is upstream's /[ \n\t\r\(\)\{\}\*:;@!&'"\+\|~>,\[\]\\]|\/(?=\*)/g at one index.
func wordEnd(css []uint16, index int) bool {
	switch css[index] {
	case ' ', '\n', '\t', '\r', '(', ')', '{', '}', '*', ':', ';', '@', '!', '&', '\'', '"', '+', '|', '~', '>', ',',
		'[', ']', '\\':
		return true
	case '/':
		return index+1 < len(css) && css[index+1] == '*'
	}
	return false
}

// endsNumberWord is upstream's /[ \n\t\r\(\)\{\}\*:;@!&'"\-\+\|~>,\[\]\\]|\//g at one index.
func endsNumberWord(css []uint16, index int) bool {
	switch css[index] {
	case ' ', '\n', '\t', '\r', '(', ')', '{', '}', '*', ':', ';', '@', '!', '&', '\'', '"', '-', '+', '|', '~', '>',
		',', '[', ']', '\\', '/':
		return true
	}
	return false
}

// alphanumeric is upstream's /^[a-z0-9]/i. JavaScript's case-insensitive class without the u flag folds
// only within ASCII here, so this is a plain ASCII test.
func alphanumeric(code uint16) bool {
	return (code >= 'a' && code <= 'z') || (code >= 'A' && code <= 'Z') || (code >= '0' && code <= '9')
}

// unicodeRange is upstream's /^[a-f0-9?\-]/i.
func unicodeRange(code uint16) bool {
	return (code >= 'a' && code <= 'f') ||
		(code >= 'A' && code <= 'F') ||
		(code >= '0' && code <= '9') ||
		code == '?' ||
		code == '-'
}

// regexLastIndex is `regex.lastIndex = from; regex.test(css); regex.lastIndex` for the one-character
// global regexes above: one past the match, or 0 when there is none.
func regexLastIndex(css []uint16, from int, matches func(css []uint16, index int) bool) int {
	for index := from; index < len(css); index++ {
		if matches(css, index) {
			return index + 1
		}
	}
	return 0
}

// indexOfUnits is css.indexOf(search, from).
func indexOfUnits(css []uint16, search []uint16, from int) int {
	for index := max(from, 0); index+len(search) <= len(css); index++ {
		found := true
		for offset := range search {
			if css[index+offset] != search[offset] {
				found = false
				break
			}
		}
		if found {
			return index
		}
	}
	return -1
}

func tokenize(input []uint16, options Options) []token {
	tokens := []token{}
	css := input
	length := len(css)
	offset := -1
	line := 1
	position := 0
	parentCount := 0
	isUrlArgument := false

	var code int
	next := undefinedNext
	var quote uint16
	var escaped bool
	var escapePosition int

	// charCodeAt is css.charCodeAt: -1 stands in for NaN past either end, equal to no code.
	characterCodeAt := func(index int) int {
		if index < 0 || index >= length {
			return -1
		}
		return int(css[index])
	}

	// slice is css.slice for the non-negative bounds this file passes, clamped the way slice clamps.
	slice := func(start int, end int) []uint16 {
		clampedStart := min(max(start, 0), length)
		clampedEnd := max(min(max(end, 0), length), clampedStart)
		return css[clampedStart:clampedEnd]
	}

	push := func(kind string, value []uint16, startLine int, startColumn int, endLine int, endColumn int, index int) {
		tokens = append(tokens, token{
			kind:        kind,
			value:       value,
			startLine:   startLine,
			startColumn: startColumn,
			endLine:     endLine,
			endColumn:   endColumn,
			index:       index,
		})
	}

	// pushStale pushes a bracket or paren token, whose end column is `next - offset` with a stale `next`.
	pushStale := func(kind string) {
		each := token{
			kind:        kind,
			value:       slice(position, position+1),
			startLine:   line,
			startColumn: position - offset,
			endLine:     line,
			index:       position,
		}
		if next == undefinedNext {
			each.endColumnIsNaN = true
		} else {
			each.endColumn = next - offset
		}
		tokens = append(tokens, each)
	}

	unclosed := func(what string) {
		// Invariant: this is upstream's throw of a TokenizeError, which an unclosed quote or comment
		// reaches; Parse recovers every *Error and returns it as a syntax error, so the panic never
		// leaves the package.
		panic(&Error{Name: "TokenizeError", Message: "Unclosed " +
			what +
			" at line: " +
			itoa(line) +
			", column: " +
			itoa(position-offset) +
			", token: " +
			itoa(position)})
	}

	for position < length {
		code = characterCodeAt(position)

		if code == newline {
			offset = position
			line += 1
		}

		switch code {
		case newline, space, tab, cr, feed:
			next = position
			for {
				next += 1
				code = characterCodeAt(next)
				if code == newline {
					offset = next
					line += 1
				}
				if !(code == space || code == newline || code == tab || code == cr || code == feed) {
					break
				}
			}

			push("space", slice(position, next), line, position-offset, line, next-offset, position)

			position = next - 1

		case colon:
			next = position + 1
			push("colon", slice(position, next), line, position-offset, line, next-offset, position)

			position = next - 1

		case comma:
			next = position + 1
			push("comma", slice(position, next), line, position-offset, line, next-offset, position)

			position = next - 1

		case openBracket:
			pushStale("{")

		case closeBracket:
			pushStale("}")

		case openParen:
			parentCount++
			isUrlArgument = !isUrlArgument && parentCount == 1 &&
				len(tokens) > 0 &&
				tokens[len(tokens)-1].kind == "word" &&
				string(utf16Decode(tokens[len(tokens)-1].value)) == "url"
			pushStale("(")

		case closeParen:
			parentCount--
			isUrlArgument = isUrlArgument && parentCount > 0
			pushStale(")")

		case singleQuote, doubleQuote:
			if code == singleQuote {
				quote = '\''
			} else {
				quote = '"'
			}
			next = position
			for {
				escaped = false
				next = indexOfUnits(css, []uint16{quote}, next+1)
				if next == -1 {
					unclosed("quote")
				}
				escapePosition = next
				for characterCodeAt(escapePosition-1) == backslash {
					escapePosition -= 1
					escaped = !escaped
				}
				if !escaped {
					break
				}
			}

			push("string", slice(position, next+1), line, position-offset, line, next-offset, position)
			position = next

		case at:
			lastIndex := regexLastIndex(
				css,
				position+1,
				func(css []uint16, index int) bool { return atEnd(css[index]) },
			)

			if lastIndex == 0 {
				next = length - 1
			} else {
				next = lastIndex - 2
			}

			push("atword", slice(position, next+1), line, position-offset, line, next-offset, position)
			position = next

		case backslash:
			next = position

			// Upstream reads the next character's code here and tests an `escape` variable that nothing
			// assigns, so the backslash is always a one-character word and the branch that would take
			// the escaped character, the only reader of that code, is dead.

			push("word", slice(position, next+1), line, position-offset, line, next-offset, position)

			position = next

		case plus, minus, asterisk:
			next = position + 1
			nextCharacter := characterCodeAt(position + 1)

			// Upstream also computes a prevChar here that it never reads.

			// if the operator is immediately followed by a word character, then we
			// have a prefix of some kind, and should fall-through. eg. -webkit

			// look for --* for custom variables
			if code == minus && nextCharacter == minus {
				next++

				push("word", slice(position, next), line, position-offset, line, next-offset, position)

				position = next - 1
				break
			}

			push("operator", slice(position, next), line, position-offset, line, next-offset, position)

			position = next - 1

		default:
			if code == slash && (characterCodeAt(position+1) == asterisk ||
				(options.Loose && !isUrlArgument && characterCodeAt(position+1) == slash)) {
				isStandardComment := characterCodeAt(position+1) == asterisk

				if isStandardComment {
					next = indexOfUnits(css, []uint16{'*', '/'}, position+2) + 1
					if next == 0 {
						unclosed("comment")
					}
				} else {
					newlinePosition := indexOfUnits(css, []uint16{'\n'}, position+2)

					if newlinePosition != -1 {
						next = newlinePosition - 1
					} else {
						next = length
					}
				}

				content := slice(position, next+1)
				// lines = content.split('\n'), of which only the count and the last line's length are read.
				last := 0
				lastLineLength := len(content)
				for index, unit := range content {
					if unit == '\n' {
						last++
						lastLineLength = len(content) - index - 1
					}
				}

				var nextLine int
				var nextOffset int
				if last > 0 {
					nextLine = line + last
					nextOffset = next - lastLineLength
				} else {
					nextLine = line
					nextOffset = offset
				}

				push("comment", content, line, position-offset, nextLine, next-nextOffset, position)

				offset = nextOffset
				line = nextLine
				position = next

			} else if code == pound && !(position+1 < length && alphanumeric(css[position+1])) {
				next = position + 1

				push("#", slice(position, next), line, position-offset, line, next-offset, position)

				position = next - 1
			} else if (code == lowerU || code == upperU) && characterCodeAt(position+1) == plus {
				next = position + 2

				// Upstream also reads each code here, which the loop test and everything after never read.
				for {
					next += 1
					if !(next < length && unicodeRange(css[next])) {
						break
					}
				}

				push("unicoderange", slice(position, next), line, position-offset, line, next-offset, position)
				position = next - 1
			} else if code == slash {
				// catch a regular slash, that isn't a comment
				next = position + 1

				push("operator", slice(position, next), line, position-offset, line, next-offset, position)

				position = next - 1
			} else {
				regex := wordEnd
				isWordEndNumber := false

				// we're dealing with a word that starts with a number
				// those get treated differently
				if code >= digit0 && code <= digit9 {
					regex = endsNumberWord
					isWordEndNumber = true
				}

				lastIndex := regexLastIndex(css, position+1, regex)

				if lastIndex == 0 {
					next = length - 1
				} else {
					next = lastIndex - 2
				}

				// Exponential number notation with minus or plus: 1e-10, 1e+10
				if isWordEndNumber || code == period {
					ncode := characterCodeAt(next)
					ncode1 := characterCodeAt(next + 1)
					ncode2 := characterCodeAt(next + 2)

					if (ncode == lowerE || ncode == upperE) &&
						(ncode1 == minus || ncode1 == plus) &&
						(ncode2 >= digit0 && ncode2 <= digit9) {
						lastIndex := regexLastIndex(css, next+2, endsNumberWord)

						if lastIndex == 0 {
							next = length - 1
						} else {
							next = lastIndex - 2
						}
					}
				}

				push("word", slice(position, next+1), line, position-offset, line, next-offset, position)
				position = next
			}
		}

		position++
	}

	return tokens
}
