package rule

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/core"
)

// Context is what a rule is handed for one file.
//
// TypeChecker is present for every rule rather than gated behind a separate tier, and that is the
// point of building in the checker's own language: asking a type question is a function call rather
// than a subprocess. The tool this replaces spent 2.09 seconds per run rebuilding a 9,530-file
// program in a separate process to answer four such questions.
//
// TypeChecker is nil only when the program could not be built, which the caller reports as a
// type-phase failure before any rule runs.
type Context struct {
	SourceFile  *ast.SourceFile
	Program     Program
	TypeChecker *checker.Checker

	// Report emits a finding. Prefer the helpers below, which spare a rule from restating how to
	// turn a node into a range.
	//
	// Report and RecordNote are good only during Run and the listeners Run returns, for this file. The
	// walk makes each rule's pair once per worker and points it at whichever file it is dispatching,
	// so a rule that keeps them past its file reports into another file, or panics between files.
	Report func(Diagnostic)

	// FileCache holds work that is expensive per file and identical for every rule that wants it.
	//
	// One walk serves every rule, which is the whole architecture, but that only covers work the
	// walk itself does. A rule that derives something from the file outside the walk pays for it
	// alone, and three rules deriving the same thing pay three times. That is not hypothetical:
	// `cohere --timing` measured three comment rules at 1,777ms combined, each visiting exactly one
	// node per file, because each rescanned the same comment trivia independently. Flat node counts
	// with unequal times is the signature.
	//
	// Nil is legal and means no caching, so a harness that builds a Context by hand keeps working
	// and simply recomputes.
	FileCache *FileCache

	// RecordNote counts one note under a key. Rules call Note, which tolerates a nil RecordNote, so a
	// harness that builds a Context by hand needs none.
	RecordNote func(key string)
}

// ReportNode is the common case: this node is wrong, here is why.
//
// The finding is anchored on the node's own text rather than on node.Loc, and that is the whole
// difference between a usable finding and an unsuppressable one.
//
// Loc.Pos() sits before leading trivia, so a node preceded by a comment reports at the comment. A
// real case from the tree this gates: in modules/mcp/McpApi.ts the binding `params` is on line 242
// with its `eslint-disable-next-line` on 241, correctly covering it. Anchored on Loc the rule
// reported 240, swallowing both the comment and the indentation. **A `-next-line` directive only
// matches the line after itself, so that finding could not be suppressed by anything the author was
// able to write.**
//
// It is worse than a cosmetic offset because it is invisible in every count. A finding at the wrong
// line still reads as a real finding, costs nothing observable, and surfaces only as a suppression
// that mysteriously does not work. This root cause produced five distinct symptoms in one night: a
// fix that ate whitespace, an exemption that failed to fire, a line span measured from the wrong
// end, three findings reported at the wrong node, and this.
//
// So it is fixed here rather than at 30 call sites across 16 rule files. Same argument as the fix
// builders taking a Context: a rule should not be able to get this wrong by writing the obvious
// thing.
//
// A rule that genuinely wants the trivia included says so with ReportRange.
// Note records one occurrence of a fact the rule wants counted rather than reported: an exemption it
// applied, say, so that a tag silencing writes shows up as a number someone reads instead of as nothing.
// A note is not a finding and fails nothing. The walk counts notes per file, per rule and per key.
func (ruleContext Context) Note(key string) {
	if ruleContext.RecordNote != nil {
		ruleContext.RecordNote(key)
	}
}

// Skip records that the rule declined this file because a compiler option or a missing precondition left
// it unable to judge, and says which. Call it once, then return no listeners. A rule that declines this
// way otherwise reads exactly like a rule that looked and found nothing: no-useless-default-assignment
// declined every file in ahra and www while every run read green (#6ar414z, #pa7k7zv). It is not for a
// file the rule is simply not about, a `.d.ts` or a file outside its directory, which is its scope.
func (ruleContext Context) Skip(reason string) {
	ruleContext.Note(SkippedNotePrefix + reason)
}

// SkipCovered records that the rule declined this file because another check reports the same thing, and
// names that check. A skip whose cover ran this run left nothing unchecked, so the footer does not mark it;
// one whose cover did not run (`--lint`, or a types phase that bailed) is a gap like any other skip. A rule
// declining for any other reason calls Skip: choosing between the two is how a skip says which it is.
func (ruleContext Context) SkipCovered(cover Cover, reason string) {
	ruleContext.Note(CoveredSkipNotePrefix + string(cover) + ": " + reason)
}

func (ruleContext Context) ReportNode(node *ast.Node, message Message) {
	ruleContext.Report(Diagnostic{
		Range:      TokenRange(ruleContext.SourceFile, node),
		Message:    message,
		SourceFile: ruleContext.SourceFile,
	})
}

// ReportNodeWithFixes reports a node and proposes repairs the edit engine may apply unattended.
//
// Anchored on the node's own text, for the reason spelled out on ReportNode. This one matters twice
// over: a finding reported at the wrong line while carrying a fix means the fix is applied at a
// location the reader was never shown.
func (ruleContext Context) ReportNodeWithFixes(node *ast.Node, message Message, fixes ...Fix) {
	ruleContext.Report(Diagnostic{
		Range:      TokenRange(ruleContext.SourceFile, node),
		Message:    message,
		SourceFile: ruleContext.SourceFile,
		Fixes:      fixes,
	})
}

// ReportNodeWithSuggestions reports a node and offers repairs that need a human to choose them.
func (ruleContext Context) ReportNodeWithSuggestions(node *ast.Node, message Message, suggestions ...Suggestion) {
	ruleContext.Report(Diagnostic{
		Range:       TokenRange(ruleContext.SourceFile, node),
		Message:     message,
		SourceFile:  ruleContext.SourceFile,
		Suggestions: suggestions,
	})
}

// ReportRange reports a span that is not exactly one node: a portion of a string literal, or the
// gap between two tokens.
func (ruleContext Context) ReportRange(textRange core.TextRange, message Message) {
	ruleContext.Report(Diagnostic{
		Range:      textRange,
		Message:    message,
		SourceFile: ruleContext.SourceFile,
	})
}

// ReportRangeWithSuggestions reports a span that is not one node and offers repairs a human chooses.
//
// The node helpers came in three shapes and the range helpers in one, so a rule reporting a sub-range
// of a string literal with suggestions had no helper and had to hand-build a Diagnostic. That works
// and it is the wrong thing to make a rule author do: `Report` exists for the cases the helpers do not
// cover, and every hand-rolled Diagnostic is a place the Range can be built from `Loc` instead of
// `TokenRange` without anything downstream noticing.
//
// The shape was already settled elsewhere. The tsgolint adapter has exactly this method, because an
// adapted rule needed it first. Native rules simply did not have it, which is the kind of asymmetry
// that is invisible until someone writes the rule that falls in the gap.
func (ruleContext Context) ReportRangeWithSuggestions(
	textRange core.TextRange,
	message Message,
	suggestions ...Suggestion,
) {
	ruleContext.Report(Diagnostic{
		Range:       textRange,
		Message:     message,
		SourceFile:  ruleContext.SourceFile,
		Suggestions: suggestions,
	})
}

// ReportRangeWithFixes reports a span that is not one node and proposes repairs the engine applies
// unattended.
//
// The last corner of the same asymmetry ReportRangeWithSuggestions describes. The node helpers came
// in three shapes and the range helpers had reached two, so a rule whose finding is a computed span
// and whose repair is safe to apply had no helper and would have hand-built a Diagnostic.
//
// No trimming happens here, and that is the point rather than an omission. A range helper exists
// precisely because the caller computed a span the AST does not name, so a helper that adjusted it
// would be second-guessing the only party that knows what it means.
//
// The fixture harness has looked for this method name since before it existed: fixture_pair_test
// matches on `ReportRangeWithFixes` alongside `ReportNodeWithFixes` when deciding whether a rule
// proposes a fix. A rule reporting a computed span with a fix through a hand-built Diagnostic would
// have read to that guard as proposing nothing.
func (ruleContext Context) ReportRangeWithFixes(textRange core.TextRange, message Message, fixes ...Fix) {
	ruleContext.Report(Diagnostic{
		Range:      textRange,
		Message:    message,
		SourceFile: ruleContext.SourceFile,
		Fixes:      fixes,
	})
}

// NodeText is a node's own source text, from its first token to its end, without the leading trivia
// node.Pos() includes: what a reader sees as the node, and what a message or a fix quotes.
//
// One home for the slice of TokenRange that three rules each wrote privately (no-implicit-coercion,
// prefer-exponentiation-operator, valid-typeof), so the next rule reaches for this instead of a fourth copy.
//
// A method on the context, reading the context's own file, rather than a function taking a file: the
// type-reach guard counts text read out of any other file as descending into an imported body, so the
// only spelling offered is the one that cannot (internal/guard/type_reach_test.go).
func (ruleContext Context) NodeText(node *ast.Node) string {
	span := TokenRange(ruleContext.SourceFile, node)
	return ruleContext.SourceFile.Text()[span.Pos():span.End()]
}

// ReplaceNode proposes replacing a node's own text, leaving the trivia before it untouched.
func (ruleContext Context) ReplaceNode(node *ast.Node, text string) Fix {
	return Fix{Range: TokenRange(ruleContext.SourceFile, node), Text: text}
}

// RemoveNode proposes deleting a node's own text.
//
// Leading trivia is deliberately left behind rather than swept up with the node. Removing a node
// and removing the blank line above it are different intentions, and a helper that guessed would
// be wrong half the time. A rule wanting the surrounding whitespace gone should say so with
// ReplaceRange over a range it computed itself.
func (ruleContext Context) RemoveNode(node *ast.Node) Fix {
	return Fix{Range: TokenRange(ruleContext.SourceFile, node), Text: ""}
}

// InsertBefore proposes inserting text immediately before a node's own text.
//
// Before the token rather than before its trivia, which is almost always what a rule means: adding
// a modifier to a declaration should land next to the declaration, not above the comment that
// documents it.
func (ruleContext Context) InsertBefore(node *ast.Node, text string) Fix {
	tokenRange := TokenRange(ruleContext.SourceFile, node)
	return Fix{Range: tokenRange.WithEnd(tokenRange.Pos()), Text: text}
}

// InsertAfter proposes inserting text immediately after a node, touching no existing bytes.
//
// No trimming needed: a node's End is already past its own text, and trailing trivia belongs to
// whatever comes next.
func (ruleContext Context) InsertAfter(node *ast.Node, text string) Fix {
	return Fix{Range: node.Loc.WithPos(node.End()), Text: text}
}
