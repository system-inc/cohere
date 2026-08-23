// Package differential proves that verify agrees with the gate it replaces.
//
// The thing this package exists to prevent is a vacuous pass. Two gates that both report zero —
// one because it checked and one because it ran on no files — produce identical output, and the
// diff between them is empty either way. An empty diff is therefore not evidence of agreement
// unless the harness has been shown, on the same run, to be capable of reporting a difference.
// That is why Report carries a Provenance and why the command refuses to print a verdict without
// one: see provenance.go.
//
// The second thing it exists to prevent is a true count attached to a wrong explanation. "verify
// found 128 fewer findings" is a fact, and on this codebase today it means verify has not ported
// those rules yet — not that verify disagrees with the gate about any file. Those are different
// facts with different remedies, and collapsing them into one number is how a coverage gap gets
// read as a correctness gap. Every difference this package reports is classified by which side saw
// it and whether the rule was in scope for both sides at all.
package differential

import (
	"fmt"
	"sort"
	"strings"
)

// Side names which gate produced a finding.
type Side string

const (
	// SideVerify is ours: the Go binary.
	SideVerify Side = "verify"

	// SideGate is theirs: oxlint behind RunCachedOxlint.ts.
	SideGate Side = "gate"
)

// Finding is one report from either gate, normalized to the fields the two formats share.
//
// The message is deliberately not part of identity. The two implementations word the same defect
// differently — that is expected and is not a disagreement about the code. Two findings match when
// they name the same rule at the same position in the same file, which is the claim that actually
// has to agree.
type Finding struct {
	// File is relative to the tree being linted, so the two gates' absolute-versus-relative paths
	// compare. Normalizing this is most of what parsing does.
	File string

	// Line and Column are 1-based, as both gates print them.
	Line   int
	Column int

	// Rule is the bare rule name with any plugin prefix stripped: `consistency-no-shouting`, never
	// `nexus(consistency-no-shouting)` or `consistency-no-shouting/someMessageId`. The two gates
	// decorate the name in different directions and the bare name is the only form both can reach.
	Rule string

	// Message is carried for the reader, never compared. A difference is worth nothing to a human
	// without the text that explains what was found.
	Message string
}

// Key identifies a finding for matching. File, line, column, rule — not the message.
func (finding Finding) Key() string {
	return fmt.Sprintf("%s:%d:%d:%s", finding.File, finding.Line, finding.Column, finding.Rule)
}

// String renders a finding the way both gates print, for a report a human reads next to the raw
// logs.
func (finding Finding) String() string {
	return fmt.Sprintf("%s:%d:%d [%s] %s", finding.File, finding.Line, finding.Column, finding.Rule, finding.Message)
}

// Difference is one finding that only one side reported, with enough context to classify it.
//
// Classification is the whole point. The task that commissioned this harness names four ways a
// difference turned out to be configuration rather than correctness — a rule scoped off by an
// override, a suppression the rule could not reach, options the rule never received, an exemption
// one parser grants for free. None of those are bugs in a rule, and all four look identical to a
// bare count. Kind is the harness's first pass at that triage; the human does the rest with the
// context this carries.
type Difference struct {
	Finding Finding

	// FoundBy is the side that reported it. The other side did not.
	FoundBy Side

	// Kind says what sort of difference this is, so a reader can skip the whole class of
	// differences that are expected today.
	Kind DifferenceKind
}

// DifferenceKind separates differences that mean "a rule is wrong" from differences that mean "a
// rule is not there yet".
type DifferenceKind string

const (
	// KindRuleNotPorted is a finding from a rule only one side implements at all. On this codebase
	// today this is the overwhelming majority, and it is expected: verify has 22 rules against the
	// gate's full catalog. It is a coverage fact, not a correctness fact, and it resolves by
	// porting the rule rather than by fixing one.
	KindRuleNotPorted DifferenceKind = "rule-not-ported"

	// KindDisagreement is a finding from a rule BOTH sides implement, where one side saw it and the
	// other did not. This is the category that matters. Every one of these is either a real defect
	// in the port or a configuration difference that has to be understood and written down, and
	// none of them should be dismissed by a count.
	KindDisagreement DifferenceKind = "disagreement"
)

// RuleAgreement is one row of the per-rule table.
//
// The aggregate number is not the claim being made. "verify agrees with the gate" is a claim about
// every rule separately, and a single rule silently disagreeing inside a matching total is exactly
// the failure this project exists to prevent — which is why agreement is computed per rule and the
// total is only ever a summary of the rows.
type RuleAgreement struct {
	Rule string

	// Matched is findings both sides reported at the same position.
	Matched int

	// OnlyVerify and OnlyGate are findings one side reported and the other did not.
	OnlyVerify int
	OnlyGate   int

	// BothImplement is whether this rule exists on both sides. A rule only one side has cannot
	// disagree; it can only be missing, and its rows are noise in an agreement table unless they
	// are marked.
	BothImplement bool
}

// Agrees is whether this rule reported identically on both sides.
//
// A rule only one side implements is not agreeing — it is not comparable, and saying otherwise
// would let an unported rule count toward a parity claim it has no part in.
func (agreement RuleAgreement) Agrees() bool {
	return agreement.BothImplement && agreement.OnlyVerify == 0 && agreement.OnlyGate == 0
}

// Report is the whole comparison.
type Report struct {
	VerifyFindings []Finding
	GateFindings   []Finding

	// Matched are findings both sides reported at the same file, line, column, and rule.
	Matched []Finding

	// Differences are findings only one side reported, classified.
	Differences []Difference

	// RuleAgreements is the per-rule table, sorted by rule name.
	RuleAgreements []RuleAgreement

	// Provenance records what actually ran, so an empty Differences list can be distinguished from
	// a harness that never looked. Without this a clean report is unfalsifiable.
	Provenance Provenance
}

// Disagreements returns only the differences on rules both sides implement — the ones that are
// about correctness rather than coverage.
func (report Report) Disagreements() []Difference {
	disagreements := []Difference{}
	for _, difference := range report.Differences {
		if difference.Kind == KindDisagreement {
			disagreements = append(disagreements, difference)
		}
	}
	return disagreements
}

// Compare diffs two sets of findings and builds the per-rule table.
//
// verifyRules and gateRules are the rules each side is known to implement, which is what lets a
// difference be classified as not-ported rather than as a disagreement. They are passed in rather
// than inferred from the findings, deliberately: a rule that implements correctly and finds nothing
// reports nothing, so inferring the implemented set from the output would classify every clean rule
// as unimplemented and quietly move real disagreements into the not-ported bucket where nobody
// reads them.
func Compare(verifyFindings []Finding, gateFindings []Finding, verifyRules map[string]bool, gateRules map[string]bool) Report {
	report := Report{
		VerifyFindings: verifyFindings,
		GateFindings:   gateFindings,
		Matched:        []Finding{},
		Differences:    []Difference{},
	}

	// Findings are matched by key, and duplicates at one key are counted rather than collapsed. A
	// rule that reports the same position twice on one side and once on the other is a real
	// difference, and a set would hide it.
	verifyByKey := groupByKey(verifyFindings)
	gateByKey := groupByKey(gateFindings)

	for key, ours := range verifyByKey {
		theirs := gateByKey[key]
		shared := min(len(ours), len(theirs))
		report.Matched = append(report.Matched, ours[:shared]...)
		for _, finding := range ours[shared:] {
			report.Differences = append(report.Differences, Difference{
				Finding: finding,
				FoundBy: SideVerify,
				Kind:    classify(finding.Rule, verifyRules, gateRules),
			})
		}
	}

	for key, theirs := range gateByKey {
		ours := verifyByKey[key]
		shared := min(len(ours), len(theirs))
		for _, finding := range theirs[shared:] {
			report.Differences = append(report.Differences, Difference{
				Finding: finding,
				FoundBy: SideGate,
				Kind:    classify(finding.Rule, verifyRules, gateRules),
			})
		}
	}

	sortFindings(report.Matched)
	sortDifferences(report.Differences)
	report.RuleAgreements = buildRuleAgreements(report, verifyRules, gateRules)

	return report
}

// classify decides whether a one-sided finding is a coverage gap or a real disagreement.
func classify(ruleName string, verifyRules map[string]bool, gateRules map[string]bool) DifferenceKind {
	if verifyRules[ruleName] && gateRules[ruleName] {
		return KindDisagreement
	}
	return KindRuleNotPorted
}

// buildRuleAgreements assembles one row per rule that either side reported or implements.
func buildRuleAgreements(report Report, verifyRules map[string]bool, gateRules map[string]bool) []RuleAgreement {
	rows := map[string]*RuleAgreement{}

	rowFor := func(ruleName string) *RuleAgreement {
		if existing, found := rows[ruleName]; found {
			return existing
		}
		row := &RuleAgreement{
			Rule:          ruleName,
			BothImplement: verifyRules[ruleName] && gateRules[ruleName],
		}
		rows[ruleName] = row
		return row
	}

	// Every rule either side implements gets a row even with no findings, because "this rule ran
	// on both sides and found nothing on either" is the most common form of agreement and omitting
	// it would make the table describe only the noisy rules.
	for ruleName := range verifyRules {
		rowFor(ruleName)
	}
	for ruleName := range gateRules {
		rowFor(ruleName)
	}

	for _, finding := range report.Matched {
		rowFor(finding.Rule).Matched++
	}
	for _, difference := range report.Differences {
		row := rowFor(difference.Finding.Rule)
		if difference.FoundBy == SideVerify {
			row.OnlyVerify++
		} else {
			row.OnlyGate++
		}
	}

	agreements := make([]RuleAgreement, 0, len(rows))
	for _, row := range rows {
		agreements = append(agreements, *row)
	}
	sort.Slice(agreements, func(first int, second int) bool {
		return agreements[first].Rule < agreements[second].Rule
	})
	return agreements
}

func groupByKey(findings []Finding) map[string][]Finding {
	grouped := map[string][]Finding{}
	for _, finding := range findings {
		grouped[finding.Key()] = append(grouped[finding.Key()], finding)
	}
	return grouped
}

func sortFindings(findings []Finding) {
	sort.Slice(findings, func(first int, second int) bool {
		return findings[first].Key() < findings[second].Key()
	})
}

func sortDifferences(differences []Difference) {
	sort.Slice(differences, func(first int, second int) bool {
		left, right := differences[first], differences[second]
		if left.Kind != right.Kind {
			// Disagreements sort first because they are the ones a reader must not scroll past.
			return left.Kind == KindDisagreement
		}
		if left.Finding.Key() != right.Finding.Key() {
			return left.Finding.Key() < right.Finding.Key()
		}
		return left.FoundBy < right.FoundBy
	})
}

// RuleNamesFromKeys is a small helper for building the implemented-rule sets from a map.
func RuleNamesFromKeys(rules map[string]bool) []string {
	names := make([]string, 0, len(rules))
	for name := range rules {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// NormalizeRuleName strips the decoration each gate puts around a rule name.
//
// The two formats decorate in opposite directions. The gate prints `structure(rule-name)` or
// `nexus(rule-name)`, putting the plugin outside. verify prints `rule-name/messageId`, putting the
// message identifier after. Neither decoration is part of the rule's identity, and comparing
// decorated names would report every single rule as a disagreement.
func NormalizeRuleName(raw string) string {
	name := strings.TrimSpace(raw)

	// `plugin(rule-name)` — take what is inside the parentheses.
	if open := strings.IndexByte(name, '('); open >= 0 && strings.HasSuffix(name, ")") {
		name = name[open+1 : len(name)-1]
	}

	// `rule-name/messageId` — take what is before the slash. Rule names in both catalogs are
	// hyphenated and never contain a slash, so the first slash is always the message boundary.
	if slash := strings.IndexByte(name, '/'); slash >= 0 {
		name = name[:slash]
	}

	return strings.TrimSpace(name)
}
