package postcss

// postcss 8.5.16, lib/tokenize.js.
//
// The tokenizer reads input.units, the text's UTF-16 code units, so its offsets are unit offsets like
// upstream's. A token's text is sliced from the Go string through input.slice.
//
// Prettier passes no options, so options.ignoreErrors is false; ignoreUnclosed is never passed by the
// css parser either (only postcss-safe-parser and the scss and less parsers pass it), and the branches
// that read them are kept as false.

const (
	singleQuote      = '\''
	doubleQuote      = '"'
	backslash        = '\\'
	slash            = '/'
	newline          = '\n'
	space            = ' '
	feed             = '\f'
	tab              = '\t'
	carriageReturn   = '\r'
	openSquare       = '['
	closeSquare      = ']'
	openParentheses  = '('
	closeParentheses = ')'
	openCurly        = '{'
	closeCurly       = '}'
	semicolon        = ';'
	asterisk         = '*'
	colon            = ':'
	at               = '@'
)

// tokenStream is the object a tokenizer function returns, { back, endOfFile, nextToken, position }: the
// parser holds whichever createTokenizer made, postcss's (tokenizer) or postcss-scss's (scssTokenizer).
type tokenStream interface {
	back(each token)
	endOfFile() bool
	nextToken() (token, bool)
	position() int
}

// isAtEnd is RE_AT_END's class, /[\t\n\f\r "#'()/;[\\\]{}]/.
func isAtEnd(unit uint16) bool {
	switch unit {
	case '\t', '\n', '\f', '\r', ' ', '"', '#', '\'', '(', ')', '/', ';', '[', '\\', ']', '{', '}':
		return true
	}
	return false
}

// isWordEnd is RE_WORD_END's class, /[\t\n\f\r !"#'():;@[\\\]{}]/; the regex's other branch,
// /\/(?=\*)/, is checked by the caller.
func isWordEnd(unit uint16) bool {
	switch unit {
	case '\t', '\n', '\f', '\r', ' ', '!', '"', '#', '\'', '(', ')', ':', ';', '@', '[', '\\', ']', '{', '}':
		return true
	}
	return false
}

// isHexEscape is RE_HEX_ESCAPE, /[\da-f]/i, on one character.
func isHexEscape(code int) bool {
	return (code >= '0' && code <= '9') || (code >= 'a' && code <= 'f') || (code >= 'A' && code <= 'F')
}

// hasBadBracket is RE_BAD_BRACKET.test(content), /.[\r\n"'(/\\]/: some unit that is not a line
// terminator (what `.` will not match) followed by one of the class.
func hasBadBracket(units []uint16) bool {
	for index := 0; index+1 < len(units); index++ {
		switch units[index] {
		case '\n', '\r', 0x2028, 0x2029:
			continue
		}
		switch units[index+1] {
		case '\r', '\n', '"', '\'', '(', '/', '\\':
			return true
		}
	}
	return false
}

type tokenizer struct {
	input *input
	css   []uint16

	length       int
	offset       int
	buffer       []token
	returned     []token
	lastBadParen int
}

func newTokenizer(in *input) *tokenizer {
	return &tokenizer{
		input:        in,
		css:          in.units,
		length:       len(in.units),
		offset:       0,
		buffer:       []token{},
		returned:     []token{},
		lastBadParen: -1,
	}
}

// characterCodeAt is css.charCodeAt(index), -1 standing for NaN past either end.
func (tokens *tokenizer) characterCodeAt(index int) int {
	if index < 0 || index >= tokens.length {
		return -1
	}
	return int(tokens.css[index])
}

// indexOf is css.indexOf(search, from) for an ASCII search string.
func (tokens *tokenizer) indexOf(search string, from int) int {
	for index := max(from, 0); index+len(search) <= tokens.length; index++ {
		matched := true
		for offset := 0; offset < len(search); offset++ {
			if tokens.css[index+offset] != uint16(search[offset]) {
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

func (tokens *tokenizer) position() int {
	return tokens.offset
}

func (tokens *tokenizer) unclosed(what string) {
	// Invariant: this is upstream's throw of a CssSyntaxError, which an unclosed url bracket, string or
	// comment reaches; Parse recovers every *CssSyntaxError and returns it as a syntax error, so the panic
	// never leaves the package.
	panic(tokens.input.errorAt("Unclosed "+what, tokens.offset))
}

func (tokens *tokenizer) endOfFile() bool {
	return len(tokens.returned) == 0 && tokens.offset >= tokens.length
}

// nextToken returns the next token, and false where upstream returns undefined.
func (tokens *tokenizer) nextToken() (token, bool) {
	if len(tokens.returned) > 0 {
		last := tokens.returned[len(tokens.returned)-1]
		tokens.returned = tokens.returned[:len(tokens.returned)-1]
		return last, true
	}
	if tokens.offset >= tokens.length {
		return token{}, false
	}

	var currentToken token
	var next int
	position := tokens.offset
	code := tokens.characterCodeAt(position)

	switch code {
	case newline, space, tab, carriageReturn, feed:
		next = position
		for {
			next += 1
			code = tokens.characterCodeAt(next)
			if !(code == space || code == newline || code == tab || code == carriageReturn || code == feed) {
				break
			}
		}

		currentToken = token{kind: "space", value: tokens.input.slice(position, next), start: -1, end: -1}
		position = next - 1

	case openSquare, closeSquare, openCurly, closeCurly, colon, semicolon, closeParentheses:
		controlCharacter := string(rune(code))
		currentToken = token{kind: controlCharacter, value: controlCharacter, start: position, end: -1}

	case openParentheses:
		previous := ""
		if len(tokens.buffer) > 0 {
			previous = tokens.buffer[len(tokens.buffer)-1].value
			tokens.buffer = tokens.buffer[:len(tokens.buffer)-1]
		}
		nextCode := tokens.characterCodeAt(position + 1)
		if previous == "url" &&
			nextCode != singleQuote &&
			nextCode != doubleQuote &&
			nextCode != space &&
			nextCode != newline &&
			nextCode != tab &&
			nextCode != feed &&
			nextCode != carriageReturn {
			next = position
			for {
				escaped := false
				next = tokens.indexOf(")", next+1)
				if next == -1 {
					// ignore and ignoreUnclosed are always false here.
					tokens.offset = position
					tokens.unclosed("bracket")
				}
				escapePosition := next
				for tokens.characterCodeAt(escapePosition-1) == backslash {
					escapePosition -= 1
					escaped = !escaped
				}
				if !escaped {
					break
				}
			}

			currentToken = token{
				kind:  "brackets",
				value: tokens.input.slice(position, next+1),
				start: position,
				end:   next,
			}

			position = next
		} else if position <= tokens.lastBadParen {
			currentToken = token{kind: "(", value: "(", start: position, end: -1}
		} else {
			next = tokens.indexOf(")", position+1)
			var contentUnits []uint16
			if next != -1 {
				contentUnits = tokens.css[position : next+1]
			}

			if next == -1 || hasBadBracket(contentUnits) {
				if next == -1 {
					tokens.lastBadParen = tokens.length
				} else {
					tokens.lastBadParen = next
				}
				currentToken = token{kind: "(", value: "(", start: position, end: -1}
			} else {
				currentToken = token{
					kind:  "brackets",
					value: tokens.input.slice(position, next+1),
					start: position,
					end:   next,
				}
				position = next
			}
		}

	case singleQuote, doubleQuote:
		quote := "'"
		if code == doubleQuote {
			quote = "\""
		}
		next = position
		for {
			escaped := false
			next = tokens.indexOf(quote, next+1)
			if next == -1 {
				// ignore and ignoreUnclosed are always false here.
				tokens.offset = position
				tokens.unclosed("string")
			}
			escapePosition := next
			for tokens.characterCodeAt(escapePosition-1) == backslash {
				escapePosition -= 1
				escaped = !escaped
			}
			if !escaped {
				break
			}
		}

		currentToken = token{kind: "string", value: tokens.input.slice(position, next+1), start: position, end: next}
		position = next

	case at:
		// RE_AT_END.lastIndex = pos + 1; RE_AT_END.test(css): a match at index m leaves lastIndex m + 1,
		// and no match leaves it 0.
		match := -1
		for index := position + 1; index < tokens.length; index++ {
			if isAtEnd(tokens.css[index]) {
				match = index
				break
			}
		}
		if match == -1 {
			next = tokens.length - 1
		} else {
			next = match - 1
		}

		currentToken = token{kind: "at-word", value: tokens.input.slice(position, next+1), start: position, end: next}

		position = next

	case backslash:
		next = position
		escape := true
		for tokens.characterCodeAt(next+1) == backslash {
			next += 1
			escape = !escape
		}
		code = tokens.characterCodeAt(next + 1)
		if escape &&
			code != slash &&
			code != space &&
			code != newline &&
			code != tab &&
			code != carriageReturn &&
			code != feed {
			next += 1
			if isHexEscape(tokens.characterCodeAt(next)) {
				for isHexEscape(tokens.characterCodeAt(next + 1)) {
					next += 1
				}
				if tokens.characterCodeAt(next+1) == space {
					next += 1
				}
			}
		}

		currentToken = token{kind: "word", value: tokens.input.slice(position, next+1), start: position, end: next}

		position = next

	default:
		if code == slash && tokens.characterCodeAt(position+1) == asterisk {
			next = tokens.indexOf("*/", position+2) + 1
			if next == 0 {
				// ignore and ignoreUnclosed are always false here.
				tokens.offset = position
				tokens.unclosed("comment")
			}

			currentToken = token{
				kind:  "comment",
				value: tokens.input.slice(position, next+1),
				start: position,
				end:   next,
			}
			position = next
		} else {
			// RE_WORD_END.lastIndex = pos + 1; RE_WORD_END.test(css).
			match := -1
			for index := position + 1; index < tokens.length; index++ {
				unit := tokens.css[index]
				if isWordEnd(unit) || (unit == slash && tokens.characterCodeAt(index+1) == asterisk) {
					match = index
					break
				}
			}
			if match == -1 {
				next = tokens.length - 1
			} else {
				next = match - 1
			}

			currentToken = token{kind: "word", value: tokens.input.slice(position, next+1), start: position, end: next}
			tokens.buffer = append(tokens.buffer, currentToken)
			position = next
		}
	}

	position++
	tokens.offset = position
	return currentToken, true
}

func (tokens *tokenizer) back(each token) {
	tokens.returned = append(tokens.returned, each)
}
