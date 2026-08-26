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

	// NeedsTypeChecker declares that this rule reads ctx.TypeChecker.
	//
	// The checker is handed out under an exclusive per-file lock, so acquiring it serializes the
	// whole walk of that file against every other file's walk. Declaring the need lets the walk skip
	// the acquisition entirely on files where no applicable rule wants types.
	//
	// Measured before this field existed: the exclusive lock cost the lint phase about 50%, 366-417ms
	// against 557-575ms on the same tree, paid on every file for rules that never asked a type
	// question.
	//
	// # How much the skip is still worth, as a dated measurement rather than a standing claim
	//
	// **2026-08-25: 44 of 216 registered rules declare this.** So the acquisition is skipped on a
	// file only when none of those 44 applies to it, which is a real saving and is nothing like the
	// blanket one this comment used to describe.
	//
	// It previously read "every file in the current catalog: of 112 rule files exactly one reads the
	// checker, and it is the tsgolint adapter, which registers no rules yet." That was true when it
	// was written and false the moment the adapter registered, and nothing announced the change. It
	// is quoted here rather than deleted because it is the evidence for the rule underneath: **a rule
	// count in a comment decays silently, so write it with a date and treat it as a measurement that
	// expires.**
	//
	// A rule that leaves this false and then reads ctx.TypeChecker gets nil, which is the same thing
	// it gets when the program fails to build. That is deliberate: a rule silently reading a checker
	// it did not declare would reintroduce the data race this lock exists to prevent, and a nil
	// dereference is a loud failure where a race is a quiet one.
	//
	// # Why there is no shared references-to-a-binding helper
	//
	// Several rules want to know whether an identifier refers to a particular binding, and the
	// obvious move is a shelf helper that answers it. We decided against one, and the cost above is
	// the reason: a helper whose cost is a 50% serialization of the file walk is not a helper, it is
	// a decision, and putting it on the shelf hides the decision behind an import. A helper also has
	// to be correct for every caller, so it would have to be scope-aware, so it would have to take
	// the checker, so every caller would pay whether or not its own rule could be shadowed.
	//
	// So the question is answered per rule, at the cheapest tier that is correct for that rule:
	//
	//	name matching     enough where shadowing is impossible. core.NoExAssign resolves its
	//	                  binding this way, because a catch clause gives it a subtree to stop at.
	//	                  That is a claim about the tier and not a warrant for the rule: the same
	//	                  rule shipped for weeks missing `e++` entirely, which is a write-detection
	//	                  gap rather than a scope one, and neither corpus tested the shape.
	//	symbol identity   where shadowing is possible. Resolve the binding once with
	//	                  GetSymbolAtLocation, resolve each candidate, compare symbols. Declare
	//	                  NeedsTypeChecker so files with no such rule skip the acquisition.
	//
	// Note which question that answers. Find-all-references answers "where is this used", and these
	// rules already know where to look, since they walk one file. They need "is this the same
	// binding", which is a symbol comparison. Reaching for a reverse index here would be answering a
	// forward question with the wrong instrument.
	//
	// Symbol comparison is unmeasured on our tree. The first rule that needs it times it with
	// --timing against planted violations, and that number decides whether the rules after it follow
	// or stay on name matching with the shadowing hazard written at their own site.
	NeedsTypeChecker bool

	// ReadsProgram declares that this rule reads something outside the file it was handed.
	//
	// `ctx.Program` reaches every source file in the run, so a rule that touches it is not pure
	// per-file even when it declares NeedsTypeChecker false. Eleven rules do this today, counted
	// 2026-08-25 by grepping for the declaration rather than from memory: the two this note
	// originally named (localization-no-untranslated-value reads the English translation table,
	// boundary-no-project-theme-value scans every theme file), five tailwind rules that resolve
	// classes against the design system, three typescript rules, and react/unsupported-syntax.
	//
	// The count is recorded with its date because it drifts, and it drifted badly once already:
	// this note read "two rules" long after the tailwind family landed. Trust the grep over the
	// prose, and prefer reading the number off the declarations to citing it from here.
	//
	// The flag exists for the findings cache, and the failure it prevents is the one this tool
	// exists to catch. A cache keyed on one file's hash serves a stale result when a file the rule
	// also read has changed and the linted file has not: zero findings, forever, indistinguishable
	// from a clean tree. Nothing else notices.
	//
	// So this cannot live in a convention. A future rule author reaching for ctx.Program has no
	// reason to know they broke caching, exactly as NeedsTypeChecker exists because the analogous
	// property could not live in discipline either. The failure is worse here: a nil checker is a
	// loud crash, a stale cache is silence.
	//
	// The declaration is asymmetric on purpose, the same way NeedsTypeChecker is: under-declaring
	// serves stale findings forever, over-declaring costs a cache miss. Those are not comparable, so
	// anything that cannot see whether it reads the program declares true.
	//
	// That rule used to have a standing exception. The adapter at internal/rules/upstream handed
	// ctx.Program to rules whose bodies it did not own, so it declared this true for all of them by
	// assumption. The adapter is gone and the six rules it wrapped were absorbed, which made the
	// question answerable per rule: two of them genuinely read the program and declare it, four do
	// not and no longer claim to. Every rule in the tree now declares this from its own body.
	ReadsProgram bool

	// ResolvesReactValueTypes declares that this rule identifies a React value by asking the checker
	// for its TYPE, so a file where the hook call resolves to `any` costs it every finding it would
	// otherwise make, silently.
	//
	// This is narrower than NeedsTypeChecker and the difference is the whole reason the field exists.
	// Measured on 2026-08-24: of the forty-one rules declaring NeedsTypeChecker, only the two that
	// call GetTypeAtLocation and read the resulting type go blind when `useState` resolves to `any`.
	// The rest ask GetSymbolAtLocation, which resolves a BINDING rather than a type, and a binding to
	// `declare function useState<T>(initial: T): any` resolves exactly as well as one to the real
	// declaration. Probed both ways on the same input: under the ambient shim the symbol comes back
	// named `useState` with one declaration, while the call's type comes back `any` with TypeFlagsAny.
	//
	// So a message naming every NeedsTypeChecker rule as blinded would be confidently wrong about
	// thirty-nine of them, and a message naming a hardcoded list would be confidently wrong the week a
	// fifth rule ships. `structure/react-hook-any-type` reads this flag off the live catalog instead,
	// which is why the flag is a declaration on the rule rather than a list somewhere else: the rule
	// that goes blind is the only thing that knows it does.
	//
	// Under-declaring costs a reader the knowledge that suppressing the tripwire disables this rule
	// too. Over-declaring names a rule that would have survived. Neither is silent, which is what
	// makes this field cheaper to get wrong than the two above it.
	ResolvesReactValueTypes bool
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
func (c Context) ReportRangeWithSuggestions(textRange core.TextRange, message Message, suggestions ...Suggestion) {
	c.Report(Diagnostic{
		Range:       textRange,
		Message:     message,
		SourceFile:  c.SourceFile,
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
func (c Context) ReportRangeWithFixes(textRange core.TextRange, message Message, fixes ...Fix) {
	c.Report(Diagnostic{
		Range:      textRange,
		Message:    message,
		SourceFile: c.SourceFile,
		Fixes:      fixes,
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
//
// This is also why vendoring tsgolint's `rule/` package is declined while its `utils/` is taken.
// Its builders are correct and do trim, but they take the SourceFile as a parameter rather than a
// receiver (`RuleFixReplace(file, node, text)`), so the call that forgets it is available. Ours is
// not. Exposing both would give a rule author two reachable spellings of one act, one of them
// unsafe, and two helpers that almost agree drift permanently.
func TokenRange(sourceFile *ast.SourceFile, node *ast.Node) core.TextRange {
	if sourceFile == nil || node == nil {
		return node.Loc
	}
	return scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).WithEnd(node.End())
}

// The fix helpers come in two families, and picking the wrong one is the most common way a correct
// rule produces a wrong edit.
//
// The node forms below (ReplaceNode, RemoveNode, InsertBefore, InsertAfter) are methods on Context
// because they trim to the token and trimming needs the SourceFile. Reach for these by default: a
// rule that says "this node" means the node, not the comment above it. The range forms
// (ReplaceRange, RemoveRange) are package-level and trim nothing, because a caller that computed its
// own span already said exactly what it meant. That split is why the safe form is also the
// convenient one, and why the trivia-eating shape is unreachable rather than merely discouraged.
//
// Two properties of the engine that consumes these are worth knowing before writing a fix.
//
// A Fix must preserve meaning, because it is applied unattended. When the correct edit requires
// choosing between alternatives only the author can rank, that is a Suggestion instead, and the
// engine never applies suggestions. core.NoCaseDeclarations is the shipped example: wrapping a case
// clause in braces changes what the code means, so it reports a Suggestion even though the edit is
// mechanical and unambiguous. Mechanical is not the test; meaning-preserving is.
//
// Fixes reported together are not applied together. ProposalsFrom flattens a diagnostic's fixes into
// independent proposals, so overlap resolution may admit one and refuse the other. A pair that only
// means something jointly needs grouping in the engine first; reporting them side by side does not
// buy atomicity. core.NoCaseDeclarations shows this too: its InsertBefore and InsertAfter are an
// opening and a closing brace, and half of that pair is broken code. It is safe today only because
// it is a Suggestion and never reaches the engine. See ProposalsFrom in internal/fix.
//
// The engine's refusal guard checks that the rewritten file parses, and parsing is necessary rather
// than sufficient. Anything sayable in valid syntax about a name that no longer exists slips
// through it: a rename that updates a usage and misses its declaration produces a file that parses
// and does not compile, and the guard has no reason to fire.
//
// That is not hypothetical. structure/react-component-require-properties-type-suffix renames a
// component's properties type at both the usage and the declaration, using a map that one listener
// fills and the other reads. Source order decides which listener runs first, so on the conventional
// declaration-first ordering the declaration is visited against an empty map and skipped: the usage
// is renamed, the declaration is not, and the file then references a type that does not exist. It
// parses. Measured: one diagnostic on that ordering against two on the other, and the rewritten
// source fails to type-check with "cannot find name".
//
// So a fix that renames anything carries an obligation the engine cannot discharge for it: every
// reference to the old name must move in the same pass, and a fixture has to assert it on both
// orderings, since either one alone passes. The failure also repairs the evidence of itself, because
// the rule's own fix silences the finding that would have reported the rest of the work.

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
