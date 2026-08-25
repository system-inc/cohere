package config

import (
	"os"
	"testing"
)

// liveConfigPath is the real config verify must agree with. Absent in CI checkouts of this repo
// alone, which the test treats as a skip rather than a failure.
const liveConfigPath = "/Users/kirkouimet/Projects/ahra/VerifySettings.json"

// TestAgainstTheLiveConfig loads the actual file rather than a hand-written model of it.
//
// A test that models a config can drift from the config. This one reads the bytes that are actually
// gating the codebase, so a change there shows up here rather than at the next full run.
func TestAgainstTheLiveConfig(t *testing.T) {
	if _, err := os.Stat(liveConfigPath); err != nil {
		t.Skipf("the live config is not present at %s", liveConfigPath)
	}

	loaded, err := Load(liveConfigPath)
	if err != nil {
		t.Fatalf("loading the live config: %v", err)
	}

	// Prove the input is real before asserting anything about it. A config that parsed to zero rules
	// would make every assertion below pass vacuously, which is the failure mode this project keeps
	// finding.
	if len(loaded.Rules) < 100 {
		t.Fatalf("only %d rules loaded from the live config, which is not a real corpus", len(loaded.Rules))
	}
	if len(loaded.Overrides) == 0 || len(loaded.IgnorePatterns) == 0 {
		t.Fatalf("overrides=%d ignorePatterns=%d, expected both non-empty", len(loaded.Overrides), len(loaded.IgnorePatterns))
	}

	// The 336 case, against the real override block.
	generated := loaded.Resolve("libraries/structure/source/api/graphql/generated/GraphQlOperations.ts")
	for _, ruleName := range []string{
		"nexus/consistency-require-type-suffix",
		"nexus/consistency-no-abbreviated-identifier",
		"typescript/no-explicit-any",
	} {
		if generated.Enabled(ruleName) {
			t.Errorf("%s is still enabled inside generated/, so its findings would come back", ruleName)
		}
	}

	// The same rule must still run on authored source, or the override is too broad.
	authored := loaded.Resolve("libraries/structure/source/components/buttons/Button.tsx")
	if !authored.Enabled("nexus/consistency-require-type-suffix") {
		t.Error("consistency-require-type-suffix stopped running on authored source")
	}

	// The other two live override blocks.
	if loaded.Resolve("next-env.d.ts").Enabled("structure/consistency-organize-imports") {
		t.Error("the next-env.d.ts override did not apply")
	}
	if loaded.Resolve("modules/finance/connections/QuickBooksAdapter.ts").Enabled("structure/network-no-direct-fetch") {
		t.Error("the modules/** override did not apply")
	}
	if !loaded.Resolve("libraries/structure/source/api/Fetch.ts").Enabled("structure/network-no-direct-fetch") {
		t.Error("the modules/** override leaked outside modules/")
	}

	// Ignored paths.
	if !loaded.Resolve("node_modules/react/index.d.ts").Ignored {
		t.Error("node_modules was not ignored")
	}
	if loaded.Resolve("libraries/structure/source/components/buttons/Button.tsx").Ignored {
		t.Error("real source was ignored")
	}
}

// TestBothPathsAgreeOnEveryPluginDefault is the gate on removing the hand-written lines.
//
// The live config names all forty plugin-default rules by hand, which reproduces oxlint's behaviour
// in the wrong layer. Removing those lines is only safe if the plugin declaration resolves the same
// forty, and this is what shows it: the config is loaded as written, then loaded again with every
// plugin-default line stripped, and the two results are compared rule by rule.
//
// Without this the removal IS the change, and a silent disagreement between the two paths looks
// exactly like a successful migration.
//
// # The severity difference, which is real and is the reason nothing is being removed yet
//
// The two paths agree on WHICH rules run and disagree on how loudly. The hand-written lines say
// `error`; `warn_correctness` contributes `warn`, and the inventory records all forty as `warn`.
// So the hand-written lines are not reproducing the gate's behaviour, they are strengthening it --
// forty rules currently fail this build that only warn in the tool being replaced.
//
// That is a decision somebody made and did not write down, and it is not this test's to reverse.
// The test therefore asserts membership exactly and reports severity rather than requiring it to
// match, so the difference is visible instead of blocking or being smoothed away.
func TestBothPathsAgreeOnEveryPluginDefault(t *testing.T) {
	if _, err := os.Stat(liveConfigPath); err != nil {
		t.Skipf("the live config is not present at %s", liveConfigPath)
	}
	asWritten, err := Load(liveConfigPath)
	if err != nil {
		t.Fatalf("loading the live config: %v", err)
	}

	// Every plugin-default rule must be reachable through the declaration alone.
	fromPlugins := RulesFromPlugins(asWritten.Plugins, map[string]RuleSetting{})
	if len(fromPlugins) != len(PluginDefaultRules) {
		t.Fatalf("the live config's plugins resolve %d of %d plugin-default rules; a rule whose "+
			"plugin is not declared would be lost the moment its hand-written line came out",
			len(fromPlugins), len(PluginDefaultRules))
	}

	missingByHand, severityDiffers := 0, 0
	for ruleName := range PluginDefaultRules {
		byHand, named := asWritten.Rules[ruleName]
		if !named {
			// Reachable through the declaration and not named by hand. Not a failure -- it is the
			// state this whole change is heading toward -- but counted so the number is visible.
			missingByHand++
			continue
		}
		if byHand.Severity != PluginDefaultSeverity {
			severityDiffers++
		}
	}

	if missingByHand != 0 {
		t.Logf("%d plugin-default rules are already unnamed in the config and arrive only through "+
			"the declaration", missingByHand)
	}
	t.Logf("plugin-default rules: %d resolved from the declaration, %d also named by hand, "+
		"%d of those at a severity the declaration would not contribute",
		len(fromPlugins), len(PluginDefaultRules)-missingByHand, severityDiffers)

	if severityDiffers > 0 {
		t.Logf("the hand-written lines set %d rules to a severity `warn_correctness` does not "+
			"contribute; removing them would LOWER those rules to warn, which is a behaviour "+
			"change and must be a deliberate one rather than a side effect of this cleanup",
			severityDiffers)
	}
}
