package typescript

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/system-inc/cohere/internal/lint/checking"
	"github.com/system-inc/cohere/internal/lint/rule"
)

type NoMisusedPromisesOptions struct {
	ChecksConditionals *bool `json:"checksConditionals"`
	// ChecksConditionalsFlagUnions is upstream's `checksConditionals: {flagUnions}`: "none" (the
	// default) reports a value that is always thenable, "all" also one a union makes sometimes
	// thenable, and "strict" also `Promise<T> | T`, whose awaited half matches the rest.
	ChecksConditionalsFlagUnions string                                    `json:"-"`
	ChecksSpreads                *bool                                     `json:"checksSpreads"`
	ChecksVoidReturn             *bool                                     `json:"checksVoidReturn"`
	ChecksVoidReturnOpts         *NoMisusedPromisesChecksVoidReturnOptions `json:"-"`
}

// UnmarshalJSON reads upstream's `checksVoidReturn`, which is a boolean or an object of six booleans,
// and its `checksConditionals`, a boolean or an object holding flagUnions.
//
// tsgolint split the two spellings into `ChecksVoidReturn` and `ChecksVoidReturnOpts`, and with no
// tags the object form could only be written under a key upstream does not have,
// `checksVoidReturnOpts`, while upstream's own `{"checksVoidReturn": {"arguments": false}}` failed
// to decode into a boolean. The two fields stay, because `Run` reads them; this fills them from
// upstream's one key. An object turns the check on with those sub-flags, as upstream's
// `parseChecksVoidReturn` does, and the sub-flags it leaves out default to true in `Run`.
func (options *NoMisusedPromisesOptions) UnmarshalJSON(raw []byte) error {
	var wire struct {
		ChecksConditionals json.RawMessage `json:"checksConditionals"`
		ChecksSpreads      *bool           `json:"checksSpreads"`
		ChecksVoidReturn   json.RawMessage `json:"checksVoidReturn"`
	}
	if unmarshalOptionsError := rule.UnmarshalOptions(raw, &wire); unmarshalOptionsError != nil {
		return unmarshalOptionsError
	}
	*options = NoMisusedPromisesOptions{ChecksSpreads: wire.ChecksSpreads}
	// An object turns the check on, as upstream's truthy test reads it, and a missing flagUnions is
	// "none", as upstream's normalizeFlagUnionsOption has it.
	// Go whitespace: raw JSON bytes of a rule's options, whose whitespace is the same in both sets.
	conditionals := strings.TrimSpace(string(wire.ChecksConditionals))
	switch {
	case conditionals == "":
	case conditionals == "true" || conditionals == "false":
		options.ChecksConditionals = type_checking.Reference(conditionals == "true")
	case strings.HasPrefix(conditionals, "{"):
		var each struct {
			FlagUnions *string `json:"flagUnions"`
		}
		if unmarshalOptionsError := rule.UnmarshalOptions(
			wire.ChecksConditionals,
			&each,
		); unmarshalOptionsError != nil {
			return fmt.Errorf("checksConditionals: %w", unmarshalOptionsError)
		}
		options.ChecksConditionals = type_checking.Reference(true)
		options.ChecksConditionalsFlagUnions = "none"
		if each.FlagUnions != nil {
			switch *each.FlagUnions {
			case "all", "strict", "none":
				options.ChecksConditionalsFlagUnions = *each.FlagUnions
			default:
				return fmt.Errorf(
					"checksConditionals.flagUnions is %q, and upstream takes all, strict or none",
					*each.FlagUnions,
				)
			}
		}
	default:
		return fmt.Errorf("checksConditionals takes a boolean or an object holding flagUnions, got %s", conditionals)
	}
	// Go whitespace: raw JSON bytes of a rule's options, whose whitespace is the same in both sets.
	trimmed := strings.TrimSpace(string(wire.ChecksVoidReturn))
	switch {
	case trimmed == "":
		return nil
	case trimmed == "true" || trimmed == "false":
		options.ChecksVoidReturn = type_checking.Reference(trimmed == "true")
		return nil
	case strings.HasPrefix(trimmed, "{"):
		var each NoMisusedPromisesChecksVoidReturnOptions
		if unmarshalOptionsError := rule.UnmarshalOptions(wire.ChecksVoidReturn, &each); unmarshalOptionsError != nil {
			return fmt.Errorf("checksVoidReturn: %w", unmarshalOptionsError)
		}
		options.ChecksVoidReturn = type_checking.Reference(true)
		options.ChecksVoidReturnOpts = &each
		return nil
	}
	return fmt.Errorf("checksVoidReturn takes a boolean or an object of booleans, got %s", trimmed)
}
