package selector

// postcss-selector-parser 2.2.3, dist/tokenize.js.
//
// # UTF-16 where Go has bytes
//
// The tokenizer walks the selector by UTF-16 code unit with charCodeAt, and every position it records
// (a token's column, its pos, and the sourceIndex the parser derives from them) is a UTF-16 index. So
// this port walks the same units: Parse encodes the selector to []uint16 once, the tokenizer slices
// that, and a token's value is written back as a WTF-8 string, because the backslash case can split a
// surrogate pair (see wtf8.go). Parse converts sourceIndex to a byte offset at the boundary;
// lines and columns stay in upstream's units.

import "unicode/utf16"

const (
	singleQuote  = 39
	doubleQuote  = 34
	backslash    = 92
	slash        = 47
	newline      = 10
	space        = 32
	feed         = 12
	tab          = 9
	cr           = 13
	plus         = 43
	gt           = 62
	tilde        = 126
	pipe         = 124
	comma        = 44
	openBracket  = 40
	closeBracket = 41
	openSq       = 91
	closeSq      = 93
	semicolon    = 59
	asterisk     = 42
	colon        = 58
	ampersand    = 38
	at           = 64
)

// atEnd is /[ \n\t\r\{\(\)'"\\;/]/g.
func atEnd(code int) bool {
	switch code {
	case space, newline, tab, cr, '{', openBracket, closeBracket, singleQuote, doubleQuote, backslash, semicolon, slash:
		return true
	}
	return false
}

// wordEnd is /[ \n\t\r\(\)\*:;@!&'"\+\|~>,\[\]\\]|\/(?=\*)/g, tested at index.
func wordEnd(css []uint16, index int) bool {
	switch characterCodeAt(css, index) {
	case space, newline, tab, cr, openBracket, closeBracket, asterisk, colon, semicolon, at, '!', ampersand,
		singleQuote, doubleQuote, plus, pipe, tilde, gt, comma, openSq, closeSq, backslash:
		return true
	case slash:
		return characterCodeAt(css, index+1) == asterisk
	}
	return false
}

// characterCodeAt is css.charCodeAt(index), with -1 for the NaN past either end.
func characterCodeAt(css []uint16, index int) int {
	if index < 0 || index >= len(css) {
		return -1
	}
	return int(css[index])
}

// slice is css.slice(start, end), as WTF-8 (see wtf8.go): it can cut a surrogate pair after a backslash.
func slice(css []uint16, start int, end int) string {
	clampedEnd := min(end, len(css))
	if start >= clampedEnd {
		return ""
	}
	return unitsToWtf8(css[start:clampedEnd])
}

// indexOf is css.indexOf(search, from).
func indexOf(css []uint16, search string, from int) int {
	units := utf16.Encode([]rune(search))
	for index := max(from, 0); index+len(units) <= len(css); index++ {
		matched := true
		for offset, unit := range units {
			if css[index+offset] != unit {
				matched = false
				break
			}
		}
		if matched {
			return index
		}
	}
	return -1
}

func tokenize(css []uint16) []*token {
	tokens := []*token{}

	var code int
	var next int
	var nextLine int
	var nextOffset int
	var escapePosition int
	var quote string
	var content string
	var escape bool
	var escaped bool

	length := len(css)
	offset := -1
	line := 1
	position := 0

	// Prettier's processor never sets input.safe, so an unclosed quote or comment always throws.
	unclosed := func(what string) {
		throwError("Unclosed " + what)
	}

	for position < length {
		code = characterCodeAt(css, position)

		if code == newline {
			offset = position
			line += 1
		}

		switch code {
		case newline, space, tab, cr, feed:
			next = position
			for {
				next += 1
				code = characterCodeAt(css, next)
				if code == newline {
					offset = next
					line += 1
				}
				if !(code == space || code == newline || code == tab || code == cr || code == feed) {
					break
				}
			}

			tokens = append(
				tokens,
				&token{"space", slice(css, position, next), []int{line, position - offset, position}},
			)
			position = next - 1

		case plus, gt, tilde, pipe:
			next = position
			for {
				next += 1
				code = characterCodeAt(css, next)
				if !(code == plus || code == gt || code == tilde || code == pipe) {
					break
				}
			}
			tokens = append(
				tokens,
				&token{"combinator", slice(css, position, next), []int{line, position - offset, position}},
			)
			position = next - 1

		case asterisk:
			tokens = append(tokens, &token{"*", "*", []int{line, position - offset, position}})

		case ampersand:
			tokens = append(tokens, &token{"&", "&", []int{line, position - offset, position}})

		case comma:
			tokens = append(tokens, &token{",", ",", []int{line, position - offset, position}})

		case openSq:
			tokens = append(tokens, &token{"[", "[", []int{line, position - offset, position}})

		case closeSq:
			tokens = append(tokens, &token{"]", "]", []int{line, position - offset, position}})

		case colon:
			tokens = append(tokens, &token{":", ":", []int{line, position - offset, position}})

		case semicolon:
			tokens = append(tokens, &token{";", ";", []int{line, position - offset, position}})

		case openBracket:
			tokens = append(tokens, &token{"(", "(", []int{line, position - offset, position}})

		case closeBracket:
			tokens = append(tokens, &token{")", ")", []int{line, position - offset, position}})

		case singleQuote, doubleQuote:
			if code == singleQuote {
				quote = "'"
			} else {
				quote = "\""
			}
			next = position
			for {
				escaped = false
				next = indexOf(css, quote, next+1)
				if next == -1 {
					unclosed("quote")
				}
				escapePosition = next
				for characterCodeAt(css, escapePosition-1) == backslash {
					escapePosition -= 1
					escaped = !escaped
				}
				if !escaped {
					break
				}
			}

			tokens = append(tokens, &token{
				"string",
				slice(css, position, next+1),
				[]int{line, position - offset, line, next - offset, position},
			})
			position = next

		case at:
			// atEnd.lastIndex = pos + 1; atEnd.test(css): lastIndex is the match's end, or 0 on a miss.
			lastIndex := 0
			for index := position + 1; index < length; index++ {
				if atEnd(characterCodeAt(css, index)) {
					lastIndex = index + 1
					break
				}
			}
			if lastIndex == 0 {
				next = length - 1
			} else {
				next = lastIndex - 2
			}
			tokens = append(tokens, &token{
				"at-word",
				slice(css, position, next+1),
				[]int{line, position - offset, line, next - offset, position},
			})
			position = next

		case backslash:
			next = position
			escape = true
			for characterCodeAt(css, next+1) == backslash {
				next += 1
				escape = !escape
			}
			code = characterCodeAt(css, next+1)
			if escape &&
				code != slash &&
				code != space &&
				code != newline &&
				code != tab &&
				code != cr &&
				code != feed {
				next += 1
			}
			tokens = append(tokens, &token{
				"word",
				slice(css, position, next+1),
				[]int{line, position - offset, line, next - offset, position},
			})
			position = next

		default:
			if code == slash && characterCodeAt(css, position+1) == asterisk {
				next = indexOf(css, "*/", position+2) + 1
				if next == 0 {
					unclosed("comment")
				}

				content = slice(css, position, next+1)
				// lines = content.split('\n'); last = lines.length - 1; lines[last].length, in UTF-16 units.
				last := 0
				lastLineStart := position
				for index := position; index <= next && index < length; index++ {
					if css[index] == newline {
						last++
						lastLineStart = index + 1
					}
				}
				lastLineLength := next + 1 - lastLineStart

				if last > 0 {
					nextLine = line + last
					nextOffset = next - lastLineLength
				} else {
					nextLine = line
					nextOffset = offset
				}

				tokens = append(
					tokens,
					&token{"comment", content, []int{line, position - offset, nextLine, next - nextOffset, position}},
				)

				offset = nextOffset
				line = nextLine
				position = next
			} else {
				// wordEnd.lastIndex = pos + 1; wordEnd.test(css), as for atEnd above.
				lastIndex := 0
				for index := position + 1; index < length; index++ {
					if wordEnd(css, index) {
						lastIndex = index + 1
						break
					}
				}
				if lastIndex == 0 {
					next = length - 1
				} else {
					next = lastIndex - 2
				}

				tokens = append(tokens, &token{
					"word",
					slice(css, position, next+1),
					[]int{line, position - offset, line, next - offset, position},
				})
				position = next
			}
		}

		position++
	}

	return tokens
}
