// Package suppression reads the comments that tell a rule "not here", and records what they did.
//
// A linter that cannot be told "not here, and here is why" gets its rules turned off globally
// instead of locally. That is the failure this whole tool exists to prevent: the pressure on a rule
// is always toward disabling it, and a narrow, local escape hatch is exactly what keeps that rule
// enabled everywhere else. Seven findings in EnumLike.test.ts are the visible case: every one of
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
// fact about a line, not about a node (it may sit above a JSX attribute, inside an object literal,
// or above a statement no rule listens to), so binding it to the AST would make it invisible
// exactly where the walk did not reach. Text in, offsets out, no parse required.
package suppression

import (
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/directives"
)

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

// RuleReference is one rule name a disable or enable comment names, with the comment's byte range.
//
// Enables are included because ESLint checks them too: `/* eslint-enable no-such-rule */` reports
// the unknown name exactly as a disable does, measured on 10.8.1.
type RuleReference struct {
	Name     string
	Position int
	End      int
}

// Build scans source text once and resolves every directive in it.
func Build(sourceText string) *Index {
	lineOf := buildLineIndex(sourceText)
	found := []*Directive{}
	references := []RuleReference{}

	for _, comment := range scanComments(sourceText) {
		commentText := sourceText[comment.position:comment.end]
		parsed := parseDirective(commentText)
		if parsed == nil {
			if names, isEnable := directives.ParseEnable(commentText); isEnable {
				for _, name := range names {
					references = append(
						references,
						RuleReference{Name: name, Position: comment.position, End: comment.end},
					)
				}
			}
			continue
		}
		for _, name := range parsed.Rules {
			references = append(references, RuleReference{Name: name, Position: comment.position, End: comment.end})
		}

		parsed.Position = comment.position
		parsed.End = comment.end
		parsed.Line = lineOf(comment.position)

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

	return &Index{directives: found, ruleReferences: references, lineOf: lineOf}
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
