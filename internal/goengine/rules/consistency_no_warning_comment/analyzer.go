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
//
// The judgment past Go's own grammar, the words, the whole-word test and the masks, is internal/warningcomment's,
// which the TypeScript rule nexus/consistency-no-warning-comment reads too, so the two engines report the same
// words. This package hands it each comment group with the directives set aside, and places each word it returns.
package consistency_no_warning_comment

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/warningcomment"
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

// directive is go/doc's own definition of a directive comment, from go.dev/doc/comment: a line starting //line,
// //extern or //export and a space, or a tool's //name:directive with no space after the slashes. Its arguments are
// the tool's words, not a task. The name is lowercase, so `//TODO:` is no directive and is read.
var directive = regexp.MustCompile(`^//(line |extern |export |[a-z0-9]+:[a-z0-9])`)

func run(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		tokenFile := pass.Fset.File(file.Pos())
		for _, group := range file.Comments {
			judgeGroup(pass, tokenFile, group)
		}
	}
	return nil, nil
}

// judgeGroup reports every warning word in group, outside a code block and backticks, as internal/warningcomment
// finds them. Go's directives are set aside first, since go/doc leaves a directive out of the comment's text, so it
// is neither read nor a line between its neighbours.
func judgeGroup(pass *analysis.Pass, tokenFile *token.File, group *ast.CommentGroup) {
	read := []*ast.Comment{}
	texts := []string{}
	for _, comment := range group.List {
		if directive.MatchString(comment.Text) {
			continue
		}
		read = append(read, comment)
		texts = append(texts, comment.Text)
	}
	for _, word := range warningcomment.WordsIn(texts) {
		comment := read[word.Comment]
		position := positionOf(pass, tokenFile, comment, word.Start)
		pass.Report(analysis.Diagnostic{
			Pos:      position,
			End:      position + token.Pos(word.End-word.Start),
			Category: warningCommentMessage.Id,
			Message:  warningCommentMessage.Render(map[string]string{"word": comment.Text[word.Start:word.End]}),
		})
	}
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
