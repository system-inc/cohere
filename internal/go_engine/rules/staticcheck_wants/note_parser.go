package staticcheck_wants

import (
	"fmt"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"text/scanner"
)

// noToken is current when no token has been scanned past the last one consumed.
const noToken rune = 0

// noteParser reads the notes of one comment with the Go tokens go/expect scans them as, newlines kept, since a
// newline separates notes as a comma does.
type noteParser struct {
	// scanner scans the comment's notes.
	scanner scanner.Scanner
	// current is the token scanned and not yet consumed, or noToken.
	current rune
	// failure is the first error the scan or the parse met, after which every token reads as the end.
	failure error
	// fileSet places the notes, and base is where in it the comment's notes start.
	fileSet *token.FileSet
	base    token.Pos
}

// newNoteParser is a parser of body, the notes of a comment, which start at base in fileSet.
func newNoteParser(fileSet *token.FileSet, base token.Pos, body string) *noteParser {
	parser := &noteParser{fileSet: fileSet, base: base}
	parser.scanner.Init(strings.NewReader(body))
	parser.scanner.Mode = scanner.GoTokens
	parser.scanner.Whitespace ^= 1 << '\n'
	parser.scanner.Error = func(_ *scanner.Scanner, message string) {
		parser.fail("%s", message)
	}
	return parser
}

// wants is the wants of the comment's notes, or the error naming where the first note it cannot read is.
func (parser *noteParser) wants() ([]Want, error) {
	wants := []Want{}
	// Once the parse fails every token reads as the end, so each way out of the loop is a break.
	for {
		parser.skipNewlines()
		if parser.token() == scanner.EOF {
			break
		}
		if parser.token() != scanner.Ident {
			parser.fail("unexpected %s, where a note's name was expected", parser.tokenName())
			break
		}
		want, isWant := parser.note()
		if !isWant {
			break
		}
		wants = append(wants, want)
		if parser.token() == scanner.EOF {
			break
		}
		if parser.token() != ',' && parser.token() != '\n' {
			parser.fail("unexpected %s after a note, where a comma or a new line was expected", parser.tokenName())
			break
		}
		parser.consume()
	}
	if parser.failure != nil {
		return nil, parser.failure
	}
	return wants, nil
}

// note reads one note, a diag note of one argument, a string or re and a string, as a Want.
func (parser *noteParser) note() (Want, bool) {
	position := parser.fileSet.Position(parser.position())
	name := parser.consume()
	if name != "diag" {
		parser.fail("a %s note, where this reader reads diag notes alone", name)
		return Want{}, false
	}
	if parser.token() != '(' {
		parser.fail("a diag note without its argument, where diag(...) was expected")
		return Want{}, false
	}
	parser.consume()
	parser.skipNewlines()
	want := Want{File: position.Filename, Line: position.Line}
	isPattern := parser.token() == scanner.Ident && parser.scanner.TokenText() == "re"
	if isPattern {
		parser.consume()
	}
	if parser.token() != scanner.String && parser.token() != scanner.RawString {
		parser.fail("unexpected %s in a diag note, where a string or re and a string was expected", parser.tokenName())
		return Want{}, false
	}
	text, unquoteError := strconv.Unquote(parser.consume())
	if unquoteError != nil {
		parser.fail("unquoting a diag note's argument: %v", unquoteError)
		return Want{}, false
	}
	if isPattern {
		pattern, compileError := regexp.Compile(text)
		if compileError != nil {
			parser.fail("invalid regular expression %s: %v", text, compileError)
			return Want{}, false
		}
		want.Pattern = pattern
	} else {
		want.Text = text
	}
	if parser.token() == ',' {
		parser.consume()
		parser.skipNewlines()
	}
	if parser.token() != ')' {
		parser.fail("unexpected %s in a diag note, where its one argument closes with )", parser.tokenName())
		return Want{}, false
	}
	parser.consume()
	return want, true
}

// token is the current token, scanning one if none is current, and the end once the parse has failed.
func (parser *noteParser) token() rune {
	if parser.failure != nil {
		return scanner.EOF
	}
	if parser.current == noToken {
		parser.current = parser.scanner.Scan()
	}
	return parser.current
}

// consume takes the current token, returning its text.
func (parser *noteParser) consume() string {
	parser.current = noToken
	return parser.scanner.TokenText()
}

// skipNewlines consumes the newlines that come next.
func (parser *noteParser) skipNewlines() {
	for parser.token() == '\n' {
		parser.consume()
	}
}

// tokenName is the current token as a message names it.
func (parser *noteParser) tokenName() string {
	return scanner.TokenString(parser.token())
}

// position is where the token last scanned starts.
func (parser *noteParser) position() token.Pos {
	return parser.base + token.Pos(parser.scanner.Position.Offset)
}

// fail records the first error, placed at the token last scanned.
func (parser *noteParser) fail(format string, arguments ...any) {
	if parser.failure != nil {
		return
	}
	parser.failure = fmt.Errorf("%s: %s", parser.fileSet.Position(parser.position()), fmt.Sprintf(format, arguments...))
}
