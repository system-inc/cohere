package suppression

import "github.com/system-inc/cohere/internal/lint/ecmascript/directives"

// Index is every directive in one file, ready to answer whether a finding is suppressed.
type Index struct {
	directives     []*Directive
	ruleReferences []RuleReference
	lineOf         func(offset int) int
}

// RuleReferences returns every rule name a disable or enable comment named, in source order.
func (suppressionIndex *Index) RuleReferences() []RuleReference {
	if suppressionIndex == nil {
		return nil
	}
	return suppressionIndex.ruleReferences
}

// Suppresses is whether a finding for ruleName at a byte offset is silenced, and records that the
// directive responsible was used.
//
// Recording on the query rather than in a separate pass is what makes an unused directive
// detectable at all: nothing else in the system knows which findings were never reported.
func (suppressionIndex *Index) Suppresses(ruleName string, offset int) bool {
	return suppressionIndex.SuppressedBy(ruleName, offset) >= 0
}

// SuppressedBy is Suppresses answering with which directive silenced the finding, by its index in Directives,
// or -1 when none did. The findings cache keeps the index, so a replay can mark the same directive applied
// without producing the finding again (MarkApplied).
func (suppressionIndex *Index) SuppressedBy(ruleName string, offset int) int {
	if suppressionIndex == nil || len(suppressionIndex.directives) == 0 {
		return -1
	}

	line := suppressionIndex.lineOf(offset)
	reportsOnDirectives := directives.IsSubject(ruleName)
	for index, candidate := range suppressionIndex.directives {
		if reportsOnDirectives && !coversAFindingAboutADirective(candidate, line) {
			continue
		}
		if candidate.Covers(ruleName, line) {
			suppressionIndex.MarkApplied(index, ruleName)
			return index
		}
	}
	return -1
}

// MarkApplied records that the directive at index withheld a finding this run did not produce, because the
// findings cache replayed the rule that would have, and reports false when no directive is at index. The index
// is SuppressedBy's from a run over the same bytes, so it names the same directive.
func (suppressionIndex *Index) MarkApplied(index int, ruleName string) bool {
	if suppressionIndex == nil || index < 0 || index >= len(suppressionIndex.directives) {
		return false
	}
	directive := suppressionIndex.directives[index]
	directive.applied++
	if directive.appliedRules == nil {
		directive.appliedRules = map[string]bool{}
	}
	directive.appliedRules[ruleName] = true
	return true
}

// Directives returns every directive found, in source order.
func (suppressionIndex *Index) Directives() []*Directive {
	if suppressionIndex == nil {
		return nil
	}
	return suppressionIndex.directives
}

// AppliedCount is how many findings the directive at an index actually withheld.
func (suppressionIndex *Index) AppliedCount(index int) int {
	if suppressionIndex == nil || index < 0 || index >= len(suppressionIndex.directives) {
		return 0
	}
	return suppressionIndex.directives[index].applied
}

// TotalApplied is how many findings this file's directives withheld, for the coverage line.
func (suppressionIndex *Index) TotalApplied() int {
	if suppressionIndex == nil {
		return 0
	}
	total := 0
	for _, candidate := range suppressionIndex.directives {
		total += candidate.applied
	}
	return total
}

// Unused returns the directives that never withheld anything.
//
// This only means what it says after a full run with every rule a directive could name. A run
// filtered to one rule shows every directive of every other rule as unused, so the caller decides
// whether to report them.
func (suppressionIndex *Index) Unused() []*Directive {
	if suppressionIndex == nil {
		return nil
	}
	unused := []*Directive{}
	for _, candidate := range suppressionIndex.directives {
		if candidate.applied == 0 {
			unused = append(unused, candidate)
		}
	}
	return unused
}

// WithoutReason returns the directives that never said why.
//
// Measured before it is enforced. Across the codebase cohere gates, 281 of the 306 directives
// naming one of our own rules carry no reason, so a hard requirement on day one would turn working
// code red rather than teaching anyone anything. The count is the argument for the requirement;
// the requirement is a policy the caller sets.
func (suppressionIndex *Index) WithoutReason() []*Directive {
	if suppressionIndex == nil {
		return nil
	}
	bare := []*Directive{}
	for _, candidate := range suppressionIndex.directives {
		if !candidate.HasReason() {
			bare = append(bare, candidate)
		}
	}
	return bare
}

// LineOf is the zero-based line a byte offset falls on.
func (suppressionIndex *Index) LineOf(offset int) int {
	if suppressionIndex == nil {
		return 0
	}
	return suppressionIndex.lineOf(offset)
}
