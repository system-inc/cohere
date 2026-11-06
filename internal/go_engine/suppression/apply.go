package suppression

import (
	"cmp"
	"slices"
	"strings"
)

// The two rules this package reports under. Their findings are never withheld: a directive that could hide
// the finding about itself would make both rules optional.
const (
	RuleRequireIgnoreReason = "cohere-go/consistency-require-ignore-reason"
	RuleNoUnusedIgnore      = "cohere-go/consistency-no-unused-ignore"
)

// The messageIds of the directive findings.
const (
	MessageIdMissingReason = "missingReason"
	MessageIdMalformed     = "malformed"
	MessageIdUnused        = "unused"
	MessageIdUnknownRule   = "unknownRule"
)

// ruleNamespace is the prefix a directive may leave off, so `printf` names `cohere-go/printf`.
const ruleNamespace = "cohere-go/"

// The message texts live here until policy/messages holds Go, and use its `{{name}}` placeholders so the move is
// a copy.
const (
	missingReasonMessage = "This directive names a rule but not why it does not apply here, so it suppresses " +
		"nothing. Write the reason after the rule names."
	malformedMessage = "This comment reads as a cohere ignore but is not one, so it suppresses nothing. Write " +
		"`//cohere:ignore <rule>[,<rule>] <reason>` with no space after the slashes and at least one rule named."
	unusedMessage = "This directive withholds nothing from {{rule}}, so it scopes the rule off for no reason. " +
		"Remove {{rule}} from it, or the whole directive if it names no other rule."
	unknownRuleMessage = "{{rule}} is not a rule this run knows, so the directive withholds nothing for it. Name " +
		"the rule in full, as cohere-go/<name>, or by <name> alone, or remove it."
)

// Finding is one finding of the run, located well enough to say whether a directive covers it.
type Finding struct {
	Rule string
	File string

	// Line is 1-based.
	Line int
}

// DirectiveFinding is what is wrong with one directive.
type DirectiveFinding struct {
	File string

	// Line and Column are the directive comment's start, 1-based, the column in bytes.
	Line   int
	Column int

	Rule      string
	MessageId string
	Message   string
}

// Outcome is what Apply decided.
type Outcome struct {
	// Kept are the findings no directive withheld, in the order given.
	Kept []Finding

	// WithheldByRule counts the findings withheld, by rule name, so a run that hid everything never reads like
	// a run that found nothing.
	WithheldByRule map[string]int

	// DirectiveFindings are ordered by file, line and column.
	DirectiveFindings []DirectiveFinding
}

// directiveState is one directive as this run sees it.
type directiveState struct {
	file      string
	directive directive

	// resolved are the known rules the directive names, in full, each once.
	resolved []string

	// unknown are the names as written that match no known rule.
	unknown []string

	// withheld are the resolved rules the directive withheld at least one finding for.
	withheld map[string]bool
}

// Apply withholds every finding a well-formed directive covers, and reports what is wrong with the
// directives. knownRules are the full names a directive may name, and rulesRan are the ones this run ran; this
// package's own two rules are known and ran without being listed. A finding in a file with no Index is kept.
//
// A directive naming a known rule that did not run, because the settings turned it off, is neither used nor
// unused for that rule: nothing was there to withhold, and calling it dead would push its author to delete an
// exemption the next run with the rule on still needs.
//
// The unused findings mean what they say only when findings holds every ran rule's results for the files
// indexed: a run filtered to one rule would call every other rule's directives dead.
func Apply(knownRules []string, rulesRan []string, indexes []*Index, findings []Finding) Outcome {
	known := map[string]bool{RuleRequireIgnoreReason: true, RuleNoUnusedIgnore: true}
	for _, ruleName := range knownRules {
		known[ruleName] = true
	}
	ran := map[string]bool{RuleRequireIgnoreReason: true, RuleNoUnusedIgnore: true}
	for _, ruleName := range rulesRan {
		ran[ruleName] = true
	}

	// One index per file, the first: a second copy's directives would be built as states of their own, and a
	// finding credits only one of them, so the other would be reported unused for withholding what its twin did.
	var unique []*Index
	seenFiles := map[string]bool{}
	for _, index := range indexes {
		if !seenFiles[index.File] {
			seenFiles[index.File] = true
			unique = append(unique, index)
		}
	}

	statesByFile := map[string][]*directiveState{}
	for _, index := range unique {
		for _, indexed := range index.directives {
			statesByFile[index.File] = append(statesByFile[index.File], resolveDirective(index.File, indexed, known))
		}
	}

	outcome := Outcome{WithheldByRule: map[string]int{}}
	for _, finding := range findings {
		withholding := withholdingDirective(statesByFile[finding.File], finding)
		if withholding == nil {
			outcome.Kept = append(outcome.Kept, finding)
			continue
		}
		withholding.withheld[finding.Rule] = true
		outcome.WithheldByRule[finding.Rule]++
	}

	for _, index := range unique {
		for _, state := range statesByFile[index.File] {
			outcome.DirectiveFindings = append(outcome.DirectiveFindings, directiveFindings(state, ran)...)
		}
	}
	slices.SortStableFunc(outcome.DirectiveFindings, func(left DirectiveFinding, right DirectiveFinding) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
		)
	})
	return outcome
}

func resolveDirective(file string, indexed directive, known map[string]bool) *directiveState {
	state := &directiveState{file: file, directive: indexed, withheld: map[string]bool{}}
	for _, written := range indexed.rules {
		ruleName := ""
		switch {
		case known[written]:
			ruleName = written
		case known[ruleNamespace+written]:
			ruleName = ruleNamespace + written
		default:
			state.unknown = append(state.unknown, written)
			continue
		}
		if !slices.Contains(state.resolved, ruleName) {
			state.resolved = append(state.resolved, ruleName)
		}
	}
	return state
}

// withholdingDirective is the directive that withholds a finding, or nil when none does.
//
// A line's own directive is credited before a file-ignore, so a file-ignore whose every finding already has a
// line directive is reported unused: it withholds nothing those do not. Crediting by source order instead would
// make the verdict depend on where the file-ignore sits.
func withholdingDirective(states []*directiveState, finding Finding) *directiveState {
	if finding.Rule == RuleRequireIgnoreReason || finding.Rule == RuleNoUnusedIgnore {
		return nil
	}
	for _, wholeFile := range []bool{false, true} {
		for _, state := range states {
			if state.directive.wholeFile != wholeFile || state.directive.malformed || !state.directive.hasReason {
				continue
			}
			if !wholeFile && state.directive.coveredLine != finding.Line {
				continue
			}
			if slices.Contains(state.resolved, finding.Rule) {
				return state
			}
		}
	}
	return nil
}

func directiveFindings(state *directiveState, ran map[string]bool) []DirectiveFinding {
	report := func(ruleName string, messageId string, message string) DirectiveFinding {
		return DirectiveFinding{
			File:      state.file,
			Line:      state.directive.line,
			Column:    state.directive.column,
			Rule:      ruleName,
			MessageId: messageId,
			Message:   message,
		}
	}
	if state.directive.malformed {
		return []DirectiveFinding{report(RuleRequireIgnoreReason, MessageIdMalformed, malformedMessage)}
	}

	var found []DirectiveFinding
	if !state.directive.hasReason {
		found = append(found, report(RuleRequireIgnoreReason, MessageIdMissingReason, missingReasonMessage))
	}
	for _, written := range state.unknown {
		message := strings.ReplaceAll(unknownRuleMessage, "{{rule}}", written)
		found = append(found, report(RuleNoUnusedIgnore, MessageIdUnknownRule, message))
	}
	// A directive without a reason withheld nothing because it could not, and its missing reason is the
	// finding: calling each of its rules unused too would bury that under noise.
	if !state.directive.hasReason {
		return found
	}
	for _, ruleName := range state.resolved {
		if state.withheld[ruleName] || !ran[ruleName] {
			continue
		}
		message := strings.ReplaceAll(unusedMessage, "{{rule}}", ruleName)
		found = append(found, report(RuleNoUnusedIgnore, MessageIdUnused, message))
	}
	return found
}
