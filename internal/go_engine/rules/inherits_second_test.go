package rules

import (
	"path/filepath"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// inheritsSecond is a rule.Inheritance under which every rule inherits second.go, with one finding at its package
// clause, and nothing else.
type inheritsSecond struct{}

// Inherits is whether filename is second.go.
func (inheritsSecond) Inherits(_ string, filename string) bool {
	return filepath.Base(filename) == "second.go"
}

// Findings is the one finding the host made on second.go, at its package clause.
func (inheritsSecond) Findings(string, string) []rule.InheritedFinding {
	return []rule.InheritedFinding{{Line: 1, Column: 1, MessageId: "inherited", Message: "the host's finding"}}
}

// Took counts nothing: no test here reads what a build showed.
func (inheritsSecond) Took(string, string, int) {
	// The registry's tests read the findings a rule reports, never the build's count of them.
}
