// Package invisible_character is staticcheck's `ST1018`, a string literal written with a control character or a
// Unicode format character in it, which a reader cannot see and which may reorder the text around it, where an escape
// sequence says what is there, ported onto the engine's syntax dispatcher.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 stylecheck/st1018/st1018.go, whose messages and fixes
// it reports word for word, over the whole literal, as the original does: for one such character, a fix writing it as
// its escape sequence and one deleting it, and for several, a fix writing each as its escape sequence and one deleting
// them all. Like the original, it reports in a generated file too, and judges every string literal, an import path
// and a struct tag among them. It reads the characters of each literal as written, quotes included, through the same
// tables of the toolchain's unicode package the original reads, so the two agree on every character. The original
// requires only inspect, for its walk over the literals, which the dispatcher's one walk replaces.
//
// The original's MergeIf, MergeIfAny, tells staticcheck's runner to keep a finding any one build configuration makes,
// and the engine runs one configuration, so there is nothing for the port to merge.
package invisible_character

import (
	"fmt"
	"go/ast"
	"go/token"
	"strconv"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// Name is the original's code, which the port takes, so the rule, its settings and every `//cohere:ignore` naming it
// read the same as before the port took the original's place.
const Name = "ST1018"

// Doc is the original's title.
const Doc = "Avoid zero-width and control characters in string literals"

// zeroWidthJoiner joins the emoji on either side of it into one, and is a format character the check allows there.
const zeroWidthJoiner = '‍'

// Listener is what the port hears of each package from the dispatcher: each basic literal, of which it judges the
// strings.
var Listener = rule.Listener{
	FileLocal: true,
	Nodes:     []ast.Node{(*ast.BasicLit)(nil)},
	Node:      checkLiteral,
}

// Analyzer is the rule's own analyzer, reporting what the dispatcher's walk found for Listener.
var Analyzer = dispatch.RuleAnalyzer(rule.Namespace+Name, Name, Doc, &Listener)

// invisibleCharacter is one character of a literal the check reports, and where it starts in the literal as written,
// in bytes from the literal's opening quote.
type invisibleCharacter struct {
	character rune
	offset    int
}

// checkLiteral reports a string literal holding any character the check refuses, with the fixes for it.
func checkLiteral(context *rule.Context, node ast.Node, _ []ast.Node) {
	literal, isLiteral := node.(*ast.BasicLit)
	// A rune literal is no string, and the original judges strings alone.
	if !isLiteral || literal.Kind != token.STRING {
		return
	}
	invisibleCharacters := []invisibleCharacter{}
	hasFormat := false
	hasControl := false
	// previous is the character before the one judged, so a zero-width joiner is judged by what it joins. The literal
	// is read as written, so the first character judged is its opening quote, which the first character inside
	// follows.
	previous := rune(-1)
	for offset, character := range literal.Value {
		invisible := invisibleCharacter{character: character, offset: offset}
		switch {
		case unicode.Is(unicode.Cf, character):
			if !isAllowedFormat(character, previous) {
				invisibleCharacters = append(invisibleCharacters, invisible)
				hasFormat = true
			}
		// A newline, a tab and a carriage return are controls a string is expected to hold.
		case unicode.Is(unicode.Cc, character) && character != '\n' && character != '\t' && character != '\r':
			invisibleCharacters = append(invisibleCharacters, invisible)
			hasControl = true
		}
		previous = character
	}
	if len(invisibleCharacters) == 0 {
		return
	}
	kind := kindOf(hasFormat, hasControl)
	if len(invisibleCharacters) == 1 {
		invisible := invisibleCharacters[0]
		escape := escapeOf(invisible.character)
		staticcheck_report.Report(context, literal,
			fmt.Sprintf("string literal contains the Unicode %s character %U, consider using the %q escape "+
				"sequence instead", kind, invisible.character, invisible.character),
			staticcheck_report.Fixes(
				analysis.SuggestedFix{
					Message: fmt.Sprintf("replace %s character %U with %q", kind, invisible.character,
						invisible.character),
					TextEdits: []analysis.TextEdit{characterEdit(literal, invisible, []byte(escape))},
				},
				analysis.SuggestedFix{
					Message:   fmt.Sprintf("delete %s character %U", kind, invisible.character),
					TextEdits: []analysis.TextEdit{characterEdit(literal, invisible, nil)},
				},
			))
		return
	}
	escapes := []analysis.TextEdit{}
	deletions := []analysis.TextEdit{}
	for _, invisible := range invisibleCharacters {
		escapes = append(escapes, characterEdit(literal, invisible, []byte(escapeOf(invisible.character))))
		deletions = append(deletions, characterEdit(literal, invisible, nil))
	}
	staticcheck_report.Report(context, literal,
		fmt.Sprintf("string literal contains Unicode %s characters, consider using escape sequences instead", kind),
		staticcheck_report.Fixes(
			analysis.SuggestedFix{
				Message:   fmt.Sprintf("replace all %s characters with escape sequences", kind),
				TextEdits: escapes,
			},
			analysis.SuggestedFix{
				Message:   fmt.Sprintf("delete all %s characters", kind),
				TextEdits: deletions,
			},
		))
}

// isAllowedFormat is whether character, a format character following previous, is one the check lets a string hold:
// one an emoji is spelled with, or one that is no format character a reader cannot see.
func isAllowedFormat(character rune, previous rune) bool {
	switch {
	// The tag characters, which spell out the region of a flag emoji.
	case character >= '\U000e0020' && character <= '\U000e007f':
		return true
	// A variation selector, always allowed.
	case unicode.Is(unicode.Variation_Selector, character):
		return true
	// A zero-width joiner after a symbol or a variation selector, joining an emoji, including one spelled with a
	// variation selector. Some scripts join letters with it too, and the original reports those, as the port does.
	case character == zeroWidthJoiner &&
		(unicode.Is(unicode.S, previous) || unicode.Is(unicode.Variation_Selector, previous)):
		return true
	}
	switch character {
	// Arabic characters that are format characters to Unicode and are seen all the same. The bidirectional formatting
	// characters, U+061C, U+202A to U+202E and U+2066 to U+2069, are not among them, and are reported, since at best
	// they render confusingly and at worst they are written to deceive.
	case '؀', '؁', '؂', '؃', '؄', '؅', '࢐', '࢑', '࣢':
		return true
	}
	return false
}

// kindOf is how the message names the characters found: format, control, or both. At least one kind was found
// whenever the check reports, so a literal with no format character holds a control one.
func kindOf(hasFormat bool, hasControl bool) string {
	switch {
	case hasFormat && hasControl:
		return "format and control"
	case hasFormat:
		return "format"
	default:
		return "control"
	}
}

// escapeOf is character's escape sequence, as Go quotes it as a rune, without the quotes.
func escapeOf(character rune) string {
	quoted := strconv.QuoteRune(character)
	return quoted[1 : len(quoted)-1]
}

// characterEdit is the edit writing replacement in the place of invisible in literal, or deleting it when replacement
// is nil.
func characterEdit(literal *ast.BasicLit, invisible invisibleCharacter, replacement []byte) analysis.TextEdit {
	start := literal.Pos() + token.Pos(invisible.offset)
	return analysis.TextEdit{
		Pos:     start,
		End:     start + token.Pos(utf8.RuneLen(invisible.character)),
		NewText: replacement,
	}
}
