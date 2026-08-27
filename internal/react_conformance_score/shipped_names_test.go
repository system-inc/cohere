package react_conformance_score

import (
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/react_conformance"
	"github.com/system-inc/verify/internal/registry"
)

// TestShippedRuleNamesAreRegistered makes the second column of `ShippedRules` executable.
//
// # Why this test has to exist, and why it lives here
//
// `ShippedRules` maps an upstream rule name to the name verify registers that rule under. Until
// 2026-08-24 every value read `react/<name>`, and not one of those strings was a registered rule:
// the react-compiler rules register BARE. The map's own doc comment asserted the opposite, claiming
// a rename on either side would be "a visible edit rather than a silent unjoin".
//
// It was silent for a reason worth stating, because it generalises past this map. `Classify` reads
// only the KEY — `_, found := ShippedRules[ruleName]` — so the value was never evaluated by
// anything. A field that no code path reads cannot be wrong in a way any test can see, and its
// presence makes the file LOOK checked. That is worse than omitting it, and it is the mechanism
// behind the open finding that the parity guard cannot tell a namespaced spelling from a bare one.
//
// This test is the fix: it evaluates every value against the live registry. It lives in
// `react_conformance_score` rather than beside the map because `react_conformance` deliberately depends
// on nothing in verify's rule engine, and importing `registry` there would couple the instrument to
// the thing it measures.
//
// Scored by mutation: setting `hooks` back to `react/rules-of-hooks` SURVIVED the entire suite
// before this test existed, and so did corrupting an unwired rule's value. Both are caught now.
func TestShippedRuleNamesAreRegistered(t *testing.T) {
	registered := map[string]bool{}
	var names []string
	for _, registeredRule := range registry.All() {
		registered[registeredRule.Name] = true
		names = append(names, registeredRule.Name)
	}
	sort.Strings(names)

	if len(registered) == 0 {
		t.Fatal("the registry is empty, so this test would pass vacuously")
	}

	for upstream, verifyName := range react_conformance.ShippedRules {
		if registered[verifyName] {
			continue
		}
		// Name the near miss rather than only the absence. A namespaced spelling is the specific
		// error this test was written for, so saying so beats "not found" and sending the reader to
		// grep the registry.
		hint := ""
		if bare := strings.TrimPrefix(verifyName, "react/"); bare != verifyName && registered[bare] {
			hint = "; the rule registers BARE as " + bare + ", with no `react/` namespace"
		}
		t.Errorf("ShippedRules[%q] = %q, which no rule registers%s", upstream, verifyName, hint)
	}
}

// TestEveryWiredRuleIsAShippedRule keeps the engine's table and the categorisation's table joined.
//
// A rule wired here but absent from `ShippedRules` would run and then have its fixtures classified
// as `no rule shipped`, so it would score nothing while appearing to work. The reverse is allowed
// and expected: `ShippedRules` is what verify HAS, `Rules` is what this harness has WIRED, and the
// gap between them is the honest measure of what is left to do.
func TestEveryWiredRuleIsAShippedRule(t *testing.T) {
	for _, upstream := range UpstreamNames() {
		if _, shipped := react_conformance.ShippedRules[upstream]; !shipped {
			t.Errorf("rule %q is wired in the engine but absent from ShippedRules, so its fixtures would be classified as unshipped", upstream)
		}
		if Rules[upstream].Rule.Name == "" {
			t.Errorf("rule %q is wired with no underlying verify rule", upstream)
		}
		if len(Rules[upstream].Messages) == 0 {
			t.Errorf("rule %q is wired with an empty message join, so it can never report anything", upstream)
		}
	}
}

// TestWiredRuleNamesMatchTheRegisteredRule checks the engine wires the rule it claims to.
//
// `RuleUnderTest.Upstream` names an upstream rule and `.Rule` carries a verify rule value; nothing
// structural stops those two disagreeing. This asserts the verify rule's own `.Name` is the one
// `ShippedRules` says implements that upstream rule, so wiring `react.UseMemo` under the key
// `gating` fails here rather than producing a confidently wrong per-rule scoreboard.
func TestWiredRuleNamesMatchTheRegisteredRule(t *testing.T) {
	for _, upstream := range UpstreamNames() {
		want := react_conformance.ShippedRules[upstream]
		if got := Rules[upstream].Rule.Name; got != want {
			t.Errorf("engine wires %q to verify rule %q, but ShippedRules says %q implements it", upstream, got, want)
		}
	}
}
