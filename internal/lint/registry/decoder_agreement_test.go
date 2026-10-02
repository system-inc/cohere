package registry

import (
	"fmt"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/lint/testing"
)

// Every registration's decoder hands its rule the type the rule reads.
//
// Run takes `options any`, so nothing at compile time compares the type a Decode returns with the
// type its rule passes to rule.OptionsAs. `unified-signatures` asserted a pointer over the value its
// decoder returned, every configured option was ignored, and every fixture stayed green because the
// fixtures handed Run options directly instead of decoding them (#qmvkf83). OptionsAs now panics on
// such a disagreement, but only when a run reaches it with decoded options, so this test makes every
// rule reach it: each decoder is fed the smallest option that means "configured" (an empty object,
// or an empty option list), and whatever it decodes is handed to that rule's own Run.
//
// A decoder that refuses the empty input (a required key, a strict schema) is skipped and counted,
// and the count has a floor, so a harness that silently decoded nothing cannot pass.
func TestEveryDecoderHandsItsRuleTheTypeTheRuleReads(t *testing.T) {
	exercised := 0
	refused := 0
	for _, registration := range rule.Registered() {
		decoded, decodeError, decodes := decodeEmptyOption(registration)
		if !decodes {
			continue
		}
		if decodeError != nil || decoded == nil {
			refused++
			continue
		}
		exercised++
		subject := registration.Rule
		t.Run(subject.Name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				message := fmt.Sprint(recovered)
				if strings.Contains(message, "registration's Decode") {
					t.Fatalf("%s: %s", subject.Name, message)
				}
				// Any other panic is the crash corpus's business, not this test's.
			}()
			if subject.NeedsTypeChecker {
				rule_testing.RunTypedWithOptions(t, subject, "/repository/source/Probe.tsx", "export const value = 1;\n", decoded)
				return
			}
			rule_testing.RunWithOptions(t, subject, "/repository/source/Probe.tsx", "export const value = 1;\n", decoded)
		})
	}
	// About 110 registrations carry a decoder today. Under 80 exercised means the empty input stopped
	// decoding for most of them, or the walk is reading the wrong list, rather than that the tree
	// lost its options.
	if exercised < 80 {
		t.Fatalf("only %d decoders produced options to hand their rule (%d refused the empty input), "+
			"so this check proved little", exercised, refused)
	}
}

// decodeEmptyOption runs a registration's decoder on the smallest configured input it takes. decodes
// is false for a rule that takes no options, or whose decoder needs to know where the config lives.
func decodeEmptyOption(registration rule.Registration) (decoded any, decodeError error, decodes bool) {
	switch {
	case registration.Decode != nil:
		decoded, decodeError = registration.Decode([]byte(`{}`))
		return decoded, decodeError, true
	case registration.DecodeOptionList != nil:
		decoded, decodeError = registration.DecodeOptionList([]byte(`[{}]`))
		if decodeError != nil {
			decoded, decodeError = registration.DecodeOptionList([]byte(`[]`))
		}
		return decoded, decodeError, true
	}
	return nil, nil, false
}
