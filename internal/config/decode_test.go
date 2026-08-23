package config

import (
	"encoding/json"
	"testing"
)

type guardOptions struct {
	LibraryDirectory string   `json:"libraryDirectory"`
	Allowed          []string `json:"allowed"`
}

type tuningOptions struct {
	MaximumLineCount int `json:"maximumLineCount"`
}

// TestRequiredOptionsFailLoudlyWhenAbsent is the whole point of this file.
//
// boundary-no-project-import was enabled and inert for months under the gate verify replaces: it
// declines every file when its LibraryDirectory is empty, which is correct behavior for a
// misconfigured guard and completely indistinguishable from a rule with nothing to report. A
// liveness harness reporting `fixtures=54 live=53 dead=1` was the only thing that ever noticed.
func TestRequiredOptionsFailLoudlyWhenAbsent(t *testing.T) {
	registry := OptionsRegistry{
		"guard": {Decode: DecodeInto[guardOptions](), Required: true},
	}

	if _, err := registry.Decode("guard", nil); err == nil {
		t.Fatal("a required option was absent and no error was raised, so the rule would decline every file in silence")
	}

	decoded, err := registry.Decode("guard", json.RawMessage(`{"libraryDirectory":"/libraries/structure/"}`))
	if err != nil {
		t.Fatalf("valid options failed to decode: %v", err)
	}
	typed, isTyped := decoded.(guardOptions)
	if !isTyped {
		t.Fatalf("options decoded to %T rather than the rule's own type, which fails its type assertion and makes it decline every file", decoded)
	}
	if typed.LibraryDirectory != "/libraries/structure/" {
		t.Fatalf("the decoded value is wrong: %+v", typed)
	}
}

// TestTuningOptionsFallBackToDefaults covers the other half. A rule whose options only adjust it
// must run on its own defaults rather than fail, or every unconfigured tunable becomes a hard error.
func TestTuningOptionsFallBackToDefaults(t *testing.T) {
	registry := OptionsRegistry{
		"tunable": {Decode: DecodeInto[tuningOptions]()},
	}

	decoded, err := registry.Decode("tunable", nil)
	if err != nil {
		t.Fatalf("an absent optional option was treated as fatal: %v", err)
	}
	if decoded != nil {
		t.Fatalf("expected nil so the rule uses its own defaults, got %+v", decoded)
	}
}

// TestARuleWithNoDecoderGetsNil keeps the common case cheap: most rules take no options at all.
func TestARuleWithNoDecoderGetsNil(t *testing.T) {
	decoded, err := OptionsRegistry{}.Decode("plain", json.RawMessage(`{"ignored":true}`))
	if err != nil {
		t.Fatalf("a rule with no decoder errored: %v", err)
	}
	if decoded != nil {
		t.Fatalf("expected nil for a rule that takes no options, got %+v", decoded)
	}
}

// TestMalformedOptionsAreAnError guards against a rule silently receiving a zero struct because its
// JSON did not parse.
func TestMalformedOptionsAreAnError(t *testing.T) {
	registry := OptionsRegistry{"guard": {Decode: DecodeInto[guardOptions](), Required: true}}

	if _, err := registry.Decode("guard", json.RawMessage(`{"libraryDirectory": 42}`)); err == nil {
		t.Fatal("options of the wrong JSON type decoded successfully")
	}
}

// TestUnconfiguredIsNotScopedOff pins the distinction the coverage line depends on.
//
// "Someone turned this rule off" and "nobody has said whether this rule should run" are different
// facts. Reporting them as one describes a brand-new rule as though it had been deliberately
// excluded, which is a lie about who decided what.
func TestUnconfiguredIsNotScopedOff(t *testing.T) {
	configuration := &Config{
		Rules: map[string]RuleSetting{
			"configured-on":  {Severity: SeverityError},
			"configured-off": {Severity: SeverityOff},
		},
	}
	resolved := configuration.Resolve("File.ts")

	if status, _ := resolved.StatusOf("configured-on"); status != StatusEnabled {
		t.Fatalf("an enabled rule reported status %d", status)
	}
	if status, _ := resolved.StatusOf("configured-off"); status != StatusScopedOff {
		t.Fatalf("a rule the config turned off reported status %d rather than scoped-off", status)
	}
	if status, _ := resolved.StatusOf("never-mentioned"); status != StatusUnconfigured {
		t.Fatalf("a rule absent from the config reported status %d rather than unconfigured", status)
	}

	ignoring := &Config{IgnorePatterns: []string{"**"}}
	if status, _ := ignoring.Resolve("File.ts").StatusOf("anything"); status != StatusFileIgnored {
		t.Fatal("a rule on an ignored file did not report the file as the reason")
	}
}

// TestTheLiveGuardRuleGetsItsOptions is the specific rule that was inert, against the real config.
func TestTheLiveGuardRuleGetsItsOptions(t *testing.T) {
	loaded, err := Load(liveConfigPath)
	if err != nil {
		t.Skipf("the live config is not present: %v", err)
	}

	raw := loaded.Resolve("libraries/structure/source/api/Fetch.ts").RawOptionsFor("boundary-no-project-import")
	if len(raw) == 0 {
		t.Fatal("boundary-no-project-import received no options from the live config, which is the inert-rule defect")
	}

	var typed guardOptions
	if err := json.Unmarshal(raw, &typed); err != nil {
		t.Fatalf("the live options did not decode: %v", err)
	}
	if typed.LibraryDirectory != "/libraries/structure/" {
		t.Fatalf("libraryDirectory did not survive the round trip: %q", typed.LibraryDirectory)
	}
}
