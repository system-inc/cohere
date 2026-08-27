package registry

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/configuration"
)

// The live config, which is the only thing that decides whether a registered rule ever runs.
const liveConfigPath = "/Users/kirkouimet/Projects/ahra/VerifySettings.json"

// A rule's fixture proves it works. The config decides whether it runs, and nothing else connects
// the two: `rule_testing` never reads `VerifySettings.json`, so a rule can pass both directions of its own
// pair and be inert on every real file.
//
// That happened. The first `@next/next` rule registered as `next-no-assign-module-variable` while
// the config said `nextjs/no-assign-module-variable`, which resolves on a `/` boundary to
// `no-assign-module-variable`. The names did not match, the rule ran on nothing, and its tests were
// green. Only the coverage line caught it, and only because a second sentence happened to print.
//
// This is the mechanical version: every registered rule must be a name the live config can resolve,
// or be listed below as deliberately not enabled.
func TestEveryRegisteredRuleIsReachableFromTheLiveConfig(t *testing.T) {
	if _, err := os.Stat(liveConfigPath); err != nil {
		t.Skipf("the live config is not present at %s", liveConfigPath)
	}

	loaded, err := configuration.Load(liveConfigPath)
	if err != nil {
		t.Fatalf("loading the live config: %v", err)
	}
	resolved := loaded.Resolve("app/Probe.tsx")

	// Rules verify implements that the live config deliberately does not enable. Each needs a reason,
	// because an entry here silences the guard for that rule permanently.
	deliberatelyNotEnabled := map[string]string{
		// The differential harness's verify-only control. The gate's oxlint plugin has no such rule,
		// which is the asymmetry the control depends on, so the config cannot name it.
		"import-require-path-alias": "the directional control for the differential",

		// The live config sets `react/jsx-key` to "off" explicitly, in a block of ten-plus rules
		// this project has deliberately turned off alongside `react/react-in-jsx-scope`. That is a
		// decision about this codebase rather than a wiring gap, and flipping it here would
		// override it silently, so the rule is ported, registered and inventoried while staying
		// off. Turning it on is a config change for whoever owns that block to make.
		"react/jsx-key": "the live config turns react/jsx-key off deliberately, beside react-in-jsx-scope",

		// The live config already turns this rule off, at VerifySettings.json:370, under the
		// spelling `typescript/require-array-sort-compare`. Somebody decided against it, and the
		// port does not get to reverse that.
		//
		// What makes this worth spelling out is that enabling it would have LOOKED like a normal
		// port rather than like an override. The registered name here is the full
		// `@typescript-eslint/` spelling, and `settingFor` resolves an exact match first and then a
		// suffix trim on a `/` boundary, so the existing `typescript/` key does not resolve against
		// it. Measured directly against `settingFor`: the bare and `typescript/` spellings both
		// resolve to that "off" and the `@typescript-eslint/` one does not. Adding an "error" line
		// would therefore have silently won over a standing decision through a spelling difference,
		// with nothing in any diff to show that is what happened.
		//
		// So the rule is ported, registered and tested while staying off, the same shape as
		// `react/jsx-key` above. Turning it on is a config change for whoever owns that "off" to
		// make, and the audit puts the cost at four sites, three of them `results.sort()` over small
		// number arrays inside test assertions where the default sort is harmless.
		"@typescript-eslint/require-array-sort-compare": "the live config turns it off deliberately at VerifySettings.json:370, under the typescript/ spelling",

		// The same situation as the entry above, one line earlier in the same block: the live
		// config turns this off at VerifySettings.json:369 under the `typescript/` spelling, which
		// does not resolve against the `@typescript-eslint/` name registered here. Enabling it was
		// attempted and reverted rather than kept, because the two rules sit in the same
		// hand-maintained list of deliberate disables and treating them differently would be
		// arbitrary.
		//
		// The audit measures eleven sites, and unlike its neighbour this rule IS auto-fixable, so
		// the cleanup is a command plus a review of the diff rather than eleven judgments. That
		// makes it the cheaper of the two to turn on, and it is still not a porter's call.
		"@typescript-eslint/no-meaningless-void-operator": "the live config turns it off deliberately at VerifySettings.json:369, under the typescript/ spelling",
	}

	rules := All()
	if len(rules) == 0 {
		t.Fatal("the registry is empty, so this test proves nothing")
	}

	var unreachable []string
	for _, subject := range rules {
		if _, excused := deliberatelyNotEnabled[subject.Name]; excused {
			continue
		}
		if !resolved.Enabled(subject.Name) {
			unreachable = append(unreachable, subject.Name)
		}
	}

	// An exemption is a claim about the world, and the world moves. `import-require-path-alias` is
	// exempt because the gate's oxlint plugin has no such rule, which is what makes it the
	// differential's verify-only control. If somebody adds it to that plugin, the exemption becomes
	// wrong silently: the guard keeps passing and the rule stays unwired for a reason that no longer
	// exists.
	//
	// So the reason gets checked rather than trusted. This is the same discipline as proving a
	// detector can fail: an allowlist nobody validates is an allowlist that outlives its premise.
	const gatePluginPath = "/Users/kirkouimet/Projects/ahra/libraries/structure/libraries/nexus/code-quality/oxlint/OxlintNexusPlugin.mjs"
	if pluginSource, err := os.ReadFile(gatePluginPath); err == nil {
		if strings.Contains(string(pluginSource), "import-require-path-alias") {
			t.Errorf(
				"the gate's oxlint plugin now defines import-require-path-alias, so exempting it here " +
					"is no longer correct: it was exempt because the gate could not name it",
			)
		}
	}

	if len(unreachable) > 0 {
		t.Errorf(
			"%d registered rules are not reachable from the live config, so they run on nothing: %v\n"+
				"a rule whose name the config cannot resolve passes its own fixtures and lints no files",
			len(unreachable), unreachable,
		)
	}
}
