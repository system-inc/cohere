package registry

import (
	"strings"
	"testing"
)

// The rules block is not the whole config, and forgetting that has now cost three instruments.
//
// The inventory itself understated by 40 until `983ceca`, the differential harness until `7b590f6`,
// and the coverage line until `1a12b14`, that last one written within an hour of fixing the second.
// The shape is always the same: a denominator derived from what somebody wrote down, in a tree where
// forty rules are enforced by `plugins` declarations and named in no rules block.
//
// So the reader is shared now, and this is what keeps it honest. It asserts the two properties a
// caller depends on rather than a fixed count, because the count moves whenever the catalog does and
// a test pinned to 214 would fail on every legitimate change while catching nothing.
func TestEnforcedRuleNamesReadsTheWholeInventory(t *testing.T) {
	names := EnforcedRuleNames()

	if len(names) == 0 {
		// Reading nothing is the failure mode this exists to prevent, and it is silent by design in
		// the caller: a coverage note that cannot find the inventory prints no note rather than
		// refusing the run. That is right for the note and would be wrong here.
		t.Fatal("read no rules from the inventory, so every caller's denominator would be the config alone")
	}

	// A rules-block-only reading would return roughly 166. The plugin defaults are what carry it past
	// 200, so this catches the specific regression rather than any change in size.
	if len(names) < 200 {
		t.Errorf("read %d rules, which is close to what the rules block alone holds; the plugin "+
			"defaults are named in no block and a denominator without them understates by about 40",
			len(names))
	}

	// Named rules rather than a count, one from each way a rule can be enabled. `no-const-assign`
	// was proven enforced by planting a violation and watching the gate report it, and it appears in
	// no rules block anywhere in the tree.
	required := map[string]string{
		"no-const-assign":                   "a plugin default, enforced and named in no rules block",
		"nexus/consistency-no-enum":         "an explicitly enabled rule of ours",
		"react-hooks/rules-of-hooks":        "an explicitly enabled rule from a third-party plugin",
		"@typescript-eslint/await-thenable": "a type-aware rule the config asks for",
	}

	present := make(map[string]bool, len(names))
	for _, name := range names {
		present[name] = true
	}

	for name, why := range required {
		if !present[name] {
			t.Errorf("the inventory is missing %q (%s), so anything deriving a denominator from it "+
				"would understate by at least that rule", name, why)
		}
	}
}

// Every entry must be a rule name rather than a namespace or an empty string.
//
// A malformed entry does not fail loudly anywhere downstream: it becomes one more name in a set,
// inflating a denominator by one and matching nothing. That reads as an unported rule forever.
func TestEnforcedRuleNamesAreWellFormed(t *testing.T) {
	for _, name := range EnforcedRuleNames() {
		if strings.TrimSpace(name) == "" {
			t.Error("the inventory holds an empty rule name, which would inflate every denominator by one")
			continue
		}
		if strings.HasSuffix(name, "/") {
			t.Errorf("the inventory holds %q, which names a namespace rather than a rule", name)
		}
	}
}
