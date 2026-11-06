// Package rule defines what a rule is and how it speaks.
//
// Every rule in cohere walks the same AST that the type checker already built, in the same process
// and the same address space. That is the whole architecture in one sentence, and it is why rule
// number five hundred costs what rule number one hundred costs. Measured on a 3,416-file codebase,
// 107 rules living inside the linter parse 24.7 MB and run in 0.19s; 53 rules of identical shape
// living behind a JavaScript boundary cost 1.81s, entirely for the crossing.
//
// The shape here is adapted from typescript-eslint/tsgolint (MIT), which solved it well against
// this same checker.
package rule

import (
	"crypto/sha256"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/microsoft/TypeScript/tsc/shim/scanner"
)

// Message is what a rule tells a reader when it fires.
//
// Id is stable and machine-facing: it survives rewording, so suppressions and metrics keep
// resolving. Description is the sentence a person reads, and it carries the reasoning rather than
// just the verdict. A rule that only says what is wrong gets disabled the first time it is
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
// changes it, and a human has to agree. Awaiting a non-promise is a fix: removing the await cannot
// alter behavior. Adding a missing switch case is a suggestion, because only the author knows what
// belongs in the body.
type Suggestion struct {
	Message Message
	Fixes   []Fix
}

// NewFileCache returns a cache for one file.
func NewFileCache() *FileCache {
	return &FileCache{
		entries: map[string]cacheEntry{},
		fills:   map[string]int{},
	}
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
		if typed, isTyped := existing.(cachedValue[Value]); isTyped {
			return typed.value
		}
		// Two callers used one key for different types. Recomputing is the safe answer, and it is
		// silent on purpose: the alternative is a rule package crashing a lint run over a cache.
		return compute()
	}
	var computed Value
	if cache.aroundFill != nil {
		cache.aroundFill(key, func() { computed = compute() })
	} else {
		computed = compute()
	}
	cache.fills[key]++

	cache.entries[key] = cachedValue[Value]{value: computed}
	return computed
}

// cacheEntry is one derivation's value in a FileCache, whatever its type. It is sealed: only cachedValue
// implements it, so Cached reads an entry back as the one type it was stored with.
type cacheEntry interface {
	fileCacheEntry()
}

// cachedValue is a derivation's value as a FileCache stores it.
type cachedValue[Value any] struct {
	value Value
}

func (cachedValue[Value]) fileCacheEntry() {}

// Listeners maps an AST node kind to the function a rule wants called when the walk reaches it.
//
// One walk serves every rule. A rule that asked for its own pass over the tree would multiply the
// only genuinely unavoidable cost in the tool by the number of rules, which is the arrangement this
// design exists to refuse.
type Listeners map[ast.Kind]func(node *ast.Node)

// TypeReach is what a type-aware rule can see of imported files. See Rule.TypeReach.
type TypeReach string

const (
	// TypeReachContents keys a type-aware rule's findings on the bytes of its file's whole import closure.
	TypeReachContents TypeReach = ""

	// TypeReachShapes keys them on the closure's shapes, for a rule that never reads into an imported
	// function's body.
	TypeReachShapes TypeReach = "Shapes"
)

// NoListenerKind is why a rule may register no listener on a file it was offered and still have done
// its job there.
//
// Coverage cannot tell three rules apart by their numbers: offered files, listened to none, reported
// nothing. One answers from the file in Run and needs no walk, one declines every file it is not about
// and met none, and one is left inert by its options and checks nothing anywhere. The first two are
// working and the third is dead, so the rule says which it is (#j69gvka). A rule that declares nothing
// is expected to listen, and registering no listener anywhere is then reported as checking nothing.
type NoListenerKind string

const (
	// NoListenerUndeclared is a rule that listens on the files it is about, so no listener anywhere
	// means it checked nothing.
	NoListenerUndeclared NoListenerKind = ""

	// NoListenerAnswersInRun is a rule that does its whole job in Run and never registers a listener:
	// it reads what it needs off the file, reports, and returns nil.
	NoListenerAnswersInRun NoListenerKind = "AnswersInRun"

	// NoListenerDeclinesIrrelevantFiles is a rule that registers listeners only on files that can hold
	// what it checks (by path, by language, or by what the file imports), so a tree with no such file
	// gives it none.
	NoListenerDeclinesIrrelevantFiles NoListenerKind = "DeclinesIrrelevantFiles"
)

// Rule is a name, and a function that returns what it wants to listen to.
//
// Run is called once per file, so a rule may allocate per-file state in its closure and read
// ctx.SourceFile while deciding whether to listen at all. Returning nil listeners is how a rule
// declines a file cheaply, the discipline that matters most for speed, since the cheapest rule is
// one that looks at a filename and stops.
//
// Options is the type the rule's options decode into, and NoOptions for a rule that takes none. Run and
// ProgramFingerprint are handed exactly that type, and Registration ties it to the decoder's result, so a
// decoder returning a pointer while the rule reads a value no longer compiles (#ymag4q6). Before this, options
// travelled as `any` and each rule asserted its own type at runtime, and that drift went unseen in
// unified-signatures until a fixture configured the rule (#qmvkf83).
//
// The framework never holds a Rule, since rules of different option types share one list. It holds an Entry
// (registered) or a Bound (options decided), which carry the rule's Traits by name.
type Rule[Options any] struct {
	Name string
	Run  func(ruleContext Context, options Given[Options]) Listeners

	// NoListener declares why this rule may register no listener on a file it was offered and still
	// have done its job. See NoListenerKind. Left undeclared, a rule offered files that registers no
	// listener on any of them is reported by default as having checked nothing.
	NoListener NoListenerKind

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
	// **2026-08-25: 44 of 216 registered rules declare this, roughly a fifth.** So the acquisition
	// is skipped on a file only when none of that fifth applies to it, which is a real saving and is
	// nothing like the blanket one this comment used to describe.
	//
	// The share is what the claim rests on rather than either count, and
	// `TestCheckerDeclarationShareStillSupportsTheConclusion` asserts it as a band for that reason.
	// A porting wave moves the total on every commit without touching what this paragraph concludes;
	// a shift in the ratio is what would make it false.
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

	// ProgramReads declares what this rule reads through ctx.Program beyond the file it was handed.
	// See ProgramRead for the kinds and what covers each.
	//
	// The declaration exists for the findings cache, and the failure it prevents is the one this tool
	// exists to catch. A cache keyed on one file serves a stale result when something the rule also
	// read has changed and the key has not: zero findings, forever, indistinguishable from a clean
	// tree. So it cannot live in a convention, and ctx.Program enforces it: a method outside the
	// declaration panics with the rule's name, and the walk names the panic on every run.
	//
	// It replaced ReadsProgram, one flag for every kind, which kept 45 rules out of the cache although
	// most read only compiler options, the default library, or their own file's module resolution,
	// all of which the key already covers. Declaring more than a rule reads costs a cache miss;
	// declaring less is refused at the read. A rule reading a helper's program methods declares what
	// the helper reads.
	ProgramReads ProgramRead

	// ProgramFingerprint, on a rule that declares ReadsOtherFiles, is a hash of exactly the program-wide data the
	// rule's verdict on one file depends on beyond that file: the theme interfaces it indexes, the import graph's
	// edges, the one file it reads. With it, the findings cache replays the rule on a file whose bytes are
	// unchanged while this hash is too (#kdee854). It is called for each set of options the run's
	// configuration gives the rule, with a Program viewed under the rule's own ProgramReads and the options as
	// Run receives them, since an option can choose which data the rule reads: no-html-link-for-pages' pagesDir
	// names the directory its routes come from (#s9k38p3). The options themselves need no hashing: every
	// config file is in the findings cache's key. A walk calls it once for each set of options it gives the
	// rule, on whichever worker asks first, while other rules' fingerprints may be computed on other workers,
	// so it must be safe for concurrent use.
	//
	// It is a claim of completeness, and it decides soundness: anything the verdict reads outside the file and
	// outside this hash replays a stale finding when it changes. So it is proven both ways for each rule, a
	// planted change to the indexed data moving it and an edit elsewhere leaving it alone. A rule that declares
	// ReadsOtherFiles without one is walked on every file, as before: the default is always the sound one.
	ProgramFingerprint func(program Program, options Given[Options]) [sha256.Size]byte

	// TypeReach declares what a type-aware rule can see of the files its file imports, and so what its
	// cached findings are keyed on. Contents, the default, keys them on the bytes of every file in the
	// import closure. Shapes keys them on each imported file's shape: its declaration output and its text
	// with function bodies cut out (program.SignatureEntry), so an edit inside a body of a file imported
	// by half the tree re-runs this rule on that one file rather than on half the tree.
	//
	// Shapes is a claim that the rule never reads into an imported function's body, which a shape does
	// not cover. The claim is checked rather than trusted: a scan of every rule and the helpers it
	// reaches refuses Shapes on any rule that can reach both an imported declaration and a body
	// (TestRulesClaimShapesOnlyWhereTheScanAllowsIt). Claiming wrongly replays a stale finding; not
	// claiming costs a re-run.
	TypeReach TypeReach

	// ResolvesReactValueTypes declares that this rule identifies a React value by asking the checker
	// for its type, so a file where the hook call resolves to `any` costs it every finding it would
	// otherwise make, silently.
	//
	// This is narrower than NeedsTypeChecker and the difference is the whole reason the field exists.
	// Measured on 2026-08-24: of the forty-one rules declaring NeedsTypeChecker, only the two that
	// call GetTypeAtLocation and read the resulting type go blind when `useState` resolves to `any`.
	// The rest ask GetSymbolAtLocation, which resolves a binding rather than a type, and a binding to
	// `declare function useState<T>(initial: T): any` resolves exactly as well as one to the real
	// declaration. Probed both ways on the same input: under the ambient shim the symbol comes back
	// named `useState` with one declaration, while the call's type comes back `any` with TypeFlagsAny.
	//
	// So a message naming every NeedsTypeChecker rule as blinded would be confidently wrong about
	// thirty-nine of them, and a message naming a hardcoded list would be confidently wrong the week a
	// fifth rule ships. `structure/react-hook-no-any-type` reads this flag off the live catalog instead,
	// which is why the flag is a declaration on the rule rather than a list somewhere else: the rule
	// that goes blind is the only thing that knows it does.
	//
	// Under-declaring costs a reader the knowledge that suppressing the tripwire disables this rule
	// too. Over-declaring names a rule that would have survived. Neither is silent, which is what
	// makes this field cheaper to get wrong than the two above it.
	ResolvesReactValueTypes bool
}

// Traits is what a rule declares about itself apart from how it runs: the facts the walk, the findings cache,
// the coverage report and the docs read without running it. Each field means what the Rule field of the same
// name means, and traitsOf copies them, so the framework reads `subject.NeedsTypeChecker` off an Entry or a
// Bound without knowing the rule's options type. TestTraitsCarryEveryRuleField holds the two field lists equal.
type Traits struct {
	Name                    string
	NoListener              NoListenerKind
	NeedsTypeChecker        bool
	ProgramReads            ProgramRead
	TypeReach               TypeReach
	ResolvesReactValueTypes bool

	// HasProgramFingerprint is whether the rule declares a ProgramFingerprint, which the findings cache reads
	// before any options are decided.
	HasProgramFingerprint bool
}

// NoOptions is the options type of a rule that takes none: `rule.Rule[rule.NoOptions]`. A config giving such
// a rule an option element is refused by name, since nothing would read it.
type NoOptions struct{}

// SkippedNotePrefix opens the key of every note Skip records, which is how --coverage tells a skip apart
// from the facts a rule counts while it judges.
const SkippedNotePrefix = "skipped: "

// Cover is another check that reports what a rule declined to, named so a run can tell whether it ran.
type Cover string

// CoverTypeCheck is the types phase: the compiler reporting the same thing itself, under an option the
// rule found on.
const CoverTypeCheck Cover = "types"

// CoveredSkipNotePrefix opens the key of every note SkipCovered records: the cover, then the reason.
const CoveredSkipNotePrefix = "skipped, covered by "

// TokenRange is a node's own text, without the trivia that precedes it.
//
// This distinction is the single sharpest edge in the fix API, and getting it wrong produces damage
// that no downstream check can catch.
//
// `node.Loc.Pos()` is the position *before* leading trivia, not the start of the node's own text.
// For `import * as X from 'fs'`, the module specifier's Loc spans `" 'fs'"`, so the space is inside
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
// Found by @system_cohere_lint_fix, running real rules through the engine on three trivia shapes
// rather than reasoning about the ranges. Upstream tsgolint gets this right via
// `utils.TrimNodeTextRange`; our port dropped the step, most likely because the signature had no
// SourceFile to trim with.
//
// This is also why vendoring tsgolint's `rule/` package is declined while its `utils/` is taken.
// Its builders are correct and do trim, but they take the SourceFile as a parameter rather than a
// receiver (`RuleFixReplace(file, node, text)`), so the call that forgets it is available. Ours is
// not. Exposing both would give a rule author two reachable spellings of one act, one of them
// unsafe, and two helpers that almost agree drift permanently.
//
// The first token's start is read with scanner.GetTokenPosOfNode, the compiler's own trivia skip. It used to
// come from scanner.GetRangeOfTokenAtPosition, which builds a scanner on every call: 252 MB and 1.5M objects of
// a cold ahra run, for a position a trivia skip gives directly (#9xfg09f). The text is read inside the compiler
// rather than with sourceFile.Text() here, since the shape-keyed rules' guard cannot tell this file from an
// imported declaration's (TestRulesClaimShapesOnlyWhereTheScanAllowsIt).
//
// GetTokenPosOfNode is a plain trivia skip, the scanner's answer, except for four shapes it reads on purpose
// another way: a missing node, whose position it returns unskipped; JSDoc and JSX text, where it stops at a
// comment; and a node inside JSDoc. Those keep the scanner. TestTokenRangeStartsWhereTheScannerDoes holds the
// two to the scanner's start on every node of the fixtures and of the four consumers, and found exactly those
// shapes when the scanner was dropped for every node (14,522 of 6,024,171).
//
// A range never starts past its end. A missing node, which is what the compiler calls any zero-width one, has
// no token of its own: an empty JSX attribute list, or a name the parser invented in recovery. Scanning from it
// finds the next token, past whatever trivia follows, so cut at the node's end that start came after the end,
// and every caller slicing the source with it panicked: no-self-compare on `<a />>0`, valid TSX (#p86b9pw). Its
// range is the empty one where it sits, which is the start the compiler's own GetTokenPosOfNode gives a missing
// node and the next token's start clamped to the node's end, both at once, since its position is its end. The
// scanner's start for the other three shapes is clamped the same way.
func TokenRange(sourceFile *ast.SourceFile, node *ast.Node) core.TextRange {
	if sourceFile == nil || node == nil {
		return node.Loc
	}
	if ast.NodeIsMissing(node) {
		return node.Loc
	}
	if ast.IsJSDocNode(node) || node.Kind == ast.KindJsxText || node.Flags&ast.NodeFlagsJSDoc != 0 {
		return core.NewTextRange(
			min(scanner.GetRangeOfTokenAtPosition(sourceFile, node.Pos()).Pos(), node.End()),
			node.End(),
		)
	}
	return core.NewTextRange(scanner.GetTokenPosOfNode(node, sourceFile, false), node.End())
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
