package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/differential"
	"github.com/system-inc/cohere/internal/lint/configuration"
)

// The control precondition has to be able to reject, or it is the thing it guards against.
//
// A control that misses is supposed to mean the harness is blind. A control written where its rule
// cannot fire misses identically, so the evidence and the defect are indistinguishable and the
// reader attributes the failure to the wrong thing. The check exists to make that case impossible,
// which means a check that cannot reject is worse than no check: it adds the appearance of a
// guarantee to a run that has none.
func TestAControlWhereItsRuleCannotFireIsRefused(test *testing.T) {
	test.Parallel()
	lintConfiguration := configFrom(test, lintConfigFile{
		Rules:          map[string]string{"nexus/consistency-no-enum": "error"},
		IgnorePatterns: []string{"ignored-here/**"},
	})

	// The good case first, so a check that rejects everything cannot pass this test.
	lintable := differential.Control{
		Name:         "shared-enum",
		RelativePath: filepath.Join("linted", "Planted.ts"),
		Rule:         "consistency-no-enum",
	}
	if checkControlsAreLintableError := checkControlsAreLintable(
		lintConfiguration,
		[]differential.Control{lintable},
		nil,
	); checkControlsAreLintableError != nil {
		test.Fatalf(
			"a control on a linted path with an enabled rule must be accepted: %v",
			checkControlsAreLintableError,
		)
	}

	ignoredPath := lintable
	ignoredPath.RelativePath = filepath.Join("ignored-here", "Planted.ts")
	checkControlsAreLintableError := checkControlsAreLintable(lintConfiguration, []differential.Control{ignoredPath}, nil)
	if checkControlsAreLintableError == nil {
		test.Fatal("a control on an ignored path must be refused: it would miss for a reason unrelated to the harness")
	}
	// The message has to name the pattern. "Refused" alone sends the reader looking at the harness,
	// which is the confusion this whole check exists to prevent.
	if !strings.Contains(checkControlsAreLintableError.Error(), "ignored-here/**") {
		test.Fatalf("the refusal must name the ignore pattern that would silence it, got %v", checkControlsAreLintableError)
	}

	unconfiguredRule := lintable
	unconfiguredRule.Rule = "import-require-path-alias"
	checkControlsAreLintableError = checkControlsAreLintable(lintConfiguration, []differential.Control{unconfiguredRule}, nil)
	if checkControlsAreLintableError == nil {
		test.Fatal("a control naming a rule the config does not enable must be refused")
	}
	if !strings.Contains(checkControlsAreLintableError.Error(), "import-require-path-alias") {
		test.Fatalf("the refusal must name the rule that would not fire, got %v", checkControlsAreLintableError)
	}
}

// An override that scopes a rule off for one directory is invisible from the base rule list.
//
// This is the case a check written against `lintConfig.Rules` alone would sail past: the rule is
// enabled at the base level, the path is not ignored, and the control would still be silent because
// an override turned that rule off for exactly that directory.
func TestAnOverrideThatScopesTheRuleOffIsCaught(test *testing.T) {
	test.Parallel()
	lintConfiguration := configFrom(test, lintConfigFile{
		Rules: map[string]string{"nexus/consistency-no-enum": "error"},
		Overrides: []lintConfigOverride{{
			Files: []string{"generated/**/*.ts"},
			Rules: map[string]string{"nexus/consistency-no-enum": "off"},
		}},
	})

	control := differential.Control{
		Name:         "shared-enum",
		RelativePath: filepath.Join("generated", "Planted.ts"),
		Rule:         "consistency-no-enum",
	}
	if checkControlsAreLintableError := checkControlsAreLintable(
		lintConfiguration,
		[]differential.Control{control},
		nil,
	); checkControlsAreLintableError == nil {
		test.Fatal(
			"a control under an override that turns its rule off must be refused, even though the base config enables the rule",
		)
	}

	// The same rule and the same config, outside the override's scope, must still be accepted.
	elsewhere := control
	elsewhere.RelativePath = filepath.Join("source", "Planted.ts")
	if checkControlsAreLintableError := checkControlsAreLintable(
		lintConfiguration,
		[]differential.Control{elsewhere},
		nil,
	); checkControlsAreLintableError != nil {
		test.Fatalf("the override must not reject a control outside its scope: %v", checkControlsAreLintableError)
	}
}

// The control declarations name bare rules and the config keys carry plugin prefixes.
//
// Without the lookup that reconciles them, every control would be refused as unconfigured and this
// guard would block every run, which is a failure that at least announces itself. Tested anyway
// because the opposite fix, dropping the check, is the tempting one.
func TestABareRuleNameFindsItsPluginPrefixedConfigKey(test *testing.T) {
	test.Parallel()
	lintConfiguration := configFrom(test, lintConfigFile{
		Rules: map[string]string{"nexus/consistency-no-enum": "error"},
	})

	if got := pluginQualified(lintConfiguration, "consistency-no-enum"); got != "nexus/consistency-no-enum" {
		test.Fatalf("pluginQualified(%q) = %q, want the config's own key", "consistency-no-enum", got)
	}
	// A rule the config never mentions returns the bare name, so Enabled reports unconfigured rather
	// than this helper deciding the control is valid.
	if got := pluginQualified(lintConfiguration, "never-configured"); got != "never-configured" {
		test.Fatalf("an unconfigured rule must come back unchanged, got %q", got)
	}
}

// lintConfigFile is the part of a lint config file these tests write: each rule's severity, the paths it
// ignores, and the overrides that scope rules by path.
type lintConfigFile struct {
	Rules          map[string]string    `json:"rules"`
	IgnorePatterns []string             `json:"ignorePatterns,omitempty"`
	Overrides      []lintConfigOverride `json:"overrides,omitempty"`
}

// lintConfigOverride is one override: the files it matches and each rule's severity there.
type lintConfigOverride struct {
	Files []string          `json:"files"`
	Rules map[string]string `json:"rules"`
}

// configFrom writes a lint config to disk and loads it through the real loader.
//
// Built through configuration.Load rather than by hand so the test exercises the same parsing, override
// ordering, and root resolution the command does. A hand-built Config would test this guard against
// a shape the loader never produces.
func configFrom(test *testing.T, raw lintConfigFile) *configuration.Config {
	test.Helper()

	directory := test.TempDir()
	path := filepath.Join(directory, ".oxlintrc.json")
	contents, encodeError := json.Marshal(raw)
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if writeError := os.WriteFile(path, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	loaded, loadError := configuration.Load(path)
	if loadError != nil {
		test.Fatal(loadError)
	}
	return loaded
}
