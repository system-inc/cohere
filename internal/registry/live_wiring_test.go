package registry

import (
	"os"
	"strings"
	"testing"

	"github.com/system-inc/verify/internal/config"
)

// The live config, which is the only thing that decides whether a registered rule ever runs.
const liveConfigPath = "/Users/kirkouimet/Projects/ahra/.oxlintrc.json"

// A rule's fixture proves it works. The config decides whether it runs, and nothing else connects
// the two: `ruletest` never reads `.oxlintrc.json`, so a rule can pass both directions of its own
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

	loaded, err := config.Load(liveConfigPath)
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
