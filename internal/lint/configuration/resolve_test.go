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

/*
 * Option elements after the first survive parsing rather than being dropped.
 *
 * eslint's wire format is `[severity, ...options]`, and several core rules use more than one
 * element: `eqeqeq` is `['error', 'always', { null: 'ignore' }]`. `parseRuleSetting` kept
 * `tuple[1]` and discarded the rest, so such an entry was accepted and silently did nothing beyond
 * its first option, which is the worst available outcome for a configuration file: the author sees
 * their setting in the file, the tool reports no error, and half the setting has no effect.
 *
 * Measured before the repair: a config carrying `["error", "always", {"null": "ignore"}]` resolved
 * its options to the string `"always"` alone.
 *
 * The exposure on the ahra tree was zero, since no rule there carries more than one option element,
 * so this was latent rather than live. It was repaired anyway because the failure mode is silence,
 * and a silent config defect is discovered by someone spending an afternoon on why their second
 * option does nothing.
 *
 * The single-option case below is the load-bearing half. `Options` is read by 120 decoders as the
 * first element alone, so a repair that widened that field to carry the whole array would break
 * every one of them, and a table testing only the multi-option case would not notice.
 */
func TestParseRuleSettingKeepsOptionsAfterTheFirst(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		body           string
		wantOptions    string
		wantAdditional []string
	}{
		"twoOptionElements": {
			body:           `["error", "always", {"null": "ignore"}]`,
			wantOptions:    `"always"`,
			wantAdditional: []string{`{"null": "ignore"}`},
		},
		"oneOptionElement": {
			body:        `["error", {"allow": ["warn"]}]`,
			wantOptions: `{"allow": ["warn"]}`,
		},
		"bareSeverity": {
			body: `"error"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			setting, err := parseRuleSetting([]byte(testCase.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := string(setting.Options); got != testCase.wantOptions {
				t.Errorf("Options = %s, want %s", got, testCase.wantOptions)
			}
			if len(setting.AdditionalOptions) != len(testCase.wantAdditional) {
				t.Fatalf("AdditionalOptions has %d entries, want %d",
					len(setting.AdditionalOptions), len(testCase.wantAdditional))
			}
			for index, want := range testCase.wantAdditional {
				if got := string(setting.AdditionalOptions[index]); got != want {
					t.Errorf("AdditionalOptions[%d] = %s, want %s", index, got, want)
				}
			}
		})
	}
}
