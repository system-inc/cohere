package configuration

import (
	"path/filepath"
	"strings"
	"testing"
)

// A rule key that reaches no registered rule is refused at load, naming the key and the file (#yv90vjs). It
// loaded before, resolved to nothing, and the run read green with the ruling applied nowhere; only `--verbose`
// said so, and only of top-level keys.
func TestARuleKeyThatReachesNoRuleIsRefused(test *testing.T) {
	test.Parallel()
	registered := []string{"no-console", "no-unused-vars"}

	// The passing cases first, so every refusal below is shown to come from the check under test rather than
	// from a fixture that never loaded.
	for name, contents := range map[string]string{
		"a known key loads": `{"rules": {"no-console": "error"}}`,
		"a key qualifying a registered name on a slash boundary loads": `{"rules": {
			"@typescript-eslint/no-unused-vars": "error", "nexus/no-console": "off"}}`,
		"unused-disable, the engine's setting, loads at top level and in an override": `{
			"rules": {"unused-disable": "warn"},
			"overrides": [{"files": ["**/*.test.ts"], "rules": {"unused-disable": "off"}}]}`,
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			directory := writeConfigs(test, map[string]string{"CohereSettings.json": contents})
			if _, loadForError := LoadFor(filepath.Join(directory, "CohereSettings.json"), registered); loadForError != nil {
				test.Fatalf("a key that reaches a rule was refused: %v", loadForError)
			}
		})
	}

	for name, testCase := range map[string]struct {
		files map[string]string
		// refusedIn is the file the refusal must name, relative to the directory.
		refusedIn string
		want      string
	}{
		"an unknown key under rules": {
			files: map[string]string{
				"CohereSettings.json": `{"rules": {"no-console": "error", "nexus/no-such-rule": "error"}}`,
			},
			refusedIn: "CohereSettings.json",
			want:      `names "nexus/no-such-rule" under "rules"`,
		},
		"an unknown key inside an override": {
			files: map[string]string{"CohereSettings.json": `{"rules": {"no-console": "error"},
				"overrides": [{"files": ["**/*.ts"], "rules": {"no-console": "error"}},
					{"files": ["**/*.test.ts"], "rules": {"no-such-rule": "warn"}}]}`},
			refusedIn: "CohereSettings.json",
			want:      `override 1 names "no-such-rule" under "rules"`,
		},
		"an unknown key turned off": {
			files:     map[string]string{"CohereSettings.json": `{"rules": {"no-such-rule": "off"}}`},
			refusedIn: "CohereSettings.json",
			want:      `names "no-such-rule" under "rules"`,
		},
		// The match falls on a slash boundary, as the resolver's does, so a key ending in part of a name reaches
		// nothing.
		"a key that is the tail of a registered name": {
			files: map[string]string{
				"CohereSettings.json": `{"rules": {"unused-vars": "error"}}`,
			},
			refusedIn: "CohereSettings.json",
			want:      `names "unused-vars" under "rules"`,
		},
		"an unknown key in a file the project extends": {
			files: map[string]string{
				"base.json":           `{"rules": {"no-console": "error", "no-such-rule": "error"}}`,
				"CohereSettings.json": `{"extends": "./base.json"}`,
			},
			refusedIn: "base.json",
			want:      `names "no-such-rule" under "rules"`,
		},
		"every unknown key of one block, sorted": {
			files: map[string]string{
				"CohereSettings.json": `{"rules": {"zeta/missing": "error", "alpha/missing": "warn"}}`,
			},
			refusedIn: "CohereSettings.json",
			want:      `names "alpha/missing", "zeta/missing" under "rules"`,
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			directory := writeConfigs(test, testCase.files)
			_, loadForError := LoadFor(filepath.Join(directory, "CohereSettings.json"), registered)
			if loadForError == nil {
				test.Fatal("a key that reaches no registered rule loaded")
			}
			for _, want := range []string{testCase.want, filepath.Join(directory, testCase.refusedIn)} {
				if !strings.Contains(loadForError.Error(), want) {
					test.Fatalf("refused, but not naming %q:\n  %v", want, loadForError)
				}
			}
		})
	}
}

// Load knows no registered names, so it has nothing to judge a key against and refuses none. Every load the
// command makes goes through LoadFor.
func TestLoadWithoutTheRegistryJudgesNoKey(test *testing.T) {
	test.Parallel()
	directory := writeConfigs(test, map[string]string{"CohereSettings.json": `{"rules": {"no-such-rule": "error"}}`})
	loadOrFail(test, filepath.Join(directory, "CohereSettings.json"))
}
