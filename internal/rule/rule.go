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
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/checker"
	"github.com/microsoft/TypeScript/tsc/shim/compiler"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
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

	// FileCache holds work that is expensive per file and identical for every rule that wants it.
	//
	// One walk serves every rule, which is the whole architecture, but that only covers work the
	// walk itself does. A rule that derives something from the file outside the walk pays for it
	// alone, and three rules deriving the same thing pay three times. That is not hypothetical:
	// `verify --timing` measured three comment rules at 1,777ms combined, each visiting exactly one
	// node per file, because each rescanned the same comment trivia independently. Flat node counts
	// with unequal times is the signature.
	//
	// Nil is legal and means no caching, so a harness that builds a Context by hand keeps working
	// and simply recomputes.
	FileCache *FileCache
}

// FileCache memoizes per-file derived work across the rules that share it.
//
// Deliberately untyped and keyed by string rather than holding named fields. This package must not
// know what a comment scan is, or a scope table, or whatever the next expensive shared derivation
// turns out to be. The rule packages own those; this owns only the fact that they are worth
// computing once.
//
// Not safe for concurrent use, and it does not need to be: one cache is created per file and every
// rule for that file runs on the one goroutine that owns it. Sharing a cache across files or
// workers would be a bug, which is why nothing here has a mutex to make that look safe.
type FileCache struct {
	entries map[string]any

	// fillDurations records what each derivation cost to compute, keyed the same way the entries
	// are.
	//
	// This exists because per-rule timing lies about shared work. Whichever rule asks for a
	// derivation first pays for it, and since files are walked in parallel the identity of that
	// rule varies per file. Measured on three comment rules sharing one scan: 171ms, 132ms, and
	// 1.0ms for identical work, where the 1.0ms rule was simply the one that asked last.
	//
	// Attributing the cost here rather than to a rule is the only reading that stays true as the
	// order changes.
	fillDurations map[string]time.Duration
}

// NewFileCache returns a cache for one file.
func NewFileCache() *FileCache {
	return &FileCache{
		entries:       map[string]any{},
		fillDurations: map[string]time.Duration{},
	}
}

// FillDurations reports what each derivation cost to compute in this file, by key.
//
// Empty for a cache that was never filled, which is the common case for a file no comment rule
// looked at.
func (c *FileCache) FillDurations() map[string]time.Duration {
	if c == nil {
		return nil
	}
	return c.fillDurations
}

// Cached returns the value stored under key, computing it once on the first ask.
//
// A nil cache computes every time rather than failing, so a rule reads the same whether or not the
// caller supplied one. That keeps the fast path an optimization rather than a requirement.
func Cached[Value any](cache *FileCache, key string, compute func() Value) Value {
	if cache == nil || cache.entries == nil {
		return compute()
	}
	if existing, isCached := cache.entries[key]; isCached {
		if typed, isTyped := existing.(Value); isTyped {
			return typed
		}
		// Two callers used one key for different types. Recomputing is the safe answer, and it is
		// silent on purpose: the alternative is a rule package crashing a lint run over a cache.
		return compute()
	}
	start := time.Now()
	computed := compute()
	cache.fillDurations[key] += time.Since(start)

	cache.entries[key] = computed
	return computed
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
func (c Context) ReportNode(node *ast.Node, message Message) {
	c.Report(Diagnostic{
		Range:      TokenRange(c.SourceFile, node),
		Message:    message,
		SourceFile: c.SourceFile,
	})
}

// ReportNodeWithFixes reports a node and proposes repairs the edit engine may apply unattended.
//
// Anchored on the node's own text, for the reason spelled out on ReportNode. This one matters twice
// over: a finding reported at the wrong line while carrying a fix means the fix is applied at a
// location the reader was never shown.
func (c Context) ReportNodeWithFixes(node *ast.Node, message Message, fixes ...Fix) {
	c.Report(Diagnostic{
		Range:      TokenRange(c.SourceFile, node),
		Message:    message,
		SourceFile: c.SourceFile,
		Fixes:      fixes,
	})
}

// ReportNodeWithSuggestions reports a node and offers repairs that need a human to choose them.
func (c Context) ReportNodeWithSuggestions(node *ast.Node, message Message, suggestions ...Suggestion) {
	c.Report(Diagnostic{
		Range:       TokenRange(c.SourceFile, node),
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

// TokenRange is a node's own text, without the trivia that precedes it.
//
// This distinction is the single sharpest edge in the fix API, and getting it wrong produces damage
// that no downstream check can catch.
//
// `node.Loc.Pos()` is the position *before* leading trivia, not the start of the node's own text.
// For `import * as X from 'fs'`, the module specifier's Loc spans `" 'fs'"` — the space is inside
// the range. A fix built from Loc therefore replaces the whitespace too:
//
//	import * as X from 'fs'   ->   import * as X from'node:fs'
//
// and `from /* pinned */ 'fs'` loses the comment permanently.
//
// The reason this is worse than cosmetic: **the corrupted output still parses.** The edit engine
// refuses a rewrite that breaks syntax, and that guard never fires here, so the damage sails
// through with every downstream check green. A range wider than the rule intended is damage that
// validation downstream is structurally unable to detect. The parse guard is necessary and not
// sufficient.
//
// Trimming needs the SourceFile, because finding where a token actually starts means scanning
// forward past the trivia. That is why the helpers below hang off Context: a rule always has one,
// so the safe form is also the convenient one, and the shape that eats trivia is not reachable by
// accident.
//
// Found by @system_verify_lint_fix, running real rules through the engine on three trivia shapes
// rather than reasoning about the ranges. Upstream tsgolint gets this right via
// `utils.TrimNodeTextRange`; our port dropped the step, most likely because the signature had no
// SourceFile to trim with.
func TokenRange(sourceFile *ast.SourceFile, node *ast.Node) core.TextRange {
	if sourceFile == nil || node == nil {
		return node.Loc
	}
	return scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).WithEnd(node.End())
}

// ReplaceNode proposes replacing a node's own text, leaving the trivia before it untouched.
func (c Context) ReplaceNode(node *ast.Node, text string) Fix {
	return Fix{Range: TokenRange(c.SourceFile, node), Text: text}
}

// RemoveNode proposes deleting a node's own text.
//
// Leading trivia is deliberately left behind rather than swept up with the node. Removing a node
// and removing the blank line above it are different intentions, and a helper that guessed would
// be wrong half the time. A rule wanting the surrounding whitespace gone should say so with
// ReplaceRange over a range it computed itself.
func (c Context) RemoveNode(node *ast.Node) Fix {
	return Fix{Range: TokenRange(c.SourceFile, node), Text: ""}
}

// InsertBefore proposes inserting text immediately before a node's own text.
//
// Before the token rather than before its trivia, which is almost always what a rule means: adding
// a modifier to a declaration should land next to the declaration, not above the comment that
// documents it.
func (c Context) InsertBefore(node *ast.Node, text string) Fix {
	tokenRange := TokenRange(c.SourceFile, node)
	return Fix{Range: tokenRange.WithEnd(tokenRange.Pos()), Text: text}
}

// InsertAfter proposes inserting text immediately after a node, touching no existing bytes.
//
// No trimming needed: a node's End is already past its own text, and trailing trivia belongs to
// whatever comes next.
func (c Context) InsertAfter(node *ast.Node, text string) Fix {
	return Fix{Range: node.Loc.WithPos(node.End()), Text: text}
}

// ReplaceRange proposes replacing an arbitrary span.
//
// The escape hatch for a rule that computed its own range: a portion of a string literal, or the
// gap between two tokens. Nothing is trimmed, because the caller already said exactly what it
// meant.
func ReplaceRange(textRange core.TextRange, text string) Fix {
	return Fix{Range: textRange, Text: text}
}

// RemoveRange proposes deleting an arbitrary span.
func RemoveRange(textRange core.TextRange) Fix {
	return Fix{Range: textRange, Text: ""}
}
