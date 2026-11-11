package nexus

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers nexus/correctness-require-drained-child-output.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// No decoder: the rule takes no options. Its producer is `spawn`, the one `node:child_process`
// function that hands back pipes nothing reads, and an option adding to it could name `execFile`,
// which reads both streams itself, and make every wait on one a false finding.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: CorrectnessRequireDrainedChildOutput})
}
