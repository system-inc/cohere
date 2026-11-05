// Package consistency_no_warning_comment is the Go engine's rule that a comment holds no task left for later.
//
// A `TODO`, `FIXME` or `XXX` in a comment is a deferred task kept where nobody is reminded of it. It rots there: the
// code around it reads as finished to every reader not looking at that line, and the task is done only if someone
// happens to edit the file. A deferred task belongs where tasks are tracked, where it is seen.
//
// The words are matched as written, in capitals, and only as whole words, so a sentence about `TODOS` or a lowercase
// todo is prose. Go's directives are instructions to a tool and are not read, and neither is anything in backticks,
// which is how a comment mentions the word without deferring anything, as this package's own comments do.
//
// Nor is a code block, the lines of a doc comment indented by a tab or further spaces after the slashes, which go/doc
// shows verbatim as code: a `TODO` there is an example of a comment, and backticks would render literally inside it.
// Which lines are a code block is judged by internal/shouting's mask, as the shouting rule judges them, so the two
// rules never disagree about one: a block opens after a blank comment line or at the group's start, and an indented
// line straight after prose is a paragraph continuation, as in CommonMark, and is read.
package consistency_no_warning_comment

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/shouting"
	"github.com/system-inc/cohere/policy"
)

// Name is what a finding of this rule prints.
const Name = "cohere-go/consistency-no-warning-comment"

// Analyzer runs the rule over one package.
var Analyzer = &analysis.Analyzer{
	Name: "consistency_no_warning_comment",
	Doc: "Flags TODO, FIXME or XXX written as a whole word in a comment, because a deferred task kept in a comment " +
		"rots where nobody is reminded of it. Directives, words in backticks and a doc comment's code blocks are not " +
		"read.",
	Run: run,
}

// Rule is this rule's entry in the Go engine's registry.
var Rule = rule.Rule{Name: Name, Origin: rule.OriginHouse, TypeAware: false, Analyzer: Analyzer}

var warningCommentMessage = policy.MessageOf(Name, "warningComment")

var (
	// directive is go/doc's own definition of a directive comment, from go.dev/doc/comment: a line starting
	// //line, //extern or //export and a space, or a tool's //name:directive with no space after the slashes. Its
	// arguments are the tool's words, not a task. The name is lowercase, so `//TODO:` is no directive and is read.
	directive = regexp.MustCompile(`^//(line |extern |export |[a-z0-9]+:[a-z0-9])`)

	// inlineBackticks is a span of code in a comment, which may wrap onto the next line of the group.
	inlineBackticks = regexp.MustCompile("`[^`]*`")

	// warningWord finds the candidates. Whether one is a whole word is isWholeWord's to say, since Go's \b knows only
	// ASCII and would read ÜTODO as holding the word.
	warningWord = regexp.MustCompile(`TODO|FIXME|XXX`)
)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.Pos())
		for _, group := range file.Comments {
			judgeGroup(pass, tokenFile, group)
		}
	}
	return nil, nil
}

// commentSpan is where one comment's body sits in its group's joined text. cut is how many bytes of the comment's
// text the body lost from its front, the slashes and the space after them, so an offset in the body plus cut is the
// same byte's offset in the comment's text.
type commentSpan struct {
	comment *ast.Comment
	start   int
	end     int
	cut     int
}

// judgeGroup reports every warning word in group outside a code block and backticks. The group's comment bodies are
// joined as one text before they are masked, so a code block opens only after a blank comment line, and a backtick
// pair a doc comment wraps across two of its // lines still hides the word inside it, as the shouting rule reads a
// group.
func judgeGroup(pass *analysis.Pass, tokenFile *token.File, group *ast.CommentGroup) {
	var joined strings.Builder
	spans := []commentSpan{}
	for _, comment := range group.List {
		// go/doc leaves a directive out of the comment's text, so it is neither read nor a line between its
		// neighbours.
		if directive.MatchString(comment.Text) {
			continue
		}
		if len(spans) > 0 {
			joined.WriteByte('\n')
		}
		// The body is what the code block mask measures indentation in, the text after the slashes and one space.
		body := shouting.GoCommentBody(comment.Text)
		start := joined.Len()
		joined.WriteString(body)
		spans = append(spans, commentSpan{
			comment: comment,
			start:   start,
			end:     joined.Len(),
			cut:     len(comment.Text) - len(body),
		})
	}
	text := joined.String()
	// Most comments hold none of the words, so they skip the masking.
	if !warningWord.MatchString(text) {
		return
	}
	// Code blocks are masked before backticks, in the order the shouting rule masks them, so a backtick inside a code
	// block opens no span over the prose after it. Each byte of a span becomes one space and each newline stays, so
	// every offset still points into the source.
	masked := inlineBackticks.ReplaceAllStringFunc(shouting.MaskIndentedCodeBlocks(text), func(match string) string {
		blanked := []byte(match)
		for index, character := range blanked {
			if character != '\n' {
				blanked[index] = ' '
			}
		}
		return string(blanked)
	})
	for _, span := range spans {
		for _, location := range warningWord.FindAllStringIndex(masked[span.start:span.end], -1) {
			start, end := span.cut+location[0], span.cut+location[1]
			if !isWholeWord(span.comment.Text, start, end) {
				continue
			}
			word := span.comment.Text[start:end]
			position := positionOf(pass, tokenFile, span.comment, start)
			pass.Report(analysis.Diagnostic{
				Pos:      position,
				End:      position + token.Pos(len(word)),
				Category: warningCommentMessage.Id,
				Message:  warningCommentMessage.Render(map[string]string{"word": word}),
			})
		}
	}
}

// isWholeWord is whether text[start:end] stands alone: no letter, digit or underscore of any script touches it on
// either side, so `TODOS`, `TODO_LIST`, `XXXX`, `0xXXX` and `ÜTODO` are other words.
func isWholeWord(text string, start int, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, _ := utf8.DecodeRuneInString(text[end:])
	return !isWordCharacter(before) && !isWordCharacter(after)
}

// isWordCharacter is a rune that continues a word. An empty side decodes to utf8.RuneError, which is none.
func isWordCharacter(character rune) bool {
	return character == '_' || unicode.IsLetter(character) || unicode.IsDigit(character)
}

// positionOf is the position of the byte at offset in comment.Text. The scanner drops each carriage return from a
// comment's text, so in a CRLF file a block comment's text is shorter than its source after its first line, and
// comment.Pos() plus the offset would drift by a byte a line. A line's own bytes come through intact, so the
// position is found as the line's start plus the column within it, by the file's own lines and not the ones a //line
// directive claims.
func positionOf(pass *analysis.Pass, tokenFile *token.File, comment *ast.Comment, offset int) token.Pos {
	before := comment.Text[:offset]
	lastNewline := strings.LastIndexByte(before, '\n')
	if lastNewline < 0 {
		return comment.Pos() + token.Pos(offset)
	}
	// Every newline in the text is one in the source, so the line exists in the file.
	line := pass.Fset.PositionFor(comment.Pos(), false).Line + strings.Count(before, "\n")
	return tokenFile.LineStart(line) + token.Pos(offset-lastNewline-1)
}
