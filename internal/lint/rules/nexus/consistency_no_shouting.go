package nexus

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/shouting"
	"github.com/system-inc/cohere/policy"
)

// consistencyNoShoutingText is the rule's message, whose wording lives in
// `policy/messages/consistency-no-shouting.json`.
var consistencyNoShoutingText = policy.MessageOf("nexus/consistency-no-shouting", "shoutingInComment")

// messageShoutingInComment names the word it saw, because a finding that says only "this comment
// shouts" makes the reader rescan a paragraph to find which word tripped it.
func messageShoutingInComment(token string) rule.Message {
	return rule.Message{
		Id: consistencyNoShoutingText.Id,
		Description: consistencyNoShoutingText.Render(
			map[string]string{"tokens": shouting.QuotedTokenList([]string{token})},
		),
	}
}

// ConsistencyNoShouting bans all-caps words in comments.
//
//	valid:   // the `NOT_FOUND` branch returns early
//	valid:   // parse the JSON body before validating it
//	invalid: // NEVER cache this
//	invalid: // TODO: this is REALLY important
//
// Why a lint rule and not a style note: comments are the register the next reader inherits. A mind
// that opens a file already shouting comes up braced and writes its own comments a little louder to
// match, so the volume ratchets file by file until emphasis means nothing because everything has it.
//
// There is a craft argument underneath the tone one. "NEVER cache this" is a writer hoping volume
// will carry a claim the sentence does not. "Caching this returns a stale session id after a fork"
// needs no capitals, because it gives the reason. Turning the volume down tends to force the reason
// out, which is the actual repair.
//
// Backticks are the escape hatch, and they are the whole discipline: the rule is not "no capitals",
// it is "tell me which kind of capitals this is." Code wears backticks and is never read as
// shouting. Everything outside them is you talking.
//
// No fix, deliberately. Lowercasing the word leaves the sentence that needed shouting still weak,
// and the repair is usually to state the reason instead, which a rule cannot write.
//
// A run of `//` lines is masked as one text, as a block comment is, so a backtick pair or a fence a
// writer wrapped across the lines is code here as it is behind a block comment's gutter. Each
// finding covers one shouted word, the defect itself, so a long comment points at what to change.
// The generated-code marker a file opens with is not read: its capitals are fixed by a convention
// tools match exactly.
//
// The rule takes no options. Kirk ruled on 2026-11-06 (#4b337jk) that there is no per-project allow
// list: the shared safe list in internal/shouting, which every engine reads, is the one mechanism, so
// an acronym a project needs earns its place there with its reason, and a config that writes `allow`
// is refused.
var ConsistencyNoShouting = rule.Rule[rule.NoOptions]{
	Name: "nexus/consistency-no-shouting",
	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		if ruleContext.SourceFile == nil {
			return nil
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				for _, run := range commentRuns(ruleContext) {
					reportShoutedWords(ruleContext, run)
				}
			},
		}
	},
}

// commentRuns groups a file's comments into the texts the masks read: a block comment alone, and a
// run of `//` comments on consecutive lines with nothing but whitespace between them, which is one
// passage however many lines it takes. The generated-code marker is left out of every run, so its
// words are never read.
func commentRuns(ruleContext rule.Context) [][]comments.Comment {
	sourceText := ruleContext.SourceFile.Text()
	// The convention counts the marker only before the file's first token, so the same words lower in
	// the file are a comment like any other.
	firstToken := scanner.SkipTrivia(sourceText, 0)

	var runs [][]comments.Comment
	for _, comment := range comments.ForFile(ruleContext) {
		if comment.Range.End() <= firstToken && shouting.IsGeneratedMarker(comment.Text) {
			continue
		}
		if len(runs) > 0 && continuesLineCommentRun(sourceText, runs[len(runs)-1], comment) {
			runs[len(runs)-1] = append(runs[len(runs)-1], comment)
			continue
		}
		runs = append(runs, []comments.Comment{comment})
	}
	return runs
}

// continuesLineCommentRun reports whether a `//` comment carries on the run before it: the run is of
// `//` comments, the comment starts on the line after the run's last one, and only whitespace sits between
// the two, so no code separates them.
func continuesLineCommentRun(sourceText string, run []comments.Comment, comment comments.Comment) bool {
	previous := run[len(run)-1]
	if comment.IsBlock || previous.IsBlock || previous.EndLine+1 != comment.StartLine {
		return false
	}
	return text.TrimWhitespace(sourceText[previous.Range.End():comment.Range.Pos()]) == ""
}

// reportShoutedWords masks a run as one text and reports each shouted word in it, at the word.
//
// The comments are joined by a newline each, so a line of the joined text is a line of the source,
// and every comment's own offsets in it are kept, so a word maps back to the source by its comment's
// start.
func reportShoutedWords(ruleContext rule.Context, run []comments.Comment) {
	var joined strings.Builder
	starts := make([]int, 0, len(run))
	for index, comment := range run {
		if index > 0 {
			joined.WriteByte('\n')
		}
		starts = append(starts, joined.Len())
		joined.WriteString(comment.Text)
	}
	runText := joined.String()
	// The shared gate: without two capitals in a row nothing can be reported, and only about one
	// comment in twenty has them, so most runs skip the masking.
	if !shouting.HasAdjacentUppercaseLetters(runText) {
		return
	}
	masked := shouting.MaskCodeAndCommands(runText)
	for index, comment := range run {
		commentMasked := masked[starts[index] : starts[index]+len(comment.Text)]
		for _, shouted := range shouting.ShoutedTokensInMasked(commentMasked) {
			start := comment.Range.Pos() + shouted.Offset
			ruleContext.ReportRange(
				core.NewTextRange(start, start+len(shouted.Text)),
				messageShoutingInComment(shouted.Text),
			)
		}
	}
}
