package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/settings"
)

// configureRules sets each enabled rule's options from the settings onto its analyzer's flags, before the first
// record, so an option the rule does not have, or a value it refuses, is a refusal rather than a run that quietly
// used its default.
//
// It also holds the settings' departures to the options they excuse, both ways. An option whose value says, as a
// rule.Loosening, that it loosens the house's is refused unless the go block states a departure for its rule, and a
// departure for a rule whose options loosen nothing is refused as dead, since a stale exemption is a rule quietly
// loosened for no one. The value decides, not a list kept here, so a rule that grows a loosening option is held to
// it with nothing to update.
//
// This sets flags on package-level analyzers, which outlive the run. That is safe because the engine is a child
// process that runs once, which Main enforces by refusing a second call, and the settings are the only thing that
// sets them. A test running the engine in process many times (TestEndToEnd) gives any rule it configures an
// analyzer of its own, which runCase checks.
func configureRules(ruleSettings settings.Settings, registered []rule.Rule) error {
	analyzerRules := map[string]rule.Rule{}
	for _, registeredRule := range registered {
		analyzerRules[registeredRule.Name] = registeredRule
	}
	// The rules whose configured options loosen the house's, which are the ones a departure may excuse.
	loosened := map[string]bool{}
	// Sorted, so a config with two bad options names the same one on every run.
	for _, name := range slices.Sorted(maps.Keys(ruleSettings.Rules)) {
		setting := ruleSettings.Rules[name]
		if setting.Severity == settings.SeverityOff || setting.Options == nil {
			continue
		}
		configured, isAnalyzer := analyzerRules[name]
		if !isAnalyzer {
			return fmt.Errorf("configuring %s: it is one of the engine's own rules and takes no options", name)
		}
		var options map[string]json.RawMessage
		if decodeError := json.Unmarshal(setting.Options, &options); decodeError != nil {
			return fmt.Errorf("reading the options of %s: %w", name, decodeError)
		}
		for _, option := range slices.Sorted(maps.Keys(options)) {
			if configured.Analyzer.Flags.Lookup(option) == nil {
				return fmt.Errorf("configuring %s: it has no option %q, so the setting would configure nothing",
					name, option)
			}
			text := optionText(options[option])
			if setError := configured.Analyzer.Flags.Set(option, text); setError != nil {
				return fmt.Errorf("setting option %q of %s to %s: %w", option, name, text, setError)
			}
			// Asked after the value is set, so it answers for the value the settings gave rather than the default.
			loosening, isLoosening := configured.Analyzer.Flags.Lookup(option).Value.(rule.Loosening)
			if !isLoosening || !loosening.Loosens() {
				continue
			}
			loosened[name] = true
			if _, hasDeparture := ruleSettings.Departures[name]; !hasDeparture {
				return fmt.Errorf("configuring %s: option %q of %s loosens the house's %s, so the go block must state "+
					"a departure for %s under \"departures\" with the reason the project loosens it",
					name, option, text, loosening.HouseValue(), name)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(ruleSettings.Departures)) {
		if !loosened[name] {
			return fmt.Errorf("configuring %s: the go block states a departure for it, %q, and none of its options "+
				"loosens the house's, so the departure is dead and exempts nothing; remove it", name,
				ruleSettings.Departures[name])
		}
	}
	return nil
}

// optionText is an option's value as a flag reads it: a JSON string's contents, and any other value's literal
// text, so `7` sets 7 and `true` sets true. The kind is read from the first byte, because `null` decodes into a
// Go string as "" with no error and would then set an empty value nobody wrote.
func optionText(value json.RawMessage) string {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var text string
		if json.Unmarshal(trimmed, &text) == nil {
			return text
		}
	}
	return string(trimmed)
}
