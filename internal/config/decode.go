package config

import (
	"encoding/json"
	"fmt"
)

// Decoder turns a rule's raw JSON options into the typed value that rule expects.
//
// A rule declares its own options struct, and only the rule's own package knows that type. The
// config layer holds bytes. This function type is the seam: the registry pairs a rule with a
// decoder that knows how to produce its struct, and neither side has to import the other.
//
// It returns an error rather than a zero value, because a rule that silently receives a zero
// options struct is the exact defect this exists to fix. `boundary-no-project-import` was enabled
// and inert for months under the previous gate, declining every file because its LibraryDirectory
// was empty, and a liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that
// ever noticed.
type Decoder func(raw json.RawMessage) (any, error)

// DecodeInto builds a Decoder for any options struct.
//
// Used as `config.DecodeInto[BoundaryNoProjectImportOptions]()`, it decodes the config's JSON into
// that type and reports what went wrong if it cannot, naming the type so the message says which
// rule's options failed rather than only that some JSON did not parse.
func DecodeInto[Options any]() Decoder {
	return func(raw json.RawMessage) (any, error) {
		var decoded Options
		if len(raw) == 0 {
			return decoded, fmt.Errorf("no options were configured")
		}
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return decoded, fmt.Errorf("decoding %T: %w", decoded, err)
		}
		return decoded, nil
	}
}

// RuleOptions pairs a rule name with how to decode its options and whether it can run without them.
//
// Required is the load-bearing field, and it is the whole lesson of the inert-rule defect. A rule
// that needs an option and does not get one must fail loudly rather than decline quietly: declining
// looks identical to a rule with nothing to report, and that ambiguity hid a dead rule for months.
// A rule whose options only tune it, rather than enable it, sets Required false and runs on
// defaults.
type RuleOptions struct {
	Decode   Decoder
	Required bool
}

// OptionsRegistry maps rule names to their decoders.
type OptionsRegistry map[string]RuleOptions

// Decode produces the typed options for one rule, or reports why it cannot.
//
// The three outcomes are deliberately distinct rather than collapsed into "nil options":
//
//	no decoder registered   the rule takes no options; nil is correct and expected
//	decoder, raw present    the typed value
//	decoder, raw absent     an error if the rule requires options, nil if they merely tune it
//
// Collapsing the last case into nil is precisely how a required option goes missing without anyone
// finding out.
func (o OptionsRegistry) Decode(ruleName string, raw json.RawMessage) (any, error) {
	declared, hasDeclared := o[ruleName]
	if !hasDeclared {
		return nil, nil
	}

	decoded, err := declared.Decode(raw)
	if err != nil {
		if len(raw) == 0 && !declared.Required {
			// The rule tunes on options it did not get. Defaults are its own business.
			return nil, nil
		}
		return nil, fmt.Errorf("rule %s: %w", ruleName, err)
	}
	return decoded, nil
}
