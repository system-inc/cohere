package nexus

import "github.com/system-inc/cohere/internal/lint/rule"

// init registers nexus/consistency-require-identifier-casing.
//
// A file of its own rather than a line in the package's shared `register.go`, so two authors landing
// at once touch nothing in common.
//
// No decoder: the rule takes no options. Values are lowerCamelCase and types UpperCamelCase in every
// engine, and an option bending that would let a project's engines disagree.
func init() {
	rule.Register(rule.Registration[rule.NoOptions]{Rule: ConsistencyRequireIdentifierCasing})
}
