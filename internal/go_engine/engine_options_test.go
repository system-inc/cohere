package go_engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_abbreviated_identifier"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_file"
	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_uppercase_initialism"
	"github.com/system-inc/cohere/internal/go_engine/settings"
)

// longFileDeparture is the reason the settings below give for loosening long-file, when they give one.
const longFileDeparture = "The parser tables are one generated unit."

// longFileSettings are settings that run long-file at error with options, and with departures when any are given,
// as settings.Load would hand them to configureRules.
func longFileSettings(options string, departures map[string]string) settings.Settings {
	setting := settings.RuleSetting{Severity: settings.SeverityError}
	if options != "" {
		setting.Options = json.RawMessage(options)
	}
	return settings.Settings{
		Rules:      map[string]settings.RuleSetting{consistency_no_long_file.Name: setting},
		Departures: departures,
	}
}

// longFileRules registers long-file with an analyzer of its own, so the max a test sets reaches no other test.
func longFileRules() []rule.Rule {
	return []rule.Rule{withOwnAnalyzer(consistency_no_long_file.Rule, consistency_no_long_file.NewAnalyzer())}
}

// A max above the house's 2,000 with no departure is refused before anything runs, and the refusal names the rule,
// the option, the value and the house value, and says what would excuse it, so the fix is one edit.
func TestALooseningOptionWithNoDepartureIsRefused(test *testing.T) {
	test.Parallel()
	configureError := configureRules(longFileSettings(`{"max": 3000}`, nil), longFileRules())
	if configureError == nil {
		test.Fatal("a max of 3000 with no departure was configured, loosening the house limit for no stated reason")
	}
	for _, fragment := range []string{consistency_no_long_file.Name, `option "max"`, "3000", "house's 2000",
		"departure", "reason"} {
		if !strings.Contains(configureError.Error(), fragment) {
			test.Errorf("the refusal %q does not say %q", configureError, fragment)
		}
	}
}

// A max above the house's beside a departure for the rule is configured, and reaches the analyzer as written.
func TestALooseningOptionWithADepartureRuns(test *testing.T) {
	test.Parallel()
	registered := longFileRules()
	departures := map[string]string{consistency_no_long_file.Name: longFileDeparture}
	ruleSettings := longFileSettings(`{"max": 3000}`, departures)
	if configureError := configureRules(ruleSettings, registered); configureError != nil {
		test.Fatalf("a max of 3000 with a departure was refused: %v", configureError)
	}
	if read := registered[0].Analyzer.Flags.Lookup("max").Value.String(); read != "3000" {
		test.Errorf("the max configured as 3000 reads %s on the analyzer", read)
	}
}

// A departure for a rule whose options loosen nothing is refused as dead, whether the options are stricter, at the
// house value, absent, or the rule is off: each would leave an exemption standing that excuses no setting.
func TestADeadDepartureIsRefused(test *testing.T) {
	test.Parallel()
	departures := map[string]string{consistency_no_long_file.Name: longFileDeparture}
	for name, ruleSettings := range map[string]settings.Settings{
		"stricter":      longFileSettings(`{"max": 1500}`, departures),
		"house value":   longFileSettings(`{"max": 2000}`, departures),
		"no options":    longFileSettings("", departures),
		"empty options": longFileSettings(`{}`, departures),
		"off": {
			Rules: map[string]settings.RuleSetting{consistency_no_long_file.Name: {
				Severity: settings.SeverityOff, Options: json.RawMessage(`{"max": 3000}`),
			}},
			Departures: departures,
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			configureError := configureRules(ruleSettings, longFileRules())
			if configureError == nil {
				test.Fatal("a departure excusing no loosening option was configured")
			}
			for _, fragment := range []string{consistency_no_long_file.Name, longFileDeparture, "dead"} {
				if !strings.Contains(configureError.Error(), fragment) {
					test.Errorf("the refusal %q does not say %q", configureError, fragment)
				}
			}
		})
	}
}

// A stricter max, and the house value itself, need no departure: only loosening the house's asks for a reason.
func TestAStricterOptionNeedsNoDeparture(test *testing.T) {
	test.Parallel()
	for _, options := range []string{`{"max": 1500}`, `{"max": 2000}`} {
		if configureError := configureRules(longFileSettings(options, nil), longFileRules()); configureError != nil {
			test.Errorf("%s with no departure was refused: %v", options, configureError)
		}
	}
}

// renameExports reaches both renaming rules' analyzers from the settings as written, and needs no departure: it
// widens the fix, never loosens a finding.
func TestRenameExportsReachesTheRenamingRules(test *testing.T) {
	test.Parallel()
	registered := []rule.Rule{
		withOwnAnalyzer(
			consistency_no_abbreviated_identifier.Rule,
			consistency_no_abbreviated_identifier.NewAnalyzer(),
		),
		withOwnAnalyzer(consistency_no_uppercase_initialism.Rule, consistency_no_uppercase_initialism.NewAnalyzer()),
	}
	ruleSettings := settings.Settings{Rules: map[string]settings.RuleSetting{}}
	for _, renaming := range registered {
		ruleSettings.Rules[renaming.Name] = settings.RuleSetting{
			Severity: settings.SeverityError, Options: json.RawMessage(`{"renameExports": true}`),
		}
	}
	if configureError := configureRules(ruleSettings, registered); configureError != nil {
		test.Fatalf("renameExports was refused: %v", configureError)
	}
	for _, renaming := range registered {
		if !rule.RenamesExports(renaming.Analyzer) {
			test.Errorf("renameExports set to true in the settings reads as off on %s", renaming.Name)
		}
	}
}
