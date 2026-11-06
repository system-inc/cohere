package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/warning_comment"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoWarningCommentText is the rule's message, whose wording lives in
// `policy/messages/consistency-no-warning-comment.json`, shared with the Go engine's twin.
var consistencyNoWarningCommentText = policy.MessageOf("nexus/consistency-no-warning-comment", "warningComment")

// ConsistencyNoWarningComment bans a task left for later in a comment: `TODO`, `FIXME` or `XXX` as a whole word.
//
//	valid:   // the `TODO` marker is what the rule reads
//	valid:   // TODO_LIST holds the queue
//	invalid: // TODO: fix this
//	invalid: return compute() // see FIXME below
//
// A deferred task kept in a comment rots there: the code around it reads as finished to every reader not looking at
// that line, and the task is done only if someone happens to edit the file. It belongs where tasks are tracked.
//
// The judgment is internal/warning_comment's, which the Go engine's cohere-go/consistency-no-warning-comment reads
// too, so the two engines report the same words: in capitals, whole, anywhere in any comment, outside backticks and
// code blocks, each finding on its word. ESLint's `no-warning-comments`, which this replaces in the style set, matched
// case-insensitively and only at a comment's start, so it reported a lowercase todo and missed a word mid-sentence or
// on a block comment's second line.
//
// No fix: the repair is moving the task to where tasks are tracked, which a rule cannot do.
var ConsistencyNoWarningComment = rule.Rule[rule.NoOptions]{
	Name: "nexus/consistency-no-warning-comment",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}
		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				sourceText := ruleContext.SourceFile.Text()
				for _, group := range warningCommentGroups(sourceText, comments.ForFile(ruleContext)) {
					texts := make([]string, 0, len(group))
					for _, comment := range group {
						// A carriage return before a newline is part of the line's end, as go/scanner drops it from
						// a Go comment's text. It becomes a space rather than going, so every offset still points
						// into the source, and a CRLF block comment's blank gutter line still reads as blank.
						texts = append(texts, strings.ReplaceAll(comment.Text, "\r\n", " \n"))
					}
					for _, word := range warning_comment.WordsIn(texts) {
						comment := group[word.Comment]
						ruleContext.ReportRange(
							core.NewTextRange(comment.Range.Pos()+word.Start, comment.Range.Pos()+word.End),
							rule.Message{
								Id: consistencyNoWarningCommentText.Id,
								Description: consistencyNoWarningCommentText.Render(
									map[string]string{"word": comment.Text[word.Start:word.End]},
								),
							},
						)
					}
				}
			},
		}
	},
}

// warningCommentGroups gathers a file's comments into the groups go/ast forms for Go, so a run of comments reads as
// one text in both engines and a backtick pair or a code block spanning its lines is seen whole. A comment joins the
// group before it when only whitespace separates them and it starts no further down than the line after the group's
// last comment ends. A group opened by a trailing comment, one with code before it on its line, takes only the
// comments on that same line, as go/parser groups a line comment, so the passage under a line of code is its own.
func warningCommentGroups(sourceText string, fileComments []comments.Comment) [][]comments.Comment {
	var groups [][]comments.Comment
	opensTrailing := false
	for _, comment := range fileComments {
		if len(groups) > 0 {
			group := groups[len(groups)-1]
			previous := group[len(group)-1]
			reach := 1
			if opensTrailing {
				reach = 0
			}
			isAdjacent := comment.StartLine <= previous.EndLine+reach &&
				text.TrimWhitespace(sourceText[previous.Range.End():comment.Range.Pos()]) == ""
			if isAdjacent {
				groups[len(groups)-1] = append(group, comment)
				continue
			}
		}
		groups = append(groups, []comments.Comment{comment})
		// StartColumn is a byte offset into the comment's line, so the line's start is that far before the comment.
		lineStart := comment.Range.Pos() - comment.StartColumn
		opensTrailing = text.TrimWhitespace(sourceText[lineStart:comment.Range.Pos()]) != ""
	}
	return groups
}
