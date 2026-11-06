package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// `--print-config` answers what runs on a file, rule by registered rule, in ESLint's shape, so a parity
// check can diff the two engines (#mnmx9s4). The cases are the ones a key listing gets wrong: an
// override turning a rule off for one path, a rule nobody configured, and a core rule configured only
// through its typescript-eslint twin's key.
func TestPrintConfigResolvesEveryRegisteredRuleForAFile(test *testing.T) {
	test.Parallel()
	lintConfiguration := &configuration.Config{
		Root: "/repository",
		Rules: map[string]configuration.RuleSetting{
			"max-classes-per-file": {
				Severity: configuration.SeverityError,
				Options:  []json.RawMessage{json.RawMessage(`{"ignoreExpressions":true}`)},
			},
			"@typescript-eslint/no-invalid-this": {Severity: configuration.SeverityError},
			"no-var":                             {Severity: configuration.SeverityWarn},
		},
		Overrides: []configuration.Override{{
			Files: []string{"**/*.test.ts"},
			Rules: map[string]configuration.RuleSetting{"no-var": {Severity: configuration.SeverityOff}},
		}},
	}

	printed := func(path string) map[string][]json.RawMessage {
		return resolvedConfigRules(lintConfiguration.Resolve(path))
	}

	sourceFile := printed("/repository/source/Thing.ts")
	if got := sourceFile["max-classes-per-file"]; len(got) != 2 ||
		string(got[0]) != "2" ||
		compacted(test, got[1]) != `{"ignoreExpressions":true}` {
		test.Errorf("max-classes-per-file printed as %s, want [2, {\"ignoreExpressions\":true}]", got)
	}
	if got := sourceFile["no-var"]; len(got) != 1 || string(got[0]) != "1" {
		test.Errorf("no-var printed as %s, want [1]", got)
	}
	// The core rule has no key of its own and runs through its twin's, as the resolver decides.
	if got := sourceFile["no-invalid-this"]; len(got) != 1 || string(got[0]) != "2" {
		test.Errorf("core no-invalid-this printed as %s, want [2] through its twin's key", got)
	}
	if _, printedAtAll := sourceFile["no-debugger"]; printedAtAll {
		test.Error("a rule nobody configured was printed")
	}

	testFile := printed("/repository/source/Thing.test.ts")
	if got := testFile["no-var"]; len(got) != 1 || string(got[0]) != "0" {
		test.Errorf("no-var on a test file printed as %s, want [0] from the override", got)
	}
}

// compacted renders a JSON element without the indentation the printer adds.
func compacted(test *testing.T, raw json.RawMessage) string {
	test.Helper()
	var buffer bytes.Buffer
	if compactError := json.Compact(&buffer, raw); compactError != nil {
		test.Fatal(compactError)
	}
	return buffer.String()
}

// The fixture goes through the command, including settings validation and the TypeScript program.
// A module local and a nonmatching override are controls against over-reporting the file's context.
func TestPrintConfigExplainsAFile(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	settings := `{"extends":"./Base.json","settings":{"better-tailwindcss":{"entryPoint":"./theme.css","attributes":["className"],"callees":["cn"]}},"rules":{"no-var":"warn","better-tailwindcss/no-unknown-classes":"error","nexus/require-await":"error","nexus/no-use-before-define":"warn","nexus/logical-assignment-operators":"error","nexus/no-implicit-coercion":"error","nexus/max-classes-per-file":["error",2]},"departures":{"no-var":"migration"},"overrides":[{"files":["Thing.ts"],"rules":{"no-var":"off"},"reason":"generated fixture"},{"files":["Other.ts"],"rules":{"no-var":"error","better-tailwindcss/no-unknown-classes":"off"},"reason":"other files"}]}`
	writeTree(test, root, map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"target":"ES2022","module":"ESNext","lib":["ES2022"],"strict":true},"include":["**/*.ts"]}`,
		"Globals.d.ts":        "declare const projectGlobal: string;\n",
		"Thing.ts":            "export const thing = projectGlobal;\n",
		"Other.ts":            "const other = projectGlobal;\n",
		"theme.css":           "@import 'tailwindcss';\n",
		"Base.json":           `{"extends":"cohere:adamic","rules":{"no-var":"error"}}`,
		"CohereSettings.json": settings,
	})
	run := func(file string) ([]byte, error) {
		test.Helper()
		command := exec.Command(binary, "--print-config", file)
		command.Dir = root
		return command.CombinedOutput()
	}
	output, runError := run("Thing.ts")
	if runError != nil {
		test.Fatalf("print-config: %v\n%s", runError, output)
	}
	var doc map[string]json.RawMessage
	if decodeError := json.Unmarshal(output, &doc); decodeError != nil {
		test.Fatalf("JSON: %v\n%s", decodeError, output)
	}
	previous := -1
	for _, section := range []string{"settings", "languageOptions", "overrides", "departures", "ruleAliases", "rules"} {
		position := bytes.Index(output, []byte("\n  \""+section+"\": "))
		if position < 0 {
			test.Errorf("missing section %s", section)
		} else if position <= previous {
			test.Errorf("section %s out of order", section)
		}
		previous = position
	}
	// The settings are read whole: a key printed beyond these in a plugin's settings fails the decode, and a
	// plugin beyond this one fails the comparison.
	type tailwindSettings struct {
		EntryPoint          string
		Attributes, Callees []string
	}
	var printedSettings map[string]tailwindSettings
	settingsDecoder := json.NewDecoder(bytes.NewReader(doc["settings"]))
	settingsDecoder.DisallowUnknownFields()
	wantSettings := map[string]tailwindSettings{
		"better-tailwindcss": {EntryPoint: "./theme.css", Attributes: []string{"className"}, Callees: []string{"cn"}},
	}
	if decodeError := settingsDecoder.Decode(&printedSettings); decodeError != nil {
		test.Errorf("settings: %v", decodeError)
	} else if !reflect.DeepEqual(printedSettings, wantSettings) {
		test.Errorf("settings = %s, want %+v", doc["settings"], wantSettings)
	}
	var language struct {
		SourceType, Target, Tsconfig string
		Globals                      map[string][]string
	}
	if decodeError := json.Unmarshal(doc["languageOptions"], &language); decodeError != nil {
		test.Error(decodeError)
	}
	if language.SourceType != "module" ||
		language.Target != "es2022" ||
		language.Tsconfig != filepath.Join(root, "tsconfig.json") {
		test.Errorf("language options = %+v", language)
	}
	if got := language.Globals["projectGlobal"]; !reflect.DeepEqual(
		got,
		[]string{filepath.Join(root, "Globals.d.ts")},
	) {
		test.Errorf("projectGlobal declarations = %v", got)
	}
	if _, ok := language.Globals["thing"]; ok {
		test.Error("module local printed as global")
	}
	var overrides []struct {
		Index        int
		Files        []string
		Reason, File string
		Rules        map[string][]json.RawMessage
	}
	if decodeError := json.Unmarshal(doc["overrides"], &overrides); decodeError != nil {
		test.Error(decodeError)
	}
	if len(overrides) != 1 {
		test.Errorf("overrides = %s", doc["overrides"])
	} else if overrides[0].Index != 0 ||
		overrides[0].Reason != "generated fixture" ||
		overrides[0].File != filepath.Join(root, "CohereSettings.json") ||
		!reflect.DeepEqual(overrides[0].Files, []string{"Thing.ts"}) ||
		!reflect.DeepEqual(overrides[0].Rules["no-var"], []json.RawMessage{json.RawMessage("0")}) {
		test.Errorf("matched override = %+v", overrides[0])
	}
	var departures map[string]struct{ File, Reason string }
	if decodeError := json.Unmarshal(doc["departures"], &departures); decodeError != nil {
		test.Error(decodeError)
	}
	if len(departures) != 1 ||
		departures["no-var"].Reason != "migration" ||
		departures["no-var"].File != filepath.Join(root, "CohereSettings.json") {
		test.Errorf("departures = %s", doc["departures"])
	}
	var rules map[string][]json.RawMessage
	var aliases map[string]string
	if decodeError := json.Unmarshal(doc["rules"], &rules); decodeError != nil {
		test.Error(decodeError)
	}
	if decodeError := json.Unmarshal(doc["ruleAliases"], &aliases); decodeError != nil {
		test.Error(decodeError)
	}
	for _, name := range []string{
		"require-await",
		"no-use-before-define",
		"logical-assignment-operators",
		"no-implicit-coercion",
		"max-classes-per-file",
	} {
		if len(rules[name]) == 0 ||
			!reflect.DeepEqual(rules["nexus/"+name], rules[name]) ||
			aliases["nexus/"+name] != name {
			test.Errorf(
				"alias nexus/%s = %s -> %q, core = %s",
				name,
				rules["nexus/"+name],
				aliases["nexus/"+name],
				rules[name],
			)
		}
	}
	if got := rules["no-var"]; len(got) != 1 || string(got[0]) != "0" {
		test.Errorf("resolved no-var = %s", got)
	}
	if again, runError := run("Thing.ts"); runError != nil || !bytes.Equal(output, again) {
		test.Errorf("unstable repeat: %v", runError)
	}
	other, runError := run("Other.ts")
	if runError != nil {
		test.Fatalf("other file: %v\n%s", runError, other)
	}
	var otherDoc struct {
		Settings        map[string]json.RawMessage
		LanguageOptions struct{ SourceType string }
	}
	if decodeError := json.Unmarshal(other, &otherDoc); decodeError != nil {
		test.Error(decodeError)
	}
	if len(otherDoc.Settings) != 0 || otherDoc.LanguageOptions.SourceType != "script" {
		test.Errorf("other file's settings/sourceType = %+v", otherDoc)
	}
	help := exec.Command(binary, "--help")
	helpText, helpError := help.CombinedOutput()
	requireChildRan(test, helpError)
	for _, phrase := range []string{"plugin package versions", "parser names", "settings.react"} {
		if !strings.Contains(string(helpText), phrase) {
			test.Errorf("help does not explain %s", phrase)
		}
	}
	// The real lint path refuses React settings; inspection must refuse them too, never print them.
	bad := strings.Replace(settings, `"settings":{`, `"settings":{"react":{"version":"19.2"},`, 1)
	if writeError := os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(bad), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if output, runError := run("Thing.ts"); runError == nil || !bytes.Contains(output, []byte(`"react"`)) {
		test.Errorf("unread React settings were not refused: %v\n%s", runError, output)
	}
}

// A reader not known to the command must work just like today's readers, without registering global
// test state. Its split also supplies an off rule's key, which must not leak into this file's settings.
func TestPrintConfigSettingsReaders(test *testing.T) {
	test.Parallel()
	base := rule.OptionsBase{Settings: map[string]json.RawMessage{
		"future-plugin": json.RawMessage(`{"read":"yes","off":"no"}`),
		"unregistered":  json.RawMessage(`{"ignored":true}`),
	}}
	registrations := []rule.SettingsRegistration{{
		Namespaces: []string{"future-plugin", "future-alias"},
		Split: func(raw []byte) (map[string]json.RawMessage, error) {
			var keys map[string]json.RawMessage
			if decodeError := json.Unmarshal(raw, &keys); decodeError != nil {
				return nil, decodeError
			}
			return map[string]json.RawMessage{
				"future/read": json.RawMessage(`{"read":` + string(keys["read"]) + `}`),
				"future/off":  json.RawMessage(`{"off":` + string(keys["off"]) + `}`),
			}, nil
		},
	}}
	resolved := configuration.Resolved{Rules: map[string]configuration.RuleSetting{
		"future/read": {Severity: configuration.SeverityWarn},
		"future/off":  {Severity: configuration.SeverityOff},
	}}
	settings, settingsForFileError := settingsForFile(base, resolved, registrations)
	if settingsForFileError != nil {
		test.Fatal(settingsForFileError)
	}
	if len(settings) != 1 || string(settings["future-plugin"]) != `{"read":"yes"}` {
		test.Errorf("registered settings = %s", settings)
	}
	resolved.Rules["future/read"] = configuration.RuleSetting{Severity: configuration.SeverityOff}
	settings, settingsForFileError = settingsForFile(base, resolved, registrations)
	if settingsForFileError != nil || len(settings) != 0 {
		test.Errorf("settings without a reader = %s, %v", settings, settingsForFileError)
	}
}

// The inherited twin lies outside the old five-name list. A scoped twin and the engine diagnostic
// exercise per-file resolution; replacing the inherited twin with its core name removes the alias.
func TestPrintConfigAliasesAndUnusedDisable(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	base := `{"extends":"cohere:adamic","rules":{"@typescript-eslint/no-invalid-this":"error"}}`
	settings := `{"extends":"./Base.json","rules":{"unused-disable":"error"},"overrides":[{"files":["Other.ts"],"rules":{"unused-disable":"off","nexus/no-debugger":"error"},"reason":"fixture exception"}]}`
	writeTree(test, root, map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"target":"ES2022","module":"ESNext","lib":["ES2022"]},"include":["*.ts"]}`,
		"Thing.ts":            "export const thing = 1;\n",
		"Other.ts":            "export const other = 2;\n",
		"Base.json":           base,
		"CohereSettings.json": settings,
	})
	read := func(file string) (map[string]string, printedUnusedDisable) {
		test.Helper()
		command := exec.Command(binary, "--print-config", file)
		command.Dir = root
		output, outputError := command.CombinedOutput()
		if outputError != nil {
			test.Fatalf("print-config: %v\n%s", outputError, output)
		}
		var doc struct {
			RuleAliases   map[string]string
			UnusedDisable *int `json:"unusedDisable"`
			Rules         map[string][]json.RawMessage
		}
		if decodeError := json.Unmarshal(output, &doc); decodeError != nil {
			test.Fatal(decodeError)
		}
		return doc.RuleAliases, printedUnusedDisable{severity: doc.UnusedDisable, rules: doc.Rules}
	}
	// unused-disable is printed on its own key, never among the rules, since --rules lists only registered
	// rules and a parity check would read an entry there as a rule cohere has no implementation for.
	checkSeverity := func(printed printedUnusedDisable, want configuration.Severity) {
		test.Helper()
		if printed.severity == nil || *printed.severity != int(want) {
			test.Errorf("unusedDisable = %v, want %d", printed.severity, want)
		}
		if _, listed := printed.rules[configuration.UnusedDisable]; listed {
			test.Errorf("unused-disable is listed among the rules, where --rules cannot answer for it")
		}
	}
	aliases, rules := read("Thing.ts")
	if aliases["@typescript-eslint/no-invalid-this"] != "no-invalid-this" ||
		!reflect.DeepEqual(rules.rules["@typescript-eslint/no-invalid-this"], rules.rules["no-invalid-this"]) {
		test.Errorf(
			"inherited twin alias = %v, rules = %s / %s",
			aliases,
			rules.rules["@typescript-eslint/no-invalid-this"],
			rules.rules["no-invalid-this"],
		)
	}
	for _, absent := range []string{
		"nexus/require-await",
		"nexus/no-use-before-define",
		"nexus/logical-assignment-operators",
		"nexus/no-implicit-coercion",
		"nexus/max-classes-per-file",
		"nexus/no-debugger",
	} {
		if _, ok := aliases[absent]; ok {
			test.Errorf("unconfigured alias %s printed", absent)
		}
		if _, ok := rules.rules[absent]; ok {
			test.Errorf("unconfigured alias rule %s printed", absent)
		}
	}
	checkSeverity(rules, configuration.SeverityError)
	aliases, rules = read("Other.ts")
	if aliases["nexus/no-debugger"] != "no-debugger" {
		test.Errorf("scoped alias = %v", aliases)
	}
	checkSeverity(rules, configuration.SeverityOff)
	// The default is asked of the engine's resolver, so its next default change requires no print fix.
	for _, fixture := range []struct {
		level string
		want  configuration.Severity
	}{
		{"warn", configuration.SeverityWarn},
		{"", (configuration.Resolved{}).UnusedDisableSeverity()},
	} {
		changed := strings.Replace(settings, `"unused-disable":"error"`, `"unused-disable":"warn"`, 1)
		if fixture.level == "" {
			changed = strings.Replace(settings, `"rules":{"unused-disable":"error"}`, `"rules":{}`, 1)
		}
		if writeError := os.WriteFile(
			filepath.Join(root, "CohereSettings.json"),
			[]byte(changed),
			0o644,
		); writeError != nil {
			test.Fatal(writeError)
		}
		_, rules = read("Thing.ts")
		checkSeverity(rules, fixture.want)
	}
	if writeError := os.WriteFile(
		filepath.Join(root, "Base.json"),
		[]byte(strings.Replace(base, "@typescript-eslint/no-invalid-this", "no-invalid-this", 1)),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	aliases, rules = read("Thing.ts")
	if _, ok := aliases["@typescript-eslint/no-invalid-this"]; ok {
		test.Errorf("removed twin still printed as alias: %v", aliases)
	}
	if _, ok := rules.rules["@typescript-eslint/no-invalid-this"]; ok {
		test.Error("removed twin still printed in rules")
	}
}

// printedUnusedDisable is what TestPrintConfigAliasesAndUnusedDisable reads back: the engine setting's own
// key, and the rules it must stay out of.
type printedUnusedDisable struct {
	severity *int
	rules    map[string][]json.RawMessage
}
