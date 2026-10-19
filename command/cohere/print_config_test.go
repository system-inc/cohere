package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// `--print-config` answers what runs on a file, rule by registered rule, in ESLint's shape, so a parity
// check can diff the two engines (#mnmx9s4). The cases are the ones a key listing gets wrong: an
// override turning a rule off for one path, a rule nobody configured, and a core rule configured only
// through its typescript-eslint twin's key.
func TestPrintConfigResolvesEveryRegisteredRuleForAFile(t *testing.T) {
	t.Parallel()
	config := &configuration.Config{
		Root: "/repository",
		Rules: map[string]configuration.RuleSetting{
			"max-classes-per-file":               {Severity: configuration.SeverityError, Options: []json.RawMessage{json.RawMessage(`{"ignoreExpressions":true}`)}},
			"@typescript-eslint/no-invalid-this": {Severity: configuration.SeverityError},
			"no-var":                             {Severity: configuration.SeverityWarn},
		},
		Overrides: []configuration.Override{{
			Files: []string{"**/*.test.ts"},
			Rules: map[string]configuration.RuleSetting{"no-var": {Severity: configuration.SeverityOff}},
		}},
	}

	printed := func(path string) map[string][]json.RawMessage {
		return resolvedConfigRules(config.Resolve(path))
	}

	source := printed("/repository/source/Thing.ts")
	if got := source["max-classes-per-file"]; len(got) != 2 || string(got[0]) != "2" || compacted(t, got[1]) != `{"ignoreExpressions":true}` {
		t.Errorf("max-classes-per-file printed as %s, want [2, {\"ignoreExpressions\":true}]", got)
	}
	if got := source["no-var"]; len(got) != 1 || string(got[0]) != "1" {
		t.Errorf("no-var printed as %s, want [1]", got)
	}
	// The core rule has no key of its own and runs through its twin's, as the resolver decides.
	if got := source["no-invalid-this"]; len(got) != 1 || string(got[0]) != "2" {
		t.Errorf("core no-invalid-this printed as %s, want [2] through its twin's key", got)
	}
	if _, printedAtAll := source["no-debugger"]; printedAtAll {
		t.Error("a rule nobody configured was printed")
	}

	test := printed("/repository/source/Thing.test.ts")
	if got := test["no-var"]; len(got) != 1 || string(got[0]) != "0" {
		t.Errorf("no-var on a test file printed as %s, want [0] from the override", got)
	}
}

// compacted renders a JSON element without the indentation the printer adds.
func compacted(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		t.Fatal(err)
	}
	return buffer.String()
}

// The fixture goes through the command, including settings validation and the TypeScript program.
// A module local and a nonmatching override are controls against over-reporting the file's context.
func TestPrintConfigExplainsAFile(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	root := t.TempDir()
	settings := `{"extends":"./Base.json","settings":{"better-tailwindcss":{"entryPoint":"./theme.css","attributes":["className"],"callees":["cn"]}},"rules":{"no-var":"warn","better-tailwindcss/no-unknown-classes":"error","nexus/require-await":"error","nexus/no-use-before-define":"warn","nexus/logical-assignment-operators":"error","nexus/no-implicit-coercion":"error","nexus/max-classes-per-file":["error",2]},"departures":{"no-var":"migration"},"overrides":[{"files":["Thing.ts"],"rules":{"no-var":"off"},"reason":"generated fixture"},{"files":["Other.ts"],"rules":{"no-var":"error","better-tailwindcss/no-unknown-classes":"off"},"reason":"other files"}]}`
	writeTree(t, root, map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"target":"ES2022","module":"ESNext","lib":["ES2022"],"strict":true},"include":["**/*.ts"]}`,
		"Globals.d.ts":        "declare const projectGlobal: string;\n",
		"Thing.ts":            "export const thing = projectGlobal;\n",
		"Other.ts":            "const other = projectGlobal;\n",
		"theme.css":           "@import 'tailwindcss';\n",
		"Base.json":           `{"extends":"cohere:adamic","rules":{"no-var":"error"}}`,
		"CohereSettings.json": settings,
	})
	run := func(file string) ([]byte, error) {
		t.Helper()
		command := exec.Command(binary, "--print-config", file)
		command.Dir = root
		return command.CombinedOutput()
	}
	output, err := run("Thing.ts")
	if err != nil {
		t.Fatalf("print-config: %v\n%s", err, output)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(output, &doc); err != nil {
		t.Fatalf("JSON: %v\n%s", err, output)
	}
	previous := -1
	for _, section := range []string{"settings", "languageOptions", "overrides", "departures", "ruleAliases", "rules"} {
		position := bytes.Index(output, []byte("\n  \""+section+"\": "))
		if position < 0 {
			t.Errorf("missing section %s", section)
		} else if position <= previous {
			t.Errorf("section %s out of order", section)
		}
		previous = position
	}
	wantJSON := func(section string, want string) {
		t.Helper()
		var gotValue, wantValue any
		if err := json.Unmarshal(doc[section], &gotValue); err != nil {
			t.Errorf("%s: %v", section, err)
			return
		}
		if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotValue, wantValue) {
			t.Errorf("%s = %s, want %s", section, doc[section], want)
		}
	}
	wantJSON("settings", `{"better-tailwindcss":{"entryPoint":"./theme.css","attributes":["className"],"callees":["cn"]}}`)
	var language struct {
		SourceType, Target, Tsconfig string
		Globals                      map[string][]string
	}
	if err := json.Unmarshal(doc["languageOptions"], &language); err != nil {
		t.Error(err)
	}
	if language.SourceType != "module" || language.Target != "es2022" || language.Tsconfig != filepath.Join(root, "tsconfig.json") {
		t.Errorf("language options = %+v", language)
	}
	if got := language.Globals["projectGlobal"]; !reflect.DeepEqual(got, []string{filepath.Join(root, "Globals.d.ts")}) {
		t.Errorf("projectGlobal declarations = %v", got)
	}
	if _, ok := language.Globals["thing"]; ok {
		t.Error("module local printed as global")
	}
	var overrides []struct {
		Index        int
		Files        []string
		Reason, File string
		Rules        map[string][]json.RawMessage
	}
	if err := json.Unmarshal(doc["overrides"], &overrides); err != nil {
		t.Error(err)
	}
	if len(overrides) != 1 {
		t.Errorf("overrides = %s", doc["overrides"])
	} else if overrides[0].Index != 0 || overrides[0].Reason != "generated fixture" || overrides[0].File != filepath.Join(root, "CohereSettings.json") || !reflect.DeepEqual(overrides[0].Files, []string{"Thing.ts"}) || !reflect.DeepEqual(overrides[0].Rules["no-var"], []json.RawMessage{json.RawMessage("0")}) {
		t.Errorf("matched override = %+v", overrides[0])
	}
	var departures map[string]struct{ File, Reason string }
	if err := json.Unmarshal(doc["departures"], &departures); err != nil {
		t.Error(err)
	}
	if len(departures) != 1 || departures["no-var"].Reason != "migration" || departures["no-var"].File != filepath.Join(root, "CohereSettings.json") {
		t.Errorf("departures = %s", doc["departures"])
	}
	var rules map[string][]json.RawMessage
	var aliases map[string]string
	if err := json.Unmarshal(doc["rules"], &rules); err != nil {
		t.Error(err)
	}
	if err := json.Unmarshal(doc["ruleAliases"], &aliases); err != nil {
		t.Error(err)
	}
	for _, name := range []string{"require-await", "no-use-before-define", "logical-assignment-operators", "no-implicit-coercion", "max-classes-per-file"} {
		if len(rules[name]) == 0 || !reflect.DeepEqual(rules["nexus/"+name], rules[name]) || aliases["nexus/"+name] != name {
			t.Errorf("alias nexus/%s = %s -> %q, core = %s", name, rules["nexus/"+name], aliases["nexus/"+name], rules[name])
		}
	}
	if got := rules["no-var"]; len(got) != 1 || string(got[0]) != "0" {
		t.Errorf("resolved no-var = %s", got)
	}
	if again, err := run("Thing.ts"); err != nil || !bytes.Equal(output, again) {
		t.Errorf("unstable repeat: %v", err)
	}
	other, err := run("Other.ts")
	if err != nil {
		t.Fatalf("other file: %v\n%s", err, other)
	}
	var otherDoc struct {
		Settings        map[string]json.RawMessage
		LanguageOptions struct{ SourceType string }
	}
	if err := json.Unmarshal(other, &otherDoc); err != nil {
		t.Error(err)
	}
	if len(otherDoc.Settings) != 0 || otherDoc.LanguageOptions.SourceType != "script" {
		t.Errorf("other file's settings/sourceType = %+v", otherDoc)
	}
	help := exec.Command(binary, "--help")
	helpText, _ := help.CombinedOutput()
	for _, phrase := range []string{"plugin package versions", "parser names", "settings.react"} {
		if !strings.Contains(string(helpText), phrase) {
			t.Errorf("help does not explain %s", phrase)
		}
	}
	// The real lint path refuses React settings; inspection must refuse them too, never print them.
	bad := strings.Replace(settings, `"settings":{`, `"settings":{"react":{"version":"19.2"},`, 1)
	if err := os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := run("Thing.ts"); err == nil || !bytes.Contains(output, []byte(`"react"`)) {
		t.Errorf("unread React settings were not refused: %v\n%s", err, output)
	}
}

// A reader not known to the command must work just like today's readers, without registering global
// test state. Its split also supplies an off rule's key, which must not leak into this file's settings.
func TestPrintConfigSettingsReaders(t *testing.T) {
	t.Parallel()
	base := rule.OptionsBase{Settings: map[string]json.RawMessage{
		"future-plugin": json.RawMessage(`{"read":"yes","off":"no"}`),
		"unregistered":  json.RawMessage(`{"ignored":true}`),
	}}
	registrations := []rule.SettingsRegistration{{Namespaces: []string{"future-plugin", "future-alias"}, Split: func(raw []byte) (map[string]json.RawMessage, error) {
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(raw, &keys); err != nil {
			return nil, err
		}
		return map[string]json.RawMessage{
			"future/read": json.RawMessage(`{"read":` + string(keys["read"]) + `}`),
			"future/off":  json.RawMessage(`{"off":` + string(keys["off"]) + `}`),
		}, nil
	}}}
	resolved := configuration.Resolved{Rules: map[string]configuration.RuleSetting{
		"future/read": {Severity: configuration.SeverityWarn},
		"future/off":  {Severity: configuration.SeverityOff},
	}}
	settings, err := settingsForFile(base, resolved, registrations)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 1 || string(settings["future-plugin"]) != `{"read":"yes"}` {
		t.Errorf("registered settings = %s", settings)
	}
	resolved.Rules["future/read"] = configuration.RuleSetting{Severity: configuration.SeverityOff}
	settings, err = settingsForFile(base, resolved, registrations)
	if err != nil || len(settings) != 0 {
		t.Errorf("settings without a reader = %s, %v", settings, err)
	}
}

// The inherited twin lies outside the old five-name list. A scoped twin and the engine diagnostic
// exercise per-file resolution; replacing the inherited twin with its core name removes the alias.
func TestPrintConfigAliasesAndUnusedDisable(t *testing.T) {
	t.Parallel()
	binary := buildCohere(t)
	root := t.TempDir()
	base := `{"extends":"cohere:adamic","rules":{"@typescript-eslint/no-invalid-this":"error"}}`
	settings := `{"extends":"./Base.json","rules":{"unused-disable":"error"},"overrides":[{"files":["Other.ts"],"rules":{"unused-disable":"off","nexus/no-debugger":"error"},"reason":"fixture exception"}]}`
	writeTree(t, root, map[string]string{
		"tsconfig.json":       `{"compilerOptions":{"target":"ES2022","module":"ESNext","lib":["ES2022"]},"include":["*.ts"]}`,
		"Thing.ts":            "export const thing = 1;\n",
		"Other.ts":            "export const other = 2;\n",
		"Base.json":           base,
		"CohereSettings.json": settings,
	})
	read := func(file string) (map[string]string, map[string][]json.RawMessage) {
		t.Helper()
		command := exec.Command(binary, "--print-config", file)
		command.Dir = root
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("print-config: %v\n%s", err, output)
		}
		var doc struct {
			RuleAliases map[string]string
			Rules       map[string][]json.RawMessage
		}
		if err := json.Unmarshal(output, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.RuleAliases, doc.Rules
	}
	checkSeverity := func(rules map[string][]json.RawMessage, want configuration.Severity) {
		t.Helper()
		got := rules[configuration.UnusedDisable]
		expected := []json.RawMessage{json.RawMessage(strconv.Itoa(int(want)))}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("unused-disable = %s, want %s", got, expected)
		}
	}
	aliases, rules := read("Thing.ts")
	if aliases["@typescript-eslint/no-invalid-this"] != "no-invalid-this" || !reflect.DeepEqual(rules["@typescript-eslint/no-invalid-this"], rules["no-invalid-this"]) {
		t.Errorf("inherited twin alias = %v, rules = %s / %s", aliases, rules["@typescript-eslint/no-invalid-this"], rules["no-invalid-this"])
	}
	for _, absent := range []string{"nexus/require-await", "nexus/no-use-before-define", "nexus/logical-assignment-operators", "nexus/no-implicit-coercion", "nexus/max-classes-per-file", "nexus/no-debugger"} {
		if _, ok := aliases[absent]; ok {
			t.Errorf("unconfigured alias %s printed", absent)
		}
		if _, ok := rules[absent]; ok {
			t.Errorf("unconfigured alias rule %s printed", absent)
		}
	}
	checkSeverity(rules, configuration.SeverityError)
	aliases, rules = read("Other.ts")
	if aliases["nexus/no-debugger"] != "no-debugger" {
		t.Errorf("scoped alias = %v", aliases)
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
		if err := os.WriteFile(filepath.Join(root, "CohereSettings.json"), []byte(changed), 0o644); err != nil {
			t.Fatal(err)
		}
		_, rules = read("Thing.ts")
		checkSeverity(rules, fixture.want)
	}
	if err := os.WriteFile(filepath.Join(root, "Base.json"), []byte(strings.Replace(base, "@typescript-eslint/no-invalid-this", "no-invalid-this", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	aliases, rules = read("Thing.ts")
	if _, ok := aliases["@typescript-eslint/no-invalid-this"]; ok {
		t.Errorf("removed twin still printed as alias: %v", aliases)
	}
	if _, ok := rules["@typescript-eslint/no-invalid-this"]; ok {
		t.Error("removed twin still printed in rules")
	}
}
