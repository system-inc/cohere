package configuration

import "testing"

/*
 * A bare registry name reachable from two prefixed entries resolves as unconfigured, not as a guess.
 *
 * The reconciliation is a convenience and it cannot be one when the config is genuinely ambiguous.
 * Returning unconfigured routes this to the caller that already reports "nobody has said whether
 * this rule should run", which is true, rather than picking whichever key the map yielded first.
 */
func TestSettingForDeclinesAnAmbiguousBareName(t *testing.T) {
	t.Parallel()

	config := &Config{
		Rules: map[string]RuleSetting{
			"@typescript-eslint/no-shadow": {Severity: SeverityError},
			"nexus/no-shadow":              {Severity: SeverityOff},
		},
		Root: "/",
	}

	for attempt := 0; attempt < 200; attempt++ {
		if config.Resolve("/file.ts").Enabled("no-shadow") {
			t.Fatalf("attempt %d: an ambiguous bare name resolved as enabled; two prefixed entries "+
				"reach it and the config has not said which one is meant", attempt)
		}
	}
}

/*
 * The reconciliation this whole mechanism exists for still works.
 *
 * Kept beside the two guards above because they constrain it, and a fix that satisfied them by
 * removing the fallback entirely would make every one of the 170 bare registry names unconfigured,
 * therefore disabled, which is the failure the doc comment on `settingFor` records as having
 * printed 0 findings over 3,408 files with exit 0.
 */
func TestSettingForStillReconcilesABarePluginRule(t *testing.T) {
	t.Parallel()

	config := &Config{
		Rules: map[string]RuleSetting{"nexus/consistency-no-enum": {Severity: SeverityError}},
		Root:  "/",
	}
	if !config.Resolve("/file.ts").Enabled("consistency-no-enum") {
		t.Error("a bare registry name no longer finds its prefixed config entry")
	}
}
