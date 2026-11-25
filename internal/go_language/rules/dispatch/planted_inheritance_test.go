package dispatch

import (
	"path/filepath"
	"sync"

	"github.com/system-inc/cohere/internal/go_language/rule"
)

// plantedInheritance is a rule.Inheritance that hands one rule the findings planted on each file by the file's base
// name, and inherits nothing else. took, when not nil, records what the rule reported of them.
type plantedInheritance struct {
	ruleName string
	planted  map[string][]rule.InheritedFinding
	took     *tookFindings
}

// tookFindings is what a rule reported of the planted findings, by the base name of the file they are on.
type tookFindings struct {
	lock   sync.Mutex
	byFile map[string]int
}

// Inherits is whether ruleName is the planted rule and filename's base name holds planted findings.
func (inheritance plantedInheritance) Inherits(ruleName string, filename string) bool {
	_, held := inheritance.planted[filepath.Base(filename)]
	return ruleName == inheritance.ruleName && held
}

// Findings is the findings planted on filename's base name, each edit naming filename itself.
func (inheritance plantedInheritance) Findings(_ string, filename string) []rule.InheritedFinding {
	findings := []rule.InheritedFinding{}
	for _, finding := range inheritance.planted[filepath.Base(filename)] {
		named := finding
		named.Suggestions = nil
		for _, suggestion := range finding.Suggestions {
			edits := []rule.InheritedEdit{}
			for _, edit := range suggestion.Edits {
				edit.File = filename
				edits = append(edits, edit)
			}
			named.Suggestions = append(named.Suggestions, rule.InheritedSuggestion{Message: suggestion.Message,
				Edits: edits})
		}
		findings = append(findings, named)
	}
	return findings
}

// Took records findings, what the rule reported of the planted findings on filename, under its base name.
func (inheritance plantedInheritance) Took(_ string, filename string, findings int) {
	if inheritance.took == nil {
		return
	}
	inheritance.took.lock.Lock()
	defer inheritance.took.lock.Unlock()
	inheritance.took.byFile[filepath.Base(filename)] = findings
}
