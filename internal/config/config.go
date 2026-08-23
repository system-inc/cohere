package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Severity is whether a rule runs, and how loudly.
type Severity int

const (
	// SeverityOff means the rule does not run for this file at all.
	SeverityOff Severity = iota

	// SeverityWarn reports without failing the run.
	SeverityWarn

	// SeverityError reports and fails the run.
	SeverityError
)

// RuleSetting is one rule's severity and its options, if it has any.
//
// Options travel with severity rather than in a parallel map, because an override that re-states a
// rule for a subset of files usually changes both, and splitting them makes it possible to apply
// one without the other.
type RuleSetting struct {
	Severity Severity

	// Options is the rule's configuration, decoded but not interpreted. Nil when the rule was
	// configured with a bare severity.
	//
	// A rule that requires an option and is handed nil either guards everything or nothing, and both
	// are silent. Four rules in the gate verify replaces were dead for months underneath exactly
	// that, so the distinction between "no options" and "options I did not read" is kept.
	Options json.RawMessage
}

// Override is a glob-scoped block that changes rule settings for matching files.
type Override struct {
	Files []string
	Rules map[string]RuleSetting
}

// Config is a resolved lint configuration: what runs everywhere, what never runs, and what changes
// per path.
type Config struct {
	// Rules is the base configuration, applying to every file not excluded.
	Rules map[string]RuleSetting

	// IgnorePatterns are paths no rule runs on at all.
	IgnorePatterns []string

	// Overrides apply in order, later blocks winning over earlier ones, which is how both ESLint and
	// oxlint resolve them. Order is preserved rather than sorted for exactly this reason.
	Overrides []Override

	// Root is the directory glob patterns resolve against.
	Root string
}

// Load reads a configuration file from disk.
//
// A configuration that cannot be read is an error rather than an empty config. An empty config lints
// everything with nothing configured, which is indistinguishable from a clean run and is precisely
// the failure this project keeps finding: three configurations in the gate verify replaces ran
// successfully having loaded zero plugins.
func Load(path string) (*Config, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading lint config %s: %w", path, err)
	}

	var raw rawConfig
	if err := json.Unmarshal(contents, &raw); err != nil {
		return nil, fmt.Errorf("parsing lint config %s: %w", path, err)
	}

	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("resolving the config directory for %s: %w", path, err)
	}

	loaded := &Config{
		Rules:          map[string]RuleSetting{},
		IgnorePatterns: raw.IgnorePatterns,
		Root:           root,
	}

	for name, value := range raw.Rules {
		setting, err := parseRuleSetting(value)
		if err != nil {
			return nil, fmt.Errorf("rule %q in %s: %w", name, path, err)
		}
		loaded.Rules[name] = setting
	}

	for index, rawOverride := range raw.Overrides {
		override := Override{Files: rawOverride.Files, Rules: map[string]RuleSetting{}}
		for name, value := range rawOverride.Rules {
			setting, err := parseRuleSetting(value)
			if err != nil {
				return nil, fmt.Errorf("override %d, rule %q in %s: %w", index, name, path, err)
			}
			override.Rules[name] = setting
		}
		loaded.Overrides = append(loaded.Overrides, override)
	}

	return loaded, nil
}

type rawConfig struct {
	Rules          map[string]json.RawMessage `json:"rules"`
	IgnorePatterns []string                   `json:"ignorePatterns"`
	Overrides      []rawOverride              `json:"overrides"`
}

type rawOverride struct {
	Files []string                   `json:"files"`
	Rules map[string]json.RawMessage `json:"rules"`
}

// parseRuleSetting decodes the two shapes a rule value takes: a bare severity, or a two-element
// array of severity and options.
//
// Both appear in the live config: 173 rules are bare strings and 9 carry options.
func parseRuleSetting(value json.RawMessage) (RuleSetting, error) {
	var severityName string
	if err := json.Unmarshal(value, &severityName); err == nil {
		severity, err := parseSeverity(severityName)
		return RuleSetting{Severity: severity}, err
	}

	var tuple []json.RawMessage
	if err := json.Unmarshal(value, &tuple); err != nil {
		return RuleSetting{}, fmt.Errorf("expected a severity string or a [severity, options] array, got %s", truncate(string(value)))
	}
	if len(tuple) == 0 {
		return RuleSetting{}, fmt.Errorf("empty rule configuration array")
	}

	if err := json.Unmarshal(tuple[0], &severityName); err != nil {
		return RuleSetting{}, fmt.Errorf("first element is not a severity string: %s", truncate(string(tuple[0])))
	}
	severity, err := parseSeverity(severityName)
	if err != nil {
		return RuleSetting{}, err
	}

	setting := RuleSetting{Severity: severity}
	if len(tuple) > 1 {
		setting.Options = tuple[1]
	}
	return setting, nil
}

// parseSeverity accepts the spellings the config files use, and refuses the rest loudly.
//
// An unrecognized severity silently treated as "off" is the worst available outcome: a typo in a
// rule name's severity would disable that rule across the whole tree with nothing reporting it.
func parseSeverity(name string) (Severity, error) {
	switch strings.ToLower(name) {
	case "off", "allow":
		return SeverityOff, nil
	case "warn", "warning":
		return SeverityWarn, nil
	case "error", "deny":
		return SeverityError, nil
	}
	return SeverityOff, fmt.Errorf("unknown severity %q (expected off, warn, or error)", name)
}

func truncate(value string) string {
	if len(value) <= 60 {
		return value
	}
	return value[:60] + "..."
}
