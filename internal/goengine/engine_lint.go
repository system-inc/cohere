package goengine

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/system-inc/cohere/internal/goengine/check"
	"github.com/system-inc/cohere/internal/goengine/contract"
	"github.com/system-inc/cohere/internal/goengine/load"
	"github.com/system-inc/cohere/internal/goengine/rule"
	"github.com/system-inc/cohere/internal/goengine/suppression"
)

// lintRecord is the contract's lint record with one additive field, withheldBySuppression: the findings each
// rule's //cohere:ignore comments withheld, so a run that hid everything never reads like one that found
// nothing. The front door ignores the field until it reads it, as the contract asks of a field it does not list.
// Embedding keeps the record's kind the contract's own, so the writer still knows it as a lint record.
type lintRecord struct {
	contract.LintRecord
	WithheldBySuppression map[string]int `json:"withheldBySuppression"`
}

// lintSuppressionRules are suppression's two rules, which run in the lint phase on the directives themselves.
var lintSuppressionRules = []string{suppression.RuleNoUnusedIgnore, suppression.RuleRequireIgnoreReason}

// runLint is the lint phase: the rules the settings leave on, over the analyzed packages, after a load with no
// type error. Findings outside the scope or in an excluded file are dropped, then the //cohere:ignore comments
// in the files in scope withhold what they cover and report what is wrong with them.
func (engine *pipeline) runLint(loaded *load.Loaded, loadError error) {
	if !engine.request.requests(contract.PhaseLint) {
		engine.phase(contract.PhaseLint, contract.OutcomeSkipped, 0, detailNotRequested)
		return
	}
	if loadError != nil {
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0, "the load failed: "+oneLine(loadError))
		return
	}
	if count := len(loaded.Diagnostics); count > 0 {
		detail := fmt.Sprintf("types bailed: %d type errors, so lint findings against wrong semantics would be noise",
			count)
		if !engine.request.requests(contract.PhaseTypes) {
			detail = fmt.Sprintf("the load found %d type errors, so lint findings against wrong semantics would be "+
				"noise; run with --types to see them", count)
		}
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0, detail)
		return
	}

	started := time.Now()
	var enabled []rule.Rule
	var turnedOff []string
	for _, registeredRule := range engine.registered {
		if _, on := engine.severityOf(registeredRule.Name); on {
			enabled = append(enabled, registeredRule)
			continue
		}
		turnedOff = append(turnedOff, registeredRule.Name)
	}
	result, checkError := check.Run(loaded.Packages, enabled, check.Options{
		SingleThreaded: engine.request.switches["single-threaded"],
	})
	if checkError != nil {
		engine.note("the rules could not run: %v", checkError)
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0, "the rules could not run: "+oneLine(checkError))
		return
	}

	inScope := make([]check.Finding, 0, len(result.Findings))
	for _, found := range result.Findings {
		if engine.described.inScopeSet[found.File] {
			inScope = append(inScope, found)
		}
	}
	ran := make([]string, 0, len(enabled))
	for _, enabledRule := range enabled {
		ran = append(ran, enabledRule.Name)
	}
	outcome := engine.suppress(ran, inScope)

	findings := make([]contract.FindingRecord, 0, len(outcome.kept)+len(outcome.directiveFindings))
	for _, kept := range outcome.kept {
		severity, _ := engine.severityOf(kept.Rule)
		findings = append(findings, ruleFinding(kept, severity))
	}
	for _, directiveFinding := range outcome.directiveFindings {
		severity, on := engine.severityOf(directiveFinding.Rule)
		if !on {
			continue
		}
		findings = append(findings, contract.FindingRecord{
			Source:      contract.SourceRule,
			File:        directiveFinding.File,
			Line:        directiveFinding.Line,
			Column:      directiveFinding.Column,
			Severity:    severity,
			Rule:        directiveFinding.Rule,
			MessageId:   directiveFinding.MessageId,
			Message:     directiveFinding.Message,
			Fixes:       []contract.Edit{},
			Suggestions: []contract.Suggestion{},
		})
	}
	slices.SortStableFunc(findings, func(left contract.FindingRecord, right contract.FindingRecord) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Rule, right.Rule),
		)
	})
	for _, finding := range findings {
		engine.write(finding)
	}

	record := engine.lintCoverage(result, findings, turnedOff)
	record.WithheldBySuppression = outcome.withheldByRule
	if len(record.Crashes) > 0 {
		engine.incomplete = true
	}
	record.ElapsedMilliseconds = int(time.Since(started).Milliseconds())
	engine.write(record)
	engine.phase(contract.PhaseLint, contract.OutcomeRan, time.Since(started), "")
}

// suppressedFindings is what the //cohere:ignore comments left of the lint phase's findings.
type suppressedFindings struct {
	kept              []check.Finding
	directiveFindings []suppression.DirectiveFinding
	withheldByRule    map[string]int
}

// suppress applies the //cohere:ignore comments of every readable file in scope, read from the contents the
// project description read, to the findings in scope. ran are the analyzer rules that ran: a directive naming a
// rule the settings turned off is neither used nor unused.
func (engine *pipeline) suppress(ran []string, inScope []check.Finding) suppressedFindings {
	indexes := make([]*suppression.Index, 0, len(engine.described.contents))
	for _, file := range engine.described.readable() {
		indexes = append(indexes, suppression.Build(file, engine.described.contents[file]))
	}
	located := make([]suppression.Finding, 0, len(inScope))
	for _, found := range inScope {
		located = append(located, suppression.Finding{Rule: found.Rule, File: found.File, Line: found.Line})
	}
	applied := suppression.Apply(knownRuleNames(engine.registered), ran, indexes, located)

	// Kept holds the located findings no directive covered, in the order given. Whether one is withheld depends
	// only on its rule, file and line, so two findings alike in those are kept or withheld together, and walking
	// both lists in step pairs each kept one with the finding it came from.
	kept := make([]check.Finding, 0, len(applied.Kept))
	next := 0
	for index, found := range inScope {
		if next < len(applied.Kept) && applied.Kept[next] == located[index] {
			kept = append(kept, found)
			next++
		}
	}
	return suppressedFindings{
		kept: kept, directiveFindings: applied.DirectiveFindings, withheldByRule: applied.WithheldByRule,
	}
}

// ruleFinding is one analyzer finding as a record. A suggestion is offered only when every edit is in the
// finding's own file, since an edit's offsets are into the file the record names.
func ruleFinding(found check.Finding, severity contract.Severity) contract.FindingRecord {
	suggestions := []contract.Suggestion{}
	for _, suggestion := range found.Suggestions {
		edits := make([]contract.Edit, 0, len(suggestion.Edits))
		for _, edit := range suggestion.Edits {
			if edit.File != found.File {
				edits = nil
				break
			}
			edits = append(edits, contract.Edit{Start: edit.Start, End: edit.End, Text: edit.Text})
		}
		if edits != nil {
			suggestions = append(suggestions, contract.Suggestion{Message: suggestion.Message, Fixes: edits})
		}
	}
	return contract.FindingRecord{
		Source:      contract.SourceRule,
		File:        found.File,
		Line:        found.Line,
		Column:      found.Column,
		EndLine:     found.EndLine,
		EndColumn:   found.EndColumn,
		Severity:    severity,
		Rule:        found.Rule,
		MessageId:   found.MessageId,
		Message:     found.Message,
		Fixes:       []contract.Edit{},
		Suggestions: suggestions,
	}
}

// lintCoverage is the lint record's counts. The rules are the analyzers and suppression's two, never the format
// rule, which belongs to the fix phase. Of them, the ones the settings turned off listened to no file and are
// named silent, off by the config for every file the rules walked; the ones that ran and wrote no finding,
// whether they had nothing to say or everything they said was outside the scope or withheld, are watched and
// quiet; the rest found something. A rule that crashed on a package is counted by what it wrote on the others,
// and the crash itself is in crashes, which makes the run incomplete.
func (engine *pipeline) lintCoverage(
	result *check.Result, written []contract.FindingRecord, turnedOff []string,
) lintRecord {
	writtenByRule := map[string]int{}
	for _, finding := range written {
		writtenByRule[finding.Rule]++
	}
	ranNames := make([]string, 0, len(result.FindingsByRule)+len(lintSuppressionRules))
	for name := range result.FindingsByRule {
		ranNames = append(ranNames, name)
	}
	// Never nil, so a run with nothing off writes `[]` rather than `null`.
	silent := append([]string{}, turnedOff...)
	for _, name := range lintSuppressionRules {
		if _, on := engine.severityOf(name); on {
			ranNames = append(ranNames, name)
			continue
		}
		silent = append(silent, name)
	}
	slices.Sort(silent)

	record := lintRecord{LintRecord: contract.LintRecord{
		Findings:           len(written),
		RulesRun:           len(ranNames) + len(silent),
		FilesWalked:        result.FilesWalked,
		NodesVisited:       result.NodesVisited,
		RulesSilent:        silent,
		Crashes:            []contract.Crash{},
		RulesScopedOff:     map[string]int{},
		RulesNotConfigured: []string{},
		ConfigNote:         engine.settings.Note,
	}}
	for _, name := range ranNames {
		if writtenByRule[name] == 0 {
			record.RulesWatchedAndQuiet++
		}
	}
	if result.FilesWalked > 0 {
		for _, name := range silent {
			record.RulesScopedOff[name] = result.FilesWalked
		}
	}
	for _, crash := range result.Crashes {
		record.Crashes = append(record.Crashes, contract.Crash{File: crash.File, Error: crash.Error})
	}
	return record
}
