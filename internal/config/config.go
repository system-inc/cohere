package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

	// Plugins are the namespaces the config declares, which contribute their own rules without any
	// rules block naming them. See PluginDefaultRules.
	Plugins []string

	// Root is the directory glob patterns resolve against.
	Root string
}

// PluginDefaultRules are the rules a `plugins` declaration turns on without any rules block naming
// them, keyed by rule name and valued by the plugin that contributes each.
//
// # Why this is a table rather than a computation
//
// oxlint's mechanism is `warn_correctness(plugins)` in `config_builder.rs:570`: every rule whose
// category is `Correctness` AND whose plugin is declared, at severity `Warn`. Reproducing that
// directly needs each rule's category, and neither this registry nor `rule-inventory.json` carries
// one -- adding categories for 214 rules would be a fresh transcription of oxc's metadata, which is
// a larger and less trustworthy job than the one this solves.
//
// The table is not a transcription. `rule-inventory.json`'s `enabledBy: pluginDefault` was
// established empirically, by planting violations and watching the real oxlint binary report rules
// named in no config -- `no-const-assign` and `react/no-children-prop` among them. That is a
// measurement of what the tool actually enables, which is what parity is about, rather than a
// second-hand copy of the metadata that produces it.
//
// # `eslint` is not a plugin, and the 16 core rules depend on that
//
// Sixteen of these have no namespace, and no `plugins` entry names `eslint`. In oxc, `ESLINT` is a
// bitflag with the value 0 (`config/plugins.rs:95`), so `plugins.contains(ESLINT)` is true whatever
// is declared, and upstream's own comment on `warn_correctness` says "there's no way to disable
// ESLint correctness rules". They are therefore contributed unconditionally, and `PluginEslint` is
// the name that records that rather than leaving sixteen entries looking unattributed.
var PluginDefaultRules = pluginDefaultRules()

// PluginEslint is the pseudo-plugin that contributes core rules. See PluginDefaultRules.
const PluginEslint = "eslint"

// pluginContributing returns which plugin a plugin-default rule arrives from.
func pluginContributing(ruleName string) string {
	namespace, _, namespaced := strings.Cut(ruleName, "/")
	if !namespaced {
		return PluginEslint
	}
	return namespace
}

// PluginDefaultSeverity is the severity a plugin declaration contributes at.
//
// `Warn`, and not by choice: `warn_correctness` inserts `AllowWarnDeny::Warn` and the inventory
// records all forty as `warn`. This is a faithful statement of what a declaration contributes and is
// deliberately not bent to encode a house preference; see below for where the preference lives.
//
// # The forty `error` lines in the config are deliberate, and removing them is a downgrade
//
// The config names all forty of these rules at `error`. They look like redundant leftovers now that
// a declaration resolves the same forty, and they are not: **the declaration contributes `warn`, so
// deleting those lines lowers all forty rather than changing nothing.**
//
// Ruled 2026-08-25 that they stay, and the reasoning is recorded here because this is where the next
// cleanup pass will look before deleting them:
//
//   - Parity is the acceptance criterion for WHICH rules run, not for how loudly. Those are separable
//     and only the first is what this migration promised.
//   - The set does not contain a rule whose violation is arguably fine, which is what `warn` would
//     assert. `no-const-assign` is a runtime TypeError, `no-this-before-super` is a crash,
//     `no-obj-calls` calls a non-function, `react/no-direct-mutation-state` silently drops a render.
//   - The tool being replaced is the floor rather than the target. verify exists because that gate
//     was insufficient, so inheriting its severity because it is the incumbent proves too much.
//
// The choice was originally made by someone who left no note, and `VerifySettings.json` is untracked
// so there is no author or date to recover. That is a gap in the record rather than evidence the
// choice was careless: reverting an undocumented decision to a weaker one BECAUSE it is undocumented
// is how a codebase loses hard-won strictness one blameless commit at a time.
//
// `TestBothPathsAgreeOnEveryPluginDefault` reports the divergence on every run, so this cannot go
// quiet.
const PluginDefaultSeverity = SeverityWarn

// RulesFromPlugins returns the settings a `plugins` declaration contributes, given what the rules
// block already names.
//
// A rule the config names explicitly is left alone: an explicit line is a decision and a default is
// not, so the default never overrides one. That is also what oxlint does, since a rules block entry
// is applied after `warn_correctness` seeds the map.
func RulesFromPlugins(plugins []string, named map[string]RuleSetting) map[string]RuleSetting {
	declared := map[string]bool{PluginEslint: true}
	for _, plugin := range plugins {
		declared[plugin] = true
	}

	contributed := map[string]RuleSetting{}
	for ruleName, plugin := range PluginDefaultRules {
		if !declared[plugin] {
			continue
		}
		if _, explicit := named[ruleName]; explicit {
			continue
		}
		contributed[ruleName] = RuleSetting{Severity: PluginDefaultSeverity}
	}
	return contributed
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

	if err := checkTopLevelKeys(contents, path); err != nil {
		return nil, err
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
		Plugins:        raw.Plugins,
		Root:           root,
	}

	for name, value := range raw.Rules {
		setting, err := parseRuleSetting(value)
		if err != nil {
			return nil, fmt.Errorf("rule %q in %s: %w", name, path, err)
		}
		loaded.Rules[name] = setting
	}

	// Applied after the rules block, and reading it: an explicit line is a decision and a default is
	// not, so `RulesFromPlugins` skips any rule already named. Seeding before would let a default
	// overwrite a deliberate `off`.
	for name, setting := range RulesFromPlugins(raw.Plugins, loaded.Rules) {
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
	Plugins        []string                   `json:"plugins"`
	Rules          map[string]json.RawMessage `json:"rules"`
	IgnorePatterns []string                   `json:"ignorePatterns"`
	Overrides      []rawOverride              `json:"overrides"`
}

// parsedTopLevelKeys are the keys `rawConfig` decodes and the loader acts on.
var parsedTopLevelKeys = map[string]bool{
	"plugins":        true,
	"rules":          true,
	"ignorePatterns": true,
	"overrides":      true,
}

// ignoredTopLevelKeys are the keys the loader deliberately does not act on, each with the reason.
//
// A reason is required, and the requirement is enforced below rather than left to convention. An
// entry with no reason says only that somebody wanted the config to load, which is the move this
// whole guard exists to stop -- the same defect one level down.
var ignoredTopLevelKeys = map[string]string{
	"$schema": "editor metadata; nothing in the linter reads it and nothing should, so it is " +
		"ignored on purpose rather than by omission",

	"jsPlugins": "paths to the JavaScript rule implementations the gate being replaced loads at " +
		"runtime. This linter compiles its rules in, so there is nothing to load and nothing to " +
		"honour. Ignored deliberately, and it stays in the config because oxlint still reads it " +
		"while both tools run side by side.",

	"settings": "per-plugin configuration for the JavaScript plugins above, and it is the entry " +
		"most worth re-reading. `settings.better-tailwindcss.entryPoint` names this repository's " +
		"root stylesheet, and `findTailwindEntryPoint` does not read it -- it probes a hardcoded " +
		"candidate list whose first entry is that same path. All three repositories we lint hit " +
		"that first candidate, so the divergence is latent rather than live: there is no known " +
		"case of it producing a wrong answer, and a project whose stylesheet is elsewhere gets a " +
		"loud decline rather than a wrong reading. Ignored on that basis, and the shape is worth " +
		"naming -- right on the population we write, wrong on the mechanism, invisible to a corpus " +
		"differential.",
}

// checkTopLevelKeys refuses a config carrying a key the loader does not implement.
//
// # Why this refuses rather than warning
//
// `encoding/json` drops an unlisted key silently and returns no error, so a config could declare
// something and have it do nothing with every check passing. Four keys were in exactly that state:
// `plugins`, `jsPlugins` and `settings` were all being discarded, and `settings` named a Tailwind
// entry point that a hardcoded candidate list happened to probe first.
//
// None of them was found by a mechanism. `plugins` surfaced while someone investigated why 40
// rules ran on zero files; the other two surfaced only because that investigation prompted a read of
// the whole file. Nothing would have surfaced the next one.
//
// That is the same defect as a rule offered no files, sitting in this tool's own front door: the
// author believes something is configured, every check is green, and nothing runs. Refusing is the
// shape the rest of the tool already takes -- `Load` errors rather than returning an empty config for
// exactly this reason, and its comment says three configurations in the gate verify replaces "ran
// successfully having loaded zero plugins."
//
// Warning was the alternative and it was rejected on the tool's own thesis. A warning goes into an
// output that already carries two hundred coverage notes, and a config author who edits one key does
// not re-read that output. A warning nobody reads is the silence this is meant to break, with a
// line of text in front of it. The kindness argument for warning is real, and the answer is that
// the error names the key and the file, so the fix is one edit rather than an investigation.
func checkTopLevelKeys(contents []byte, path string) error {
	var keyed map[string]json.RawMessage
	if err := json.Unmarshal(contents, &keyed); err != nil {
		// Not this function's error to report. Returning nil hands the real parse failure to the
		// caller's own Unmarshal, which produces the better message for a malformed file.
		return nil
	}

	var unimplemented []string
	for key := range keyed {
		if parsedTopLevelKeys[key] {
			continue
		}
		reason, ignored := ignoredTopLevelKeys[key]
		if !ignored {
			unimplemented = append(unimplemented, key)
			continue
		}
		if reason == "" {
			return fmt.Errorf(
				"lint config %s declares %q, which is listed as deliberately ignored with no reason "+
					"given: an entry with no reason says only that somebody wanted this to load",
				path, key)
		}
	}
	if len(unimplemented) == 0 {
		return nil
	}
	sort.Strings(unimplemented)

	return fmt.Errorf(
		"lint config %s declares %s, which this loader does not implement: the key would be "+
			"discarded silently and whatever it configures would never take effect. Either "+
			"implement it in `rawConfig` and act on it, or add it to `ignoredTopLevelKeys` "+
			"with a note saying why ignoring it is correct",
		path, strings.Join(quoteEach(unimplemented), ", "))
}

// quoteEach quotes each name so a multi-key message reads unambiguously.
func quoteEach(names []string) []string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		quoted = append(quoted, strconv.Quote(name))
	}
	return quoted
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

// pluginDefaultRules builds the table. See PluginDefaultRules for why it is a table.
//
// Generated from `rule-inventory.json`'s `enabledBy: pluginDefault` entries and pinned by
// `TestPluginDefaultsMatchTheInventory`, which reads that file and fails on any drift. Written out
// rather than read at runtime because `internal/config` has no business reading a repository-root
// JSON file to answer a question about its own semantics, and a test can hold the two together.
func pluginDefaultRules() map[string]string {
	return map[string]string{
		"constructor-super":                   PluginEslint,
		"getter-return":                       PluginEslint,
		"no-caller":                           PluginEslint,
		"no-class-assign":                     PluginEslint,
		"no-const-assign":                     PluginEslint,
		"no-dupe-class-members":               PluginEslint,
		"no-eval":                             PluginEslint,
		"no-func-assign":                      PluginEslint,
		"no-import-assign":                    PluginEslint,
		"no-iterator":                         PluginEslint,
		"no-new-native-nonconstructor":        PluginEslint,
		"no-obj-calls":                        PluginEslint,
		"no-setter-return":                    PluginEslint,
		"no-this-before-super":                PluginEslint,
		"no-unsafe-negation":                  PluginEslint,
		"no-with":                             PluginEslint,
		"react/forward-ref-uses-ref":          "react",
		"react/jsx-no-duplicate-props":        "react",
		"react/jsx-no-undef":                  "react",
		"react/jsx-props-no-spread-multi":     "react",
		"react/no-children-prop":              "react",
		"react/no-danger-with-children":       "react",
		"react/no-did-mount-set-state":        "react",
		"react/no-did-update-set-state":       "react",
		"react/no-direct-mutation-state":      "react",
		"react/no-find-dom-node":              "react",
		"react/no-is-mounted":                 "react",
		"react/no-render-return-value":        "react",
		"react/no-string-refs":                "react",
		"react/no-this-in-sfc":                "react",
		"react/no-unescaped-entities":         "react",
		"react/no-unsafe":                     "react",
		"react/no-will-update-set-state":      "react",
		"react/void-dom-elements-no-children": "react",
		"react/void-use-memo":                 "react",
		"typescript/no-array-delete":          "typescript",
		"typescript/no-for-in-array":          "typescript",
		"typescript/no-implied-eval":          "typescript",
		"typescript/no-unnecessary-parameter-property-assignment": "typescript",
		"typescript/no-unsafe-unary-minus":                        "typescript",
		"typescript/no-useless-empty-export":                      "typescript",
	}
}
