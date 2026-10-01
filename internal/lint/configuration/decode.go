package configuration

import (
	"encoding/json"
	"fmt"
	"strings"
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
// Used as `configuration.DecodeInto[BoundaryNoProjectImportOptions]()`, it decodes the config's JSON into
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
// Exactly one of Decode and DecodeList is set, and which one is the rule's arity. Decode serves a
// rule that takes ONE option element and is handed that element alone. DecodeList serves a rule
// whose upstream schema takes more than one, and is handed every element as a JSON array, which is
// upstream's own `context.options` with the severity removed. The arity has to live here, beside
// the decoder, because this is the only layer that sees both the elements and the rule.
//
// Required is the load-bearing field, and it is the whole lesson of the inert-rule defect. A rule
// that needs an option and does not get one must fail loudly rather than decline quietly: declining
// looks identical to a rule with nothing to report, and that ambiguity hid a dead rule for months.
// A rule whose options only tune it, rather than enable it, sets Required false and runs on
// defaults.
type RuleOptions struct {
	Decode     Decoder
	DecodeList Decoder
	Required   bool
}

// OptionsRegistry maps rule names to their decoders.
type OptionsRegistry map[string]RuleOptions

// Decode produces the typed options for one rule from every option element the config wrote, or
// reports why it cannot.
//
// The outcomes are deliberately distinct rather than collapsed into "nil options":
//
//	no decoder, no elements        the rule takes no options; nil is correct and expected
//	no decoder, any element        an error naming the rule and each element
//	Decode, two or more elements   an error naming the rule and every element after the first
//	DecodeList, any count          the rule's own decoder reads the whole list, upstream's shape
//	decoder, elements present      the typed value
//	decoder, elements absent       an error if the rule requires options, nil if they merely tune it
//
// Collapsing the last case into nil is precisely how a required option goes missing without anyone
// finding out.
//
// # No element is dropped
//
// Every element the config wrote is either handed to the rule's decoder or refused here by name.
// The previous shape kept `tuple[1]`, so `["error", {"object": true},
// {"enforceForRenamedProperties": true}]` loaded clean, ran, and reported 0 on source the second
// element exists to flag, while the one-object spelling reported 1. A refusal costs one edit to the
// config; a silent drop costs an afternoon of somebody wondering why their option does nothing.
func (o OptionsRegistry) Decode(ruleName string, elements []json.RawMessage) (any, error) {
	declared, hasDeclared := o[ruleName]
	if !hasDeclared || (declared.Decode == nil && declared.DecodeList == nil) {
		if len(elements) > 0 {
			return nil, fmt.Errorf(
				"rule %s takes no options, and the config gives it %s, which it would never read. "+
					"Remove the options or write the bare severity", ruleName, describeElements(elements, 0))
		}
		return nil, nil
	}

	var raw json.RawMessage
	decode := declared.Decode
	switch {
	case declared.DecodeList != nil:
		decode = declared.DecodeList
		if len(elements) > 0 {
			list, err := json.Marshal(elements)
			if err != nil {
				return nil, fmt.Errorf("rule %s: re-encoding its option elements: %w", ruleName, err)
			}
			raw = list
		}
	case len(elements) > 1:
		return nil, fmt.Errorf(
			"rule %s takes one option element, and the config gives it %d: %s would never be read, "+
				"so the entry is refused rather than half applied",
			ruleName, len(elements), describeElements(elements, 1))
	case len(elements) == 1:
		raw = elements[0]
	}

	decoded, err := decode(raw)
	if err != nil {
		if len(raw) == 0 && !declared.Required {
			// The rule tunes on options it did not get. Defaults are its own business.
			return nil, nil
		}
		return nil, fmt.Errorf("rule %s: %w", ruleName, err)
	}
	return decoded, nil
}

// describeElements names the option elements from index `from` on, numbered the way a config
// author counts them (the first element after the severity is element 1), so an error says which
// element it means and what it held.
func describeElements(elements []json.RawMessage, from int) string {
	described := make([]string, 0, len(elements)-from)
	for index := from; index < len(elements); index++ {
		described = append(described, fmt.Sprintf("element %d %s", index+1, truncate(string(elements[index]))))
	}
	return strings.Join(described, ", ")
}
