package config

import (
	"os"
	"testing"
)

// liveConfigPath is the real config verify must agree with. Absent in CI checkouts of this repo
// alone, which the test treats as a skip rather than a failure.
const liveConfigPath = "/Users/kirkouimet/Projects/ahra/.oxlintrc.json"

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
