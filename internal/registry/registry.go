// Package registry is the list of every rule verify knows.
//
// Rules are compiled in rather than loaded, which is what makes them free to run: a rule walks the
// same AST the parser already built, in the same address space, so the five hundredth rule costs
// what the hundredth does. It also means a rule present in the source cannot be silently absent at
// runtime, which a configuration-driven plugin system cannot promise. During the migration this
// tool replaces, three configurations ran successfully having loaded zero plugins.
//
// The catalog is assembled rather than written down. Each rule package registers its own rules in
// its own `init`, and this package's only job is to import every one of them and read the result.
// Before that, adding a rule meant appending a line to a sorted list here and often a second entry
// to an options map, so every port edited this file and two ports landing at once conflicted on a
// file neither was really changing.
//
// The blank imports below are therefore load-bearing rather than incidental. A rule package that
// nothing imports contributes nothing, so dropping one of these lines does not fail to compile, it
// makes those rules quietly stop existing. `TestParityAgainstInventory` is what turns that into a
// visible number, and it is the reason this indirection is safe to rely on.
package registry

import (
	"encoding/json"

	"github.com/system-inc/verify/internal/configuration"
	"github.com/system-inc/verify/internal/rule"

	_ "github.com/system-inc/verify/internal/rules/core"
	_ "github.com/system-inc/verify/internal/rules/next"
	_ "github.com/system-inc/verify/internal/rules/nexus"
	_ "github.com/system-inc/verify/internal/rules/react"
	_ "github.com/system-inc/verify/internal/rules/structure"
	_ "github.com/system-inc/verify/internal/rules/tailwind"
	_ "github.com/system-inc/verify/internal/rules/typescript"
)

// All returns every rule, in a stable order.
//
// Order is stable so that two runs over the same tree report findings in the same sequence, which
// is what makes a diff between them meaningful. `rule.Registered` sorts by name, so the order no
// longer depends on how a human kept a list tidy, nor on the order Go happens to run `init`.
func All() []rule.Rule {
	registrations := rule.Registered()
	rules := make([]rule.Rule, 0, len(registrations))
	for _, registration := range registrations {
		rules = append(rules, registration.Rule)
	}
	return rules
}

// Count is how many rules exist, for the coverage line.
func Count() int {
	return len(All())
}

// Options says how to decode each rule's configuration, and which rules cannot run without it.
//
// A rule declares its own options struct and only its own package knows that type, while the config
// layer holds JSON. A rule's registration carries the decoder across that gap, and this function
// only reshapes what the packages already declared.
//
// Required is the field that matters. `boundary-no-project-import` was enabled and inert for months
// under the gate verify replaces: it declines every file when LibraryDirectory is empty, which is
// correct behavior for a misconfigured guard and indistinguishable from a rule with nothing to
// report. A liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that ever
// caught it. Marking it required turns that silence into a failure.
//
// A rule that registers no decoder takes no options, which is the common case.
func Options() configuration.OptionsRegistry {
	options := configuration.OptionsRegistry{}
	for _, registration := range rule.Registered() {
		if registration.Decode == nil {
			continue
		}
		decode := registration.Decode
		options[registration.Rule.Name] = configuration.RuleOptions{
			Decode:   func(raw json.RawMessage) (any, error) { return decode(raw) },
			Required: registration.RequiresOptions,
		}
	}
	return options
}
