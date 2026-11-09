package parity

import (
	"bytes"
	"fmt"
	"strings"
	"text/scanner"
)

// fixtureComment is one comment of a differences fixture's file: the line it opens on, the line it ends on, and its
// text, its opening and any closing included, as `// port finds: call to gained`.
type fixtureComment struct {
	line    int
	endLine int
	text    string
}

// assemblyComments are the comments of content, the assembly file named name, as the Go assembler lexes them:
// through text/scanner in the modes cmd/asm's tokenizer scans in, so a `//` inside a string or a character literal
// opens no comment, a block comment is one comment over every line it spans, and there are no raw strings. The
// whitespace and identifier runes cmd/asm also sets decide no comment, so they are left as text/scanner has them. A
// file the scanner cannot lex is an error naming where, since what follows the error could be misread.
func assemblyComments(name string, content []byte) ([]fixtureComment, error) {
	lexing := scanner.Scanner{}
	lexing.Init(bytes.NewReader(content))
	lexing.Filename = name
	lexing.Mode = scanner.ScanChars | scanner.ScanFloats | scanner.ScanIdents | scanner.ScanInts |
		scanner.ScanStrings | scanner.ScanComments
	refused := []string{}
	lexing.Error = func(at *scanner.Scanner, message string) {
		refused = append(refused, fmt.Sprintf("%s: %s", at.Pos(), message))
	}
	comments := []fixtureComment{}
	for scanned := lexing.Scan(); scanned != scanner.EOF; scanned = lexing.Scan() {
		if scanned != scanner.Comment {
			continue
		}
		text := lexing.TokenText()
		comments = append(comments, fixtureComment{
			line:    lexing.Position.Line,
			endLine: lexing.Position.Line + strings.Count(text, "\n"),
			text:    text,
		})
	}
	if len(refused) > 0 {
		return nil, fmt.Errorf("the assembler's lexer refuses %s: %s", name, strings.Join(refused, "; "))
	}
	return comments, nil
}
