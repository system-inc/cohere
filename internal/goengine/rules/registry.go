// Package rules is the list of rules the Go engine runs: one place that names every rule, so the engine,
// `--rules` and the docs all read the same list.
package rules

import "github.com/system-inc/cohere/internal/goengine/rule"

// All is every rule the Go engine knows, in the order `--rules` lists them before sorting.
//
// It is empty until the rules land: vet's suite, the house rules and their suppression rules each arrive
// in a unit of their own, and the registry is the last to change, so no unit edits another's list.
func All() []rule.Rule {
	return []rule.Rule{}
}
