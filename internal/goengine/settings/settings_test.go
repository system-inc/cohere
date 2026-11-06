package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	parallelRule = "cohere-go/performance-require-parallel-test"
	namingRule   = "cohere-go/consistency-no-abbreviated-identifier"
)

var knownRules = []string{parallelRule, namingRule}

// fixtureRoot is the directory every fixture below sits in, written once for the package by TestMain.
var fixtureRoot string

// rootFixtures are the engine roots, each a directory holding the named files.
var rootFixtures = map[string]map[string]string{
	"workspace": {"go.work": "go 1.27\n\nuse .\n", "go.mod": "module example.com/workspace\n\ngo 1.27\n"},
	"module":    {"go.mod": "module example.com/module\n\ngo 1.27\n"},
	"configured": {
		"go.work":             "go 1.27\n\nuse .\n",
		"go.mod":              "module example.com/configured\n\ngo 1.27\n",
		"CohereSettings.json": `{"go": {"rules": {"` + parallelRule + `": "off"}}}`,
	},
}

// fileFixtures are settings files named by an explicit path, written to files/<name>.json.
var fileFixtures = map[string]string{
	// The TypeScript engine's keys, a top-level format among them, beside no go block.
	"no-go-block": `{"rules": {"no-debugger": "error"}, "format": {"tabWidth": 4}}`,

	"every-shape": `{
		"format": {"tabWidth": 4},
		"go": {"rules": {
			"` + parallelRule + `": ["warn", {"limit": 3}],
			"` + namingRule + `": "warning"
		}}
	}`,
	"empty-rules": `{"go": {"rules": {}}}`,
	"departures": `{"go": {
		"rules": {"` + parallelRule + `": ["error", {"limit": 3}]},
		"departures": {"` + parallelRule + `": "Our suites share one database."}
	}}`,

	"go-format":          `{"go": {"format": {}, "rules": {}}}`,
	"go-other-key":       `{"go": {"rules": {}, "severity": "error", "extends": "x"}}`,
	"go-not-object":      `{"go": []}`,
	"go-null":            `{"go": null}`,
	"go-without-rules":   `{"go": {}}`,
	"rules-not-object":   `{"go": {"rules": ["error"]}}`,
	"unknown-rule":       `{"go": {"rules": {"cohere-go/performance-require-paralel-test": "error"}}}`,
	"unknown-severity":   `{"go": {"rules": {"` + parallelRule + `": "fatal"}}}`,
	"capital-severity":   `{"go": {"rules": {"` + parallelRule + `": "Error"}}}`,
	"array-bad-severity": `{"go": {"rules": {"` + parallelRule + `": ["deny"]}}}`,
	"number-value":       `{"go": {"rules": {"` + parallelRule + `": 2}}}`,
	"null-value":         `{"go": {"rules": {"` + parallelRule + `": null}}}`,
	"empty-array":        `{"go": {"rules": {"` + parallelRule + `": []}}}`,
	"three-elements":     `{"go": {"rules": {"` + parallelRule + `": ["error", {}, {}]}}}`,
	"string-options":     `{"go": {"rules": {"` + parallelRule + `": ["error", "always"]}}}`,
	"null-options":       `{"go": {"rules": {"` + parallelRule + `": ["error", null]}}}`,
	"top-level-array":    `[{"go": {"rules": {}}}]`,
	"top-level-null":     `null`,
	"malformed":          `{"go": {"rules": {}}`,

	"departures-not-object":  `{"go": {"rules": {}, "departures": ["` + parallelRule + `"]}}`,
	"departures-null":        `{"go": {"rules": {}, "departures": null}}`,
	"departure-blank-reason": `{"go": {"rules": {}, "departures": {"` + parallelRule + `": " \t "}}}`,
	"departure-empty-reason": `{"go": {"rules": {}, "departures": {"` + parallelRule + `": ""}}}`,
	"departure-not-string":   `{"go": {"rules": {}, "departures": {"` + parallelRule + `": 2}}}`,
	"departure-unknown-rule": `{"go": {"rules": {},
		"departures": {"cohere-go/performance-require-paralel-test": "x"}}}`,
}

func TestMain(suite *testing.M) {
	root, createError := os.MkdirTemp("", "goengine-settings-")
	if createError != nil {
		// Ignored: standard error is the only place left to say it, and the test binary exits next.
		fmt.Fprintf(os.Stderr, "creating the fixture directory: %v\n", createError)
		os.Exit(1)
	}
	fixtureRoot = root
	writeError := writeFixtures()
	code := 1
	if writeError != nil {
		// Ignored: standard error is the only place left to say it, and the test binary exits next.
		fmt.Fprintf(os.Stderr, "writing the fixtures: %v\n", writeError)
	} else {
		code = suite.Run()
	}
	if removeError := os.RemoveAll(root); removeError != nil {
		// Ignored: standard error is the only place left to say it, and the test binary exits next.
		fmt.Fprintf(os.Stderr, "removing the fixture directory %s: %v\n", root, removeError)
	}
	os.Exit(code)
}

func writeFixtures() error {
	files := map[string]string{}
	for directory, contents := range rootFixtures {
		for name, text := range contents {
			files[filepath.Join(directory, name)] = text
		}
	}
	for name, text := range fileFixtures {
		files[filepath.Join("files", name+".json")] = text
	}
	for relative, text := range files {
		path := filepath.Join(fixtureRoot, relative)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			return makeError
		}
		if writeError := os.WriteFile(path, []byte(text), 0o644); writeError != nil {
			return writeError
		}
	}
	return nil
}

func filePath(name string) string {
	return filepath.Join(fixtureRoot, "files", name+".json")
}

// requireEveryRuleAtError holds a defaults result to its promise: every known rule present, at error, with
// no options.
func requireEveryRuleAtError(test *testing.T, loaded Settings) {
	test.Helper()
	if len(loaded.Rules) != len(knownRules) {
		test.Errorf("got %d rules, want every known rule (%d)", len(loaded.Rules), len(knownRules))
	}
	for _, name := range knownRules {
		setting, present := loaded.Rules[name]
		if !present || setting.Severity != SeverityError || setting.Options != nil {
			test.Errorf("rule %s is %+v (present %t), want error with no options", name, setting, present)
		}
	}
}

// TestNoConfigRunsEveryRuleAtErrorAndSaysWhere: the note names the file that makes the root a root, and the
// path a config would take, so a missing config never reads as a configured clean tree.
func TestNoConfigRunsEveryRuleAtErrorAndSaysWhere(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		root     string
		manifest string
	}{
		{root: "workspace", manifest: "go.work"},
		{root: "module", manifest: "go.mod"},
	} {
		test.Run(testCase.root, func(test *testing.T) {
			test.Parallel()
			root := filepath.Join(fixtureRoot, testCase.root)
			loaded, loadError := Load(root, "", knownRules)
			if loadError != nil {
				test.Fatalf("loading with no config: %v", loadError)
			}
			requireEveryRuleAtError(test, loaded)
			want := "config: no CohereSettings.json beside " + testCase.manifest + ", so every house rule ran at " +
				"error; put one at " + filepath.Join(root, "CohereSettings.json") + " to change that"
			if loaded.Note != want {
				test.Errorf("note is %q, want %q", loaded.Note, want)
			}
		})
	}
}

// TestTheConfigBesideTheRootIsRead: with no path named, the file beside go.work decides, and a read config
// carries no note.
func TestTheConfigBesideTheRootIsRead(test *testing.T) {
	test.Parallel()
	loaded, loadError := Load(filepath.Join(fixtureRoot, "configured"), "", knownRules)
	if loadError != nil {
		test.Fatalf("loading the root's config: %v", loadError)
	}
	if loaded.Rules[parallelRule].Severity != SeverityOff {
		test.Errorf("%s is %+v, want off as the root's config says", parallelRule, loaded.Rules[parallelRule])
	}
	if loaded.Rules[namingRule].Severity != SeverityError {
		test.Errorf("%s is %+v, want error, since the config does not mention it", namingRule,
			loaded.Rules[namingRule])
	}
	if loaded.Note != "" {
		test.Errorf("note is %q, want none when a go block was read", loaded.Note)
	}
}

// TestAnExplicitPathWinsAndIsNeverReplacedByTheDefaults: a named file is read in place of the root's, and a
// named file that is missing is an error rather than a run at the defaults.
func TestAnExplicitPathWinsAndIsNeverReplacedByTheDefaults(test *testing.T) {
	test.Parallel()
	root := filepath.Join(fixtureRoot, "configured")

	named := filePath("no-go-block")
	loaded, loadError := Load(root, named, knownRules)
	if loadError != nil {
		test.Fatalf("loading the named config: %v", loadError)
	}
	if !strings.Contains(loaded.Note, named) {
		test.Errorf("note is %q, want it to name the file the flag named, %s, over the root's", loaded.Note, named)
	}

	missing := filePath("absent")
	if _, missingError := Load(root, missing, knownRules); missingError == nil {
		test.Errorf("a named config that does not exist loaded, so the run would go on at the defaults")
	} else if !strings.Contains(missingError.Error(), missing) ||
		!strings.Contains(missingError.Error(), "never replaced by the defaults") {
		test.Errorf("the refusal %q does not name %s and say the defaults are not used", missingError, missing)
	}
}

// TestAFileWithNoGoBlockRunsEveryRuleAtError: the TypeScript engine's keys, a top-level format among them,
// are not this loader's to refuse, and the note names the file that has no go block.
func TestAFileWithNoGoBlockRunsEveryRuleAtError(test *testing.T) {
	test.Parallel()
	path := filePath("no-go-block")
	loaded, loadError := Load(fixtureRoot, path, knownRules)
	if loadError != nil {
		test.Fatalf("a file with the TypeScript engine's keys and no go block was refused: %v", loadError)
	}
	requireEveryRuleAtError(test, loaded)
	want := "config: " + path + " has no go block, so every house rule ran at error"
	if loaded.Note != want {
		test.Errorf("note is %q, want %q", loaded.Note, want)
	}
}

// TestEveryAcceptedShapeLoads: a bare severity, warn and warning alike, and an array with an options object,
// whose bytes reach the rule as written. An empty rules object is a config read, so it carries no note.
func TestEveryAcceptedShapeLoads(test *testing.T) {
	test.Parallel()
	loaded, loadError := Load(fixtureRoot, filePath("every-shape"), knownRules)
	if loadError != nil {
		test.Fatalf("loading every accepted shape: %v", loadError)
	}
	parallel := loaded.Rules[parallelRule]
	if parallel.Severity != SeverityWarning {
		test.Errorf("%s has severity %q, want warning from \"warn\"", parallelRule, parallel.Severity)
	}
	var compacted bytes.Buffer
	if compactError := json.Compact(&compacted, parallel.Options); compactError != nil ||
		compacted.String() != `{"limit":3}` {
		test.Errorf("%s has options %s, want {\"limit\": 3}", parallelRule, parallel.Options)
	}
	naming := loaded.Rules[namingRule]
	if naming.Severity != SeverityWarning || naming.Options != nil {
		test.Errorf("%s is %+v, want warning with no options", namingRule, naming)
	}
	if loaded.Note != "" {
		test.Errorf("note is %q, want none when a go block was read", loaded.Note)
	}

	empty, emptyError := Load(fixtureRoot, filePath("empty-rules"), knownRules)
	if emptyError != nil {
		test.Fatalf("loading an empty rules object: %v", emptyError)
	}
	requireEveryRuleAtError(test, empty)
	if empty.Note != "" {
		test.Errorf("note is %q, want none when a go block was read", empty.Note)
	}

	for spelled, want := range map[string]Severity{
		`"error"`: SeverityError, `"warn"`: SeverityWarning, `"warning"`: SeverityWarning, `"off"`: SeverityOff,
		`["error"]`: SeverityError, `["off", {}]`: SeverityOff,
	} {
		setting, settingError := parseRuleSetting(json.RawMessage(spelled))
		if settingError != nil || setting.Severity != want {
			test.Errorf("%s read as %+v (%v), want %s", spelled, setting, settingError, want)
		}
	}
}

// TestADeparturesObjectLoads: each departure's reason reaches the settings as written, keyed by its rule, and a
// file that gives none carries none.
func TestADeparturesObjectLoads(test *testing.T) {
	test.Parallel()
	loaded, loadError := Load(fixtureRoot, filePath("departures"), knownRules)
	if loadError != nil {
		test.Fatalf("loading a departures object: %v", loadError)
	}
	if len(loaded.Departures) != 1 || loaded.Departures[parallelRule] != "Our suites share one database." {
		test.Errorf("departures read as %v, want %s's reason alone", loaded.Departures, parallelRule)
	}
	if loaded.Rules[parallelRule].Severity != SeverityError {
		test.Errorf("%s is %+v, want error beside its departure", parallelRule, loaded.Rules[parallelRule])
	}
	none, noneError := Load(fixtureRoot, filePath("every-shape"), knownRules)
	if noneError != nil {
		test.Fatalf("loading a file with no departures: %v", noneError)
	}
	if len(none.Departures) != 0 {
		test.Errorf("a file with no departures read as %v", none.Departures)
	}
}

// TestTheSeveritySpellingsAreTheLoaders: the schema offers SeveritySpellings, so each must load and a
// spelling outside them, the TypeScript loader's extra ones and a capitalized one among them, must not.
func TestTheSeveritySpellingsAreTheLoaders(test *testing.T) {
	test.Parallel()
	for _, spelling := range SeveritySpellings {
		if _, severityError := parseSeverity(json.RawMessage(`"` + spelling + `"`)); severityError != nil {
			test.Errorf("the schema offers %q and the loader refuses it: %v", spelling, severityError)
		}
	}
	for _, refused := range []string{"allow", "deny", "Error", "fatal", ""} {
		if _, severityError := parseSeverity(json.RawMessage(`"` + refused + `"`)); severityError == nil {
			test.Errorf("the loader accepts %q, which the schema does not offer", refused)
		}
	}
}

// TestEveryBadShapeIsRefusedByName: each refusal names the file and the key, so the fix is one edit.
func TestEveryBadShapeIsRefusedByName(test *testing.T) {
	test.Parallel()
	for name, fragments := range map[string][]string{
		"go-format":          {`"format"`, "gofmt"},
		"go-other-key":       {`"extends", "severity"`, `takes "rules"`},
		"go-not-object":      {"the go block is not an object"},
		"go-null":            {"the go block is not an object"},
		"go-without-rules":   {"no rules object"},
		"rules-not-object":   {"rules is not an object"},
		"unknown-rule":       {`"cohere-go/performance-require-paralel-test"`, "not a rule the Go engine knows"},
		"unknown-severity":   {parallelRule, `"fatal"`},
		"capital-severity":   {parallelRule, `"Error"`},
		"array-bad-severity": {parallelRule, `"deny"`},
		"number-value":       {parallelRule, "is 2"},
		"null-value":         {parallelRule, "is null"},
		"empty-array":        {parallelRule, "empty array"},
		"three-elements":     {parallelRule, "3 elements"},
		"string-options":     {parallelRule, `"always"`, "not an object"},
		"null-options":       {parallelRule, "null", "not an object"},
		"top-level-array":    {"the top level is not an object"},
		"top-level-null":     {"the top level is not an object"},
		"malformed":          {"unexpected end of JSON input"},

		"departures-not-object":  {"departures is", "not an object"},
		"departures-null":        {"departures is null", "not an object"},
		"departure-unknown-rule": {`"cohere-go/performance-require-paralel-test"`, "not a rule the Go engine knows"},
		"departure-blank-reason": {parallelRule, "blank reason"},
		"departure-empty-reason": {parallelRule, "blank reason"},
		"departure-not-string":   {parallelRule, "is 2", "not a reason written as a string"},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			path := filePath(name)
			_, loadError := Load(fixtureRoot, path, knownRules)
			if loadError == nil {
				test.Fatalf("%s loaded, and it should be refused", fileFixtures[name])
			}
			for _, fragment := range append([]string{path}, fragments...) {
				if !strings.Contains(loadError.Error(), fragment) {
					test.Errorf("the refusal %q does not say %q", loadError, fragment)
				}
			}
		})
	}
}
