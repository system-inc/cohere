package check

import (
	"slices"
	"sync"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// HostInheritance is an extra build's rule.Inheritance (#y6q1x2j): the host's findings of each rule the run enables,
// which only a syntax-only rule asks for (rule.InheritedFiles), on each file the host compiled into a package whose
// kept entry holds the rule's analysis, the rule having finished there. A file the host never compiled, such as one
// only windows builds, and a file whose every holder lost the rule to a crash, which leaves no entry, are never
// inherited, so the build runs the rule on them itself. Of the host's packages holding a file, p and `p [p.test]`, the
// lowest ID holding the rule's analysis is the one inherited from, and since a syntax-only rule's findings on a file
// are the file's alone, either would give the same.
//
// It counts what the build took, each rule's findings on each file once however many of the build's packages and loads
// took them (Counts). Every method may be called from several goroutines at once.
type HostInheritance struct {
	host *HostRun
	// analyzerOf is each enabled rule's analyzer by the rule's name, in whose slot the host's entries hold its
	// findings.
	analyzerOf map[string]*analysis.Analyzer
	// fromHighest picks the highest ID among a file's holders rather than the lowest, for a test that shows either
	// gives the same findings.
	fromHighest bool

	lock  sync.Mutex
	taken map[inheritedKey]int
}

// inheritedKey is one rule's findings on one file, as the build took them.
type inheritedKey struct {
	ruleName string
	filename string
}

// Inherits is whether the rule named ruleName takes the host's findings on filename (rule.Inheritance). A file one of
// whose findings suggests an edit to another file is never inherited, since the edit's file may not be the build's.
func (inheritance *HostInheritance) Inherits(ruleName string, filename string) bool {
	_, held := inheritance.held(ruleName, filename)
	return held
}

// Findings is the host's findings of the rule named ruleName on filename, before any dedupe or withholding
// (rule.Inheritance), counted as the build's take of them.
func (inheritance *HostInheritance) Findings(ruleName string, filename string) []rule.InheritedFinding {
	findings, held := inheritance.held(ruleName, filename)
	if !held {
		return nil
	}
	inherited := make([]rule.InheritedFinding, 0, len(findings))
	for _, finding := range findings {
		inherited = append(inherited, inheritedFinding(finding))
	}
	inheritance.lock.Lock()
	inheritance.taken[inheritedKey{ruleName: ruleName, filename: filename}] = len(findings)
	inheritance.lock.Unlock()
	return inherited
}

// Counts is how many distinct files the build took at least one rule's findings on, and how many findings it took,
// each rule's on each file once.
func (inheritance *HostInheritance) Counts() (files int, findings int) {
	if inheritance == nil {
		return 0, 0
	}
	inheritance.lock.Lock()
	defer inheritance.lock.Unlock()
	filenames := map[string]bool{}
	for key, count := range inheritance.taken {
		filenames[key.filename] = true
		findings += count
	}
	return len(filenames), findings
}

// held is the host's findings of the rule named ruleName on filename, from the first of the file's holders, lowest ID
// first, whose entry holds the rule's analysis, and false when none does, the rule is not enabled, or a finding edits
// another file.
func (inheritance *HostInheritance) held(ruleName string, filename string) ([]Finding, bool) {
	analyzer, marked := inheritance.analyzerOf[ruleName]
	if !marked {
		return nil, false
	}
	holders := inheritance.host.holdersOf(filename)
	if inheritance.fromHighest {
		holders = slices.Clone(holders)
		slices.Reverse(holders)
	}
	for _, identifier := range holders {
		kept := inheritance.host.entries[identifier]
		if kept == nil {
			continue
		}
		decoded, decodeError := kept.decoded()
		if decodeError != nil {
			continue
		}
		analyzed, holds := decoded.analyses[analyzer]
		if !holds {
			continue
		}
		var findings []Finding
		for _, finding := range analyzed.findings {
			if finding.File != filename {
				continue
			}
			for _, suggestion := range finding.Suggestions {
				for _, edit := range suggestion.Edits {
					if edit.File != filename {
						return nil, false
					}
				}
			}
			findings = append(findings, finding)
		}
		return findings, true
	}
	return nil, false
}

// inheritedFinding is finding as rule holds an inherited one.
func inheritedFinding(finding Finding) rule.InheritedFinding {
	inherited := rule.InheritedFinding{
		Line:      finding.Line,
		Column:    finding.Column,
		EndLine:   finding.EndLine,
		EndColumn: finding.EndColumn,
		MessageId: finding.MessageId,
		Message:   finding.Message,
	}
	for _, suggestion := range finding.Suggestions {
		inheritedSuggestion := rule.InheritedSuggestion{Message: suggestion.Message}
		for _, edit := range suggestion.Edits {
			inheritedSuggestion.Edits = append(inheritedSuggestion.Edits,
				rule.InheritedEdit{File: edit.File, Start: edit.Start, End: edit.End, Text: edit.Text})
		}
		inherited.Suggestions = append(inherited.Suggestions, inheritedSuggestion)
	}
	return inherited
}
