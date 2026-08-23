// Package rule defines what a rule is and how it speaks.
//
// Every rule in verify walks the same AST that the type checker already built, in the same process
// and the same address space. That is the whole architecture in one sentence, and it is why rule
// number five hundred costs what rule number one hundred costs. Measured on a 3,416-file codebase,
// 107 rules living inside the linter parse 24.7 MB and run in 0.19s; 53 rules of identical shape
// living behind a JavaScript boundary cost 1.81s, entirely for the crossing.
//
// The shape here is adapted from typescript-eslint/tsgolint (MIT), which solved it well against
// this same checker.
package rule

import (
	"github.com/microsoft/typescript-go/shim/ast"
	"github.com/microsoft/typescript-go/shim/checker"
	"github.com/microsoft/typescript-go/shim/compiler"
	"github.com/microsoft/typescript-go/shim/core"
)

// Message is what a rule tells a reader when it fires.
//
// Id is stable and machine-facing: it survives rewording, so suppressions and metrics keep
// resolving. Description is the sentence a person reads, and it carries the reasoning rather than
// just the verdict — a rule that only says what is wrong gets disabled the first time it is
// inconvenient, while one that says why gets fixed.
type Message struct {
	Id          string
	Description string
}

// Fix is a proposed replacement of a range of source text.
//
// A rule proposes; it never applies. The edit engine owns what actually lands, because it is the
// only component that can see every proposal at once and reason about the cases a single rule
// cannot: two fixes overlapping the same bytes, one fix shifting the offsets every later fix was
// computed against, and a rewrite that parses as something other than what the author meant.
//
// An empty Text is a deletion.
type Fix struct {
	Range core.TextRange
	Text  string
}

// Suggestion is a fix that must not be applied automatically.
//
// The distinction is intent, not confidence. A fix preserves what the code means; a suggestion
// changes it, and a human has to agree. Awaiting a non-promise is a fix — removing the await cannot
// alter behavior. Adding a missing switch case is a suggestion, because only the author knows what
// belongs in the body.
type Suggestion struct {
	Message Message
	Fixes   []Fix
}

// Diagnostic is one finding: a range, what is wrong there, and optionally how to repair it.
type Diagnostic struct {
	RuleName    string
	Range       core.TextRange
	Message     Message
	SourceFile  *ast.SourceFile
	Fixes       []Fix
	Suggestions []Suggestion
}

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
	Program     *compiler.Program
	TypeChecker *checker.Checker

	// Report emits a finding. Prefer the helpers below, which spare a rule from restating how to
	// turn a node into a range.
	Report func(Diagnostic)
}

// Listeners maps an AST node kind to the function a rule wants called when the walk reaches it.
//
// One walk serves every rule. A rule that asked for its own pass over the tree would multiply the
// only genuinely unavoidable cost in the tool by the number of rules, which is the arrangement this
// design exists to refuse.
type Listeners map[ast.Kind]func(node *ast.Node)

// Rule is a name, and a function that returns what it wants to listen to.
//
// Run is called once per file, so a rule may allocate per-file state in its closure and read
// ctx.SourceFile while deciding whether to listen at all. Returning nil listeners is how a rule
// declines a file cheaply — the discipline that matters most for speed, since the cheapest rule is
// one that looks at a filename and stops.
type Rule struct {
	Name string
	Run  func(ctx Context, options any) Listeners
}

// ReportNode is the common case: this node is wrong, here is why.
func (c Context) ReportNode(node *ast.Node, message Message) {
	c.Report(Diagnostic{
		Range:      node.Loc,
		Message:    message,
		SourceFile: c.SourceFile,
	})
}

// ReportNodeWithFixes reports a node and proposes repairs the edit engine may apply unattended.
func (c Context) ReportNodeWithFixes(node *ast.Node, message Message, fixes ...Fix) {
	c.Report(Diagnostic{
		Range:      node.Loc,
		Message:    message,
		SourceFile: c.SourceFile,
		Fixes:      fixes,
	})
}

// ReportNodeWithSuggestions reports a node and offers repairs that need a human to choose them.
func (c Context) ReportNodeWithSuggestions(node *ast.Node, message Message, suggestions ...Suggestion) {
	c.Report(Diagnostic{
		Range:       node.Loc,
		Message:     message,
		SourceFile:  c.SourceFile,
		Suggestions: suggestions,
	})
}

// ReportRange reports a span that is not exactly one node — a portion of a string literal, or the
// gap between two tokens.
func (c Context) ReportRange(textRange core.TextRange, message Message) {
	c.Report(Diagnostic{
		Range:      textRange,
		Message:    message,
		SourceFile: c.SourceFile,
	})
}

// ReplaceNode proposes replacing a node's text.
func ReplaceNode(node *ast.Node, text string) Fix {
	return Fix{Range: node.Loc, Text: text}
}

// ReplaceRange proposes replacing an arbitrary span.
func ReplaceRange(textRange core.TextRange, text string) Fix {
	return Fix{Range: textRange, Text: text}
}

// RemoveNode proposes deleting a node.
func RemoveNode(node *ast.Node) Fix {
	return Fix{Range: node.Loc, Text: ""}
}

// InsertBefore proposes inserting text immediately before a node, touching no existing bytes.
func InsertBefore(node *ast.Node, text string) Fix {
	return Fix{Range: node.Loc.WithEnd(node.Loc.Pos()), Text: text}
}

// InsertAfter proposes inserting text immediately after a node, touching no existing bytes.
func InsertAfter(node *ast.Node, text string) Fix {
	return Fix{Range: node.Loc.WithPos(node.End()), Text: text}
}
