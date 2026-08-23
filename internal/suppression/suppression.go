// Package suppression reads the comments that tell a rule "not here", and records what they did.
//
// A linter that cannot be told "not here, and here is why" gets its rules turned off globally
// instead of locally. That is the failure this whole tool exists to prevent: the pressure on a rule
// is always toward disabling it, and a narrow, local escape hatch is exactly what keeps that rule
// enabled everywhere else. Seven findings in EnumLike.test.ts are the visible case — every one of
// those enums is deliberate, each carries a stated reason, and a tool that reports them anyway
// teaches its reader to stop reading it.
//
// Two properties matter more than the parsing, and both are about not lying:
//
// A suppression is never silent. Every finding withheld is counted, so a run that suppressed forty
// findings cannot print the same line as a run that found none. That is the coverage discipline
// applied in the other direction: coverage stops a run that checked nothing from looking clean,
// and this stops a run that hid everything from looking clean.
//
// A suppression that never fires is visible too. An unused suppression is a rule silently scoped
// off a file that no longer needs it, and it is how a codebase accumulates permanent exemptions
// nobody chose.
//
// The index works on source text and byte offsets rather than on a parsed file. A suppression is a
// fact about a line, not about a node — it may sit above a JSX attribute, inside an object literal,
// or above a statement no rule listens to — so binding it to the AST would make it invisible
// exactly where the walk did not reach. Text in, offsets out, no parse required.
package suppression

import "strings"

// Directive spellings verify honors, with identical grammar.
//
// `verify-disable` is what new code writes. `eslint-disable` keeps working because 387 of them
// already exist across the codebase this tool gates, written by the linter verify replaces, and
// rewriting that corpus is not worth what it would cost. Kirk decided this directly: both forms,
// all three shapes, no codemod, no migration, no flag day. `oxlint-disable` is honored on the same
// reasoning, since oxlint is the gate actually being replaced.
//
// Honoring the old spellings is not deference to the old tools. It is refusing to make a mechanical
// rename the price of switching linters — the same reasoning that keeps a rule enabled by giving it
// a narrow escape hatch, applied one level up to the tool itself.
//
// The grammar does not vary by spelling, so nothing downstream of parsing knows which one it read.
// That is the property that keeps this from becoming several code paths that drift.
const (
	// DirectiveVerify is the spelling new code should use.
	DirectiveVerify = "verify-disable"

	// DirectiveEslint is the spelling the existing corpus uses. Permanent, not transitional.
	DirectiveEslint = "eslint-disable"

	// DirectiveOxlint is the spelling of the gate verify replaces.
	DirectiveOxlint = "oxlint-disable"
)

// disableDirectives is every honored disable spelling.
var disableDirectives = []string{DirectiveVerify, DirectiveEslint, DirectiveOxlint}

// enableDirectives closes a block, one per disable spelling.
var enableDirectives = []string{"verify-enable", "eslint-enable", "oxlint-enable"}

// Kind is the scope a directive covers.
type Kind int

const (
	// KindNextLine silences the line after the comment. 372 of 387 directives in the corpus, so this
	// is the form to get exactly right; the others are rounding.
	KindNextLine Kind = iota

	// KindSameLine silences the line the comment sits on, as a trailing comment. 2 in the corpus.
	KindSameLine

	// KindFile silences from the comment's own line to a matching enable, or to the end of the file.
	// 4 in the corpus, all of them around a run of declarations.
	KindFile
)

// String names a kind for a failure message, so a fixture that fails says which form it got.
func (k Kind) String() string {
	switch k {
	case KindNextLine:
		return "next-line"
	case KindSameLine:
		return "same-line"
	case KindFile:
		return "file"
	}
	return "unknown"
}

// Directive is one suppression comment, resolved to the lines and rules it covers.
type Directive struct {
	Kind Kind

	// Rules are the rule names the comment named. Empty means it named none, which silences
	// everything in its scope — a blanket, and the shape most worth being able to count.
	Rules []string

	// Reason is the text after ` -- `, trimmed. Empty means the author did not say why.
	Reason string

	// Line is the zero-based line the comment starts on.
	Line int

	// StartLine and EndLine are the inclusive zero-based line span this directive covers.
	StartLine int
	EndLine   int

	// Pos and End are the comment's own byte range, so an unused directive can be reported where it
	// actually sits rather than at the finding it failed to silence.
	Pos int
	End int

	// applied counts the findings this directive actually withheld.
	applied int
}

// HasReason is whether the author said why.
func (d *Directive) HasReason() bool {
	return d.Reason != ""
}

// Covers is whether this directive applies to ruleName on a zero-based line.
//
// A directive naming no rules covers every rule; one naming rules covers only those, matched by
// plugin-qualified suffix so `nexus/consistency-no-enum` in a comment resolves against the bare
// `consistency-no-enum` a rule registers under. The corpus writes the plugin prefix and verify's
// registry does not, and reconciling that here is cheaper than making every rule carry a prefix it
// has no other use for.
func (d *Directive) Covers(ruleName string, line int) bool {
	if line < d.StartLine || line > d.EndLine {
		return false
	}
	if len(d.Rules) == 0 {
		return true
	}
	for _, named := range d.Rules {
		if matchesRuleName(named, ruleName) {
			return true
		}
	}
	return false
}

// matchesRuleName resolves a comment's rule name against a registry rule name.
//
// Exact first, then plugin-qualified. The suffix has to fall on a `/` boundary, or `no-enum` would
// match `consistency-no-enum` and a directive would silence rules its author never named.
func matchesRuleName(named string, ruleName string) bool {
	if named == ruleName {
		return true
	}
	if prefix := strings.TrimSuffix(named, ruleName); prefix != named {
		return strings.HasSuffix(prefix, "/")
	}
	return false
}

// Index is every directive in one file, ready to answer whether a finding is suppressed.
type Index struct {
	directives []*Directive
	lineOf     func(offset int) int
}

// Build scans source text once and resolves every directive in it.
func Build(sourceText string) *Index {
	lineOf := buildLineIndex(sourceText)
	found := []*Directive{}

	for _, comment := range scanComments(sourceText) {
		parsed := parseDirective(sourceText[comment.pos:comment.end])
		if parsed == nil {
			continue
		}

		parsed.Pos = comment.pos
		parsed.End = comment.end
		parsed.Line = lineOf(comment.pos)

		switch parsed.Kind {
		case KindNextLine:
			parsed.StartLine = parsed.Line + 1
			parsed.EndLine = parsed.Line + 1
		case KindSameLine:
			parsed.StartLine = parsed.Line
			parsed.EndLine = parsed.Line
		case KindFile:
			// Runs from its own line to a matching enable, or to the end of the file. It must not
			// reach backward: a file-level directive that covered lines above itself would silence
			// findings written before anyone decided to suppress them.
			parsed.StartLine = parsed.Line
			parsed.EndLine = lineOf(len(sourceText))
		}

		found = append(found, parsed)
	}

	resolveFileScopeEnds(found, sourceText, lineOf)

	return &Index{directives: found, lineOf: lineOf}
}

// resolveFileScopeEnds closes each file-scope directive at the enable comment that matches it.
//
// Matching is by rule name, because blocks overlap in the real corpus: one file disables a rule,
// enables it, and disables it again over a different span. An enable naming no rules closes every
// open block, which is what a bare enable means.
func resolveFileScopeEnds(found []*Directive, sourceText string, lineOf func(int) int) {
	hasFileScope := false
	for _, candidate := range found {
		if candidate.Kind == KindFile {
			hasFileScope = true
			break
		}
	}
	if !hasFileScope {
		return
	}

	enables := scanEnables(sourceText, lineOf)
	for _, block := range found {
		if block.Kind != KindFile {
			continue
		}
		for _, enable := range enables {
			if enable.line <= block.Line {
				continue
			}
			if len(enable.rules) == 0 || namesIntersect(enable.rules, block.Rules) {
				block.EndLine = enable.line
				break
			}
		}
	}
}

func namesIntersect(enableRules []string, blockRules []string) bool {
	// A block naming no rules is blanket, so any enable closes it.
	if len(blockRules) == 0 {
		return true
	}
	for _, left := range enableRules {
		for _, right := range blockRules {
			if left == right {
				return true
			}
		}
	}
	return false
}

// Suppresses is whether a finding for ruleName at a byte offset is silenced, and records that the
// directive responsible was used.
//
// Recording on the query rather than in a separate pass is what makes an unused directive
// detectable at all: nothing else in the system knows which findings were never reported.
func (i *Index) Suppresses(ruleName string, offset int) bool {
	if i == nil || len(i.directives) == 0 {
		return false
	}

	line := i.lineOf(offset)
	for _, candidate := range i.directives {
		if candidate.Covers(ruleName, line) {
			candidate.applied++
			return true
		}
	}
	return false
}

// Directives returns every directive found, in source order.
func (i *Index) Directives() []*Directive {
	if i == nil {
		return nil
	}
	return i.directives
}

// AppliedCount is how many findings the directive at an index actually withheld.
func (i *Index) AppliedCount(index int) int {
	if i == nil || index < 0 || index >= len(i.directives) {
		return 0
	}
	return i.directives[index].applied
}

// TotalApplied is how many findings this file's directives withheld, for the coverage line.
func (i *Index) TotalApplied() int {
	if i == nil {
		return 0
	}
	total := 0
	for _, candidate := range i.directives {
		total += candidate.applied
	}
	return total
}

// Unused returns the directives that never withheld anything.
//
// This only means what it says after a full run with every rule a directive could name. A run
// filtered to one rule shows every directive of every other rule as unused, so the caller decides
// whether to report them.
func (i *Index) Unused() []*Directive {
	if i == nil {
		return nil
	}
	unused := []*Directive{}
	for _, candidate := range i.directives {
		if candidate.applied == 0 {
			unused = append(unused, candidate)
		}
	}
	return unused
}

// WithoutReason returns the directives that never said why.
//
// Measured before it is enforced. Across the codebase verify gates, 281 of the 306 directives
// naming one of our own rules carry no reason, so a hard requirement on day one would turn working
// code red rather than teaching anyone anything. The count is the argument for the requirement;
// the requirement is a policy the caller sets.
func (i *Index) WithoutReason() []*Directive {
	if i == nil {
		return nil
	}
	bare := []*Directive{}
	for _, candidate := range i.directives {
		if !candidate.HasReason() {
			bare = append(bare, candidate)
		}
	}
	return bare
}

// LineOf is the zero-based line a byte offset falls on.
func (i *Index) LineOf(offset int) int {
	if i == nil {
		return 0
	}
	return i.lineOf(offset)
}
