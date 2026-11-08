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
	// readsFacts is whether fact notes are read beside diag notes, or refused as any other note is.
	readsFacts bool
	// wants and facts are the notes read so far.
	wants []Want
	facts []FactWant
}

// newNoteParser is a parser of body, the notes of a comment, which start at base in fileSet, reading fact notes
// beside diag notes when readsFacts is set.
func newNoteParser(fileSet *token.FileSet, base token.Pos, body string, readsFacts bool) *noteParser {
	parser := &noteParser{fileSet: fileSet, base: base, readsFacts: readsFacts}
	parser.scanner.Init(strings.NewReader(body))
	parser.scanner.Mode = scanner.GoTokens
	parser.scanner.Whitespace ^= 1 << '\n'
	parser.scanner.Error = func(_ *scanner.Scanner, message string) {
		parser.fail("%s", message)
	}
	return parser
}

// notes is the wants and the facts of the comment's notes, or the error naming where the first note it cannot read
// is.
func (parser *noteParser) notes() ([]Want, []FactWant, error) {
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
		if !parser.note() {
			break
		}
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
		return nil, nil, parser.failure
	}
	return parser.wants, parser.facts, nil
}

// note reads one note, a diag note of one argument, a string or re and a string, as a Want, or when facts are read,
// a fact note of two, an object's name and a string or re and a string, as a FactWant. It is false once the parse
// has failed.
func (parser *noteParser) note() bool {
	position := parser.fileSet.Position(parser.position())
	name := parser.consume()
	switch {
	case name == "diag":
	case name == "fact" && parser.readsFacts:
	case parser.readsFacts:
		parser.fail("a %s note, where this reader reads diag and fact notes alone", name)
		return false
	default:
		parser.fail("a %s note, where this reader reads diag notes alone", name)
		return false
	}
	if parser.token() != '(' {
		if name == "fact" {
			parser.fail("a fact note without its arguments, where fact(...) was expected")
		} else {
			parser.fail("a diag note without its argument, where diag(...) was expected")
		}
		return false
	}
	parser.consume()
	parser.skipNewlines()
	object := ""
	if name == "fact" {
		if parser.token() != scanner.Ident {
			parser.fail("unexpected %s in a fact note, where the name of the object it is about was expected",
				parser.tokenName())
			return false
		}
		object = parser.consume()
		if parser.token() != ',' {
			parser.fail("unexpected %s in a fact note, where a comma after the object's name was expected",
				parser.tokenName())
			return false
		}
		parser.consume()
		parser.skipNewlines()
	}
	text, pattern, isText := parser.textArgument(name)
	if !isText {
		return false
	}
	if parser.token() == ',' {
		parser.consume()
		parser.skipNewlines()
	}
	if parser.token() != ')' {
		if name == "fact" {
			parser.fail("unexpected %s in a fact note, where its two arguments close with )", parser.tokenName())
		} else {
			parser.fail("unexpected %s in a diag note, where its one argument closes with )", parser.tokenName())
		}
		return false
	}
	parser.consume()
	if name == "fact" {
		parser.facts = append(parser.facts, FactWant{
			File: position.Filename, Line: position.Line, Object: object, Text: text, Pattern: pattern,
		})
		return true
	}
	parser.wants = append(parser.wants, Want{
		File: position.Filename, Line: position.Line, Text: text, Pattern: pattern,
	})
	return true
}

// textArgument reads what a note named name expects the message or the fact to say: a string, whose text it
// returns, or re and a string, whose compiled pattern it returns. It is false once the parse has failed.
func (parser *noteParser) textArgument(name string) (string, *regexp.Regexp, bool) {
	isPattern := parser.token() == scanner.Ident && parser.scanner.TokenText() == "re"
	if isPattern {
		parser.consume()
	}
	if parser.token() != scanner.String && parser.token() != scanner.RawString {
		parser.fail("unexpected %s in a %s note, where a string or re and a string was expected", parser.tokenName(),
			name)
		return "", nil, false
	}
	text, unquoteError := strconv.Unquote(parser.consume())
	if unquoteError != nil {
		parser.fail("unquoting a %s note's argument: %v", name, unquoteError)
		return "", nil, false
	}
	if !isPattern {
		return text, nil, true
	}
	pattern, compileError := regexp.Compile(text)
	if compileError != nil {
		parser.fail("invalid regular expression %s: %v", text, compileError)
		return "", nil, false
	}
	return "", pattern, true
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
