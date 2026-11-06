package nexus

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers nexus/consistency-no-warning-comment.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// No decoder: the rule takes no options. The words are the house's, read by every engine from
// internal/warning_comment, and an option adding or removing one would let a project's engines disagree.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: ConsistencyNoWarningComment})
}
