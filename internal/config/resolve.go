package config

import (
	"path/filepath"
	"strings"
)

// Resolved is one file's answer: whether it is linted at all, and which rules apply to it.
type Resolved struct {
	// Ignored is true when an ignorePattern excluded the file entirely.
	Ignored bool

	// IgnoredBy is the pattern that excluded it, so a report can say which one rather than only that
	// something did.
	IgnoredBy string

	// Rules is the effective setting per rule name after overrides are applied.
	Rules map[string]RuleSetting
}

// Enabled reports whether a rule runs on this file.
func (r Resolved) Enabled(ruleName string) bool {
	if r.Ignored {
		return false
	}
	setting, configured := r.Rules[ruleName]
	if !configured {
		// A rule the config never mentions is not enabled. Defaulting the other way would mean adding
		// a rule to the registry silently turns it on across the whole tree, which is a decision that
		// belongs in the config rather than in a Go file.
		return false
	}
	return setting.Severity != SeverityOff
}

// OptionsFor returns a rule's options for this file, or nil.
func (r Resolved) OptionsFor(ruleName string) any {
	setting, configured := r.Rules[ruleName]
	if !configured || len(setting.Options) == 0 {
		return nil
	}
	return setting.Options
}

// Resolve computes the effective configuration for one file path.
//
// Precedence is base rules first, then each matching override in order, later blocks winning. That
// is how both ESLint and oxlint resolve, and matching them exactly is the whole point: verify cannot
// be diffed honestly against the gate it replaces while the two disagree about which rules were
// even supposed to run.
func (c *Config) Resolve(path string) Resolved {
	relative := c.relativePath(path)

	for _, pattern := range c.IgnorePatterns {
		if Match(pattern, relative) {
			return Resolved{Ignored: true, IgnoredBy: pattern}
		}
	}

	// The base map is copied rather than shared, because an override writes into the result and a
	// shared map would leak one file's overrides into every later file.
	effective := make(map[string]RuleSetting, len(c.Rules))
	for name, setting := range c.Rules {
		effective[name] = setting
	}

	for _, override := range c.Overrides {
		if !MatchAny(override.Files, relative) {
			continue
		}
		for name, setting := range override.Rules {
			effective[name] = setting
		}
	}

	return Resolved{Rules: effective}
}

// relativePath expresses a path the way the config's patterns are written: relative to the config
// file, slash-separated, with no leading dot.
//
// Getting this wrong is silent in the dangerous direction. An absolute path never matches
// `modules/**`, so every override quietly stops applying and the rules they scoped off come back —
// which is exactly the 336 findings this package exists to remove.
func (c *Config) relativePath(path string) string {
	normalized := filepath.ToSlash(path)

	if c.Root != "" && filepath.IsAbs(path) {
		if relative, err := filepath.Rel(c.Root, path); err == nil {
			normalized = filepath.ToSlash(relative)
		}
	}

	return strings.TrimPrefix(normalized, "./")
}
