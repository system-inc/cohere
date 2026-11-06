package configuration

import (
	"encoding/json"
	"fmt"

	"github.com/system-inc/cohere/internal/lint/optionschema"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// OptionsRegistry maps rule names to their decoders.
type OptionsRegistry map[string]RuleOptions

// Decode binds one rule to the options decoded from every option element the config wrote, or
// reports why it cannot.
//
// The outcomes are deliberately distinct rather than collapsed into "no options":
//
//	no decoder, no elements        the rule takes no options; Unconfigured is correct and expected
//	no decoder, any element        an error naming the rule and each element
//	Decode, two or more elements   an error naming the rule and every element after the first
//	DecodeList, any count          the rule's own decoder reads the whole list, upstream's shape
//	decoder, elements present      the rule bound to the decoded value
//	decoder, elements absent       an error if the rule requires options, Unconfigured if they merely tune it
//
// Collapsing the last case into Unconfigured is precisely how a required option goes missing without
// anyone finding out.
//
// # No element is dropped
//
// Every element the config wrote is either handed to the rule's decoder or refused here by name.
// The previous shape kept `tuple[1]`, so `["error", {"object": true},
// {"enforceForRenamedProperties": true}]` loaded clean, ran, and reported 0 on source the second
// element exists to flag, while the one-object spelling reported 1. A refusal costs one edit to the
// config; a silent drop costs an afternoon of somebody wondering why their option does nothing.
func (registry OptionsRegistry) Decode(ruleName string, elements []json.RawMessage) (rule.Bound, error) {
	declared, hasDeclared := registry[ruleName]
	if !hasDeclared || (declared.Decode == nil && declared.DecodeList == nil) {
		if len(elements) > 0 {
			return rule.Bound{}, fmt.Errorf(
				"rule %s takes no options, and the config gives it %s, which it would never read. "+
					"Remove the options or write the bare severity", ruleName, describeElements(elements, 0))
		}
		return declared.Unconfigured, nil
	}

	if refuseNullsError := refuseNulls(elements); refuseNullsError != nil {
		return rule.Bound{}, fmt.Errorf("rule %s: %w", ruleName, refuseNullsError)
	}

	var raw json.RawMessage
	decode := declared.Decode
	switch {
	case declared.DecodeList != nil:
		decode = declared.DecodeList
		if len(elements) > 0 {
			list, encodeError := json.Marshal(elements)
			if encodeError != nil {
				return rule.Bound{}, fmt.Errorf("rule %s: re-encoding its option elements: %w", ruleName, encodeError)
			}
			raw = list
		}
	case len(elements) > 1:
		return rule.Bound{}, fmt.Errorf(
			"rule %s takes one option element, and the config gives it %d: %s would never be read, "+
				"so the entry is refused rather than half applied",
			ruleName, len(elements), describeElements(elements, 1))
	case len(elements) == 1:
		raw = elements[0]
	}

	bound, decodeError := decode(raw)
	if decodeError != nil {
		if len(raw) == 0 && !declared.Required {
			// The rule tunes on options it did not get. Defaults are its own business.
			return declared.Unconfigured, nil
		}
		return rule.Bound{}, fmt.Errorf("rule %s: %w", ruleName, decodeError)
	}

	// A ported rule's options are also checked against upstream's own schema, so cohere accepts
	// exactly the shapes ESLint accepts (#pd2chkx). A Go decoder reads what its types let it: an
	// unknown enum string fell back to a default, a duplicate stayed a duplicate, and an entry missing
	// a required key decoded and was then dropped, all shapes ESLint refuses. One check here covers
	// every decoder.
	//
	// It runs after the decoder, so a shape both refuse keeps the decoder's message, which knows the
	// rule (an extra element is named by its text, an unknown key by the rule's own words); the
	// schema's refusal is for what the decoder let through. A bare severity is not checked, since no
	// upstream schema refuses an empty list (TestEverySchemaAcceptsTheBareSeverity).
	if len(elements) > 0 {
		if checkError := optionschema.Check(ruleName, elements); checkError != nil {
			return rule.Bound{}, fmt.Errorf("rule %s: %w", ruleName, checkError)
		}
	}
	return bound, nil
}
