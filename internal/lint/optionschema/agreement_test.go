package optionschema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// samplesFile is testdata/samples.json, written by internal/lint/tools/option_schemas/extract.mjs: option
// lists drawn from each upstream schema in both directions, each with ajv's verdict as ESLint 10
// configures ajv.
type samplesFile struct {
	GeneratedFrom map[string]string `json:"generatedFrom"`
	Samples       []struct {
		Rule     string            `json:"rule"`
		Elements []json.RawMessage `json:"elements"`
		Keyword  string            `json:"keyword"`
		Path     string            `json:"path"`
		EsLint   string            `json:"eslint"`
	} `json:"samples"`
}

// corpusDirectory is ESLint's own test rows for core, every one of which ESLint ran with its options.
const corpusDirectory = "../registry/testdata/eslint-corpus/eslint-10.8.1"

// The gate is two counts, each required to be zero (#pd2chkx condition 5): refusing a list ajv accepts,
// which would stop a run on a config ESLint loads, and accepting a list ajv refuses, which is the hole
// this package closes. Reported separately, because the first is the costly direction.
func TestValidatorAgreesWithAjv(test *testing.T) {
	test.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "samples.json"))
	if err != nil {
		test.Fatal(err)
	}
	var samples samplesFile
	if decodeError := json.Unmarshal(data, &samples); decodeError != nil {
		test.Fatal(decodeError)
	}
	if !equalVersions(samples.GeneratedFrom, GeneratedFrom()) {
		test.Fatalf("the samples were drawn from %v and the embedded schemas from %v; regenerate both together", samples.GeneratedFrom, GeneratedFrom())
	}

	var falseRefusals []string
	var falseAccepts []string
	accepted, refused := 0, 0
	for _, sample := range samples.Samples {
		schema, found, optionSchemaForError := For(sample.Rule)
		if optionSchemaForError != nil {
			test.Fatalf("%s: %v", sample.Rule, optionSchemaForError)
		}
		if !found {
			test.Fatalf("%s has a sample and no schema", sample.Rule)
		}
		violation := schema.Validate(sample.Elements)
		encoded, encodeError := json.Marshal(sample.Elements)
		if encodeError != nil {
			test.Fatalf("%s: %v", sample.Rule, encodeError)
		}
		switch sample.EsLint {
		case "accepts":
			accepted++
			if violation != nil {
				falseRefusals = append(falseRefusals, fmt.Sprintf("%s %s: %v", sample.Rule, encoded, violation))
			}
		case "refuses":
			refused++
			if violation == nil {
				falseAccepts = append(falseAccepts, fmt.Sprintf("%s %s (%s at %s)", sample.Rule, encoded, sample.Keyword, sample.Path))
			}
		default:
			test.Fatalf("%s: unknown verdict %q", sample.Rule, sample.EsLint)
		}
	}

	corpusRows := 0
	entries, err := os.ReadDir(corpusDirectory)
	if err != nil {
		test.Fatal(err)
	}
	for _, entry := range entries {
		ruleName, isRows := strings.CutSuffix(entry.Name(), ".json")
		if !isRows || entry.Name() == "known-gaps.json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(corpusDirectory, entry.Name()))
		if err != nil {
			test.Fatal(err)
		}
		var rows []struct {
			Options []json.RawMessage `json:"options"`
		}
		if decodeError := json.Unmarshal(data, &rows); decodeError != nil {
			test.Fatalf("%s: %v", entry.Name(), decodeError)
		}
		schema, found, err := For(ruleName)
		if err != nil {
			test.Fatalf("%s: %v", ruleName, err)
		}
		for _, row := range rows {
			if len(row.Options) == 0 {
				continue
			}
			corpusRows++
			if !found {
				continue
			}
			if violation := schema.Validate(row.Options); violation != nil {
				encoded, encodeError := json.Marshal(row.Options)
				if encodeError != nil {
					test.Fatalf("%s: %v", ruleName, encodeError)
				}
				falseRefusals = append(falseRefusals, fmt.Sprintf("%s corpus row %s: %v", ruleName, encoded, violation))
			}
		}
	}

	test.Logf("samples: ajv accepts %d and refuses %d; corpus option rows: %d", accepted, refused, corpusRows)
	report := func(label string, failures []string) {
		if len(failures) == 0 {
			return
		}
		sort.Strings(failures)
		shown := failures
		if len(shown) > 40 {
			shown = shown[:40]
		}
		test.Errorf("%s: %d\n  %s", label, len(failures), strings.Join(shown, "\n  "))
	}
	report("refuses what ajv accepts", falseRefusals)
	report("accepts what ajv refuses", falseAccepts)

	// The counts are part of the gate: a sample file that shrank would read as agreement.
	if accepted < 1900 || refused < 8000 || corpusRows != 6641 {
		test.Errorf("the gate ran on %d accepted samples, %d refused and %d corpus rows, fewer than it was built on", accepted, refused, corpusRows)
	}
}

// Every embedded schema compiles, so a rule's first config load can never be the place a schema fails.
func TestEveryEmbeddedSchemaCompiles(test *testing.T) {
	test.Parallel()

	for _, name := range RuleNames() {
		if _, _, optionSchemaForError := For(name); optionSchemaForError != nil {
			test.Errorf("%s: %v", name, optionSchemaForError)
		}
	}
	if count := len(RuleNames()); count != 384 {
		test.Errorf("the embedded schemas cover %d rules, want 384", count)
	}
}

func equalVersions(left map[string]string, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for name, version := range left {
		if right[name] != version {
			return false
		}
	}
	return true
}

// pluginOrigins are the origins docs/data/rules.json gives a rule ported from ESLint core or an ESLint
// plugin, which are the rules extract.mjs reads a schema for.
var pluginOrigins = map[string]bool{
	"core": true, "typescript-eslint": true, "react": true, "react-hooks": true, "next": true,
	"tailwind": true, "eslint-comments": true, "boundaries": true,
}

// The embedded schemas cover exactly the rules cohere ports from ESLint, as docs/data/rules.json lists
// them (which docsdata's own check keeps current with the registry). A port added without regenerating
// has no schema and would be checked by nothing, and a schema for a rule cohere dropped is dead weight.
func TestSchemasCoverEveryPortedRule(test *testing.T) {
	test.Parallel()

	data, readError := os.ReadFile(filepath.Join("..", "..", "..", "docs", "data", "rules.json"))
	if readError != nil {
		test.Fatal(readError)
	}
	var rules struct {
		Rules []struct {
			Name   string `json:"name"`
			Origin string `json:"origin"`
		} `json:"rules"`
	}
	if decodeError := json.Unmarshal(data, &rules); decodeError != nil {
		test.Fatal(decodeError)
	}
	ported := map[string]bool{}
	for _, row := range rules.Rules {
		if pluginOrigins[row.Origin] {
			ported[row.Name] = true
		}
	}
	embedded := map[string]bool{}
	for _, name := range RuleNames() {
		embedded[name] = true
		if !ported[name] {
			test.Errorf("%s has an embedded schema and is not a ported rule; regenerate with extract.mjs", name)
		}
	}
	for name := range ported {
		if !embedded[name] {
			test.Errorf("%s is ported and has no embedded schema; regenerate with extract.mjs", name)
		}
	}
}

// A bare severity is an empty option list, which the config layer does not check because no upstream
// schema refuses it. This holds that true for every embedded schema.
func TestEverySchemaAcceptsTheBareSeverity(test *testing.T) {
	test.Parallel()

	for _, name := range RuleNames() {
		schema, found, optionSchemaForError := For(name)
		if optionSchemaForError != nil {
			test.Fatalf("%s: %v", name, optionSchemaForError)
		}
		if found {
			if violation := schema.Validate(nil); violation != nil {
				test.Errorf("%s refuses the bare severity: %v", name, violation)
			}
		}
	}
}

// The extension list is a set of decisions, so its size is a bare equality: adding one is a change to
// this number, made on purpose (#pd2chkx condition 6).
func TestExtensionsAreExactlyTheDeclaredOnes(test *testing.T) {
	test.Parallel()

	if len(Extensions) != 1 {
		test.Fatalf("there are %d extensions, and exactly 1 is declared", len(Extensions))
	}
	for _, extension := range Extensions {
		if extension.Reason == "" {
			test.Errorf("%s's extension %q carries no reason", extension.Rule, extension.Key)
		}
		_, found, optionSchemaForError := For(extension.Rule)
		if optionSchemaForError != nil {
			test.Fatalf("%s: %v", extension.Rule, optionSchemaForError)
		}
		if !found {
			test.Errorf("%s has an extension and no schema", extension.Rule)
		}
	}
}

// boundaries/dependencies' elements load through its extension, are refused without it, and the rest of
// the element is still checked.
func TestTheBoundariesExtensionAcceptsElementsAndNothingElse(test *testing.T) {
	test.Parallel()

	withElements := []json.RawMessage{json.RawMessage(`{"default":"disallow","elements":[{"type":"api","pattern":"api/**"}]}`)}
	if checkError := Check("boundaries/dependencies", withElements); checkError != nil {
		test.Errorf("elements are refused: %v", checkError)
	}
	schema, found, optionSchemaForError := For("boundaries/dependencies")
	if optionSchemaForError != nil {
		test.Fatal(optionSchemaForError)
	}
	if !found {
		test.Fatal("boundaries/dependencies has no schema")
	}
	if validateError := schema.Validate(withElements); validateError == nil {
		test.Errorf("upstream's schema accepts elements without the extension, so the extension is not needed")
	}
	otherKey := []json.RawMessage{json.RawMessage(`{"default":"disallow","elements":[],"notAnUpstreamKey":true}`)}
	if checkError := Check("boundaries/dependencies", otherKey); checkError == nil {
		test.Errorf("a key outside the extension is accepted")
	}
}

// A refusal names the element as a config author counts it, the path inside it, and what upstream
// expected (#pd2chkx condition 8).
func TestARefusalNamesTheElementThePathAndTheExpectation(test *testing.T) {
	test.Parallel()

	checkError := Check("@typescript-eslint/no-deprecated", []json.RawMessage{json.RawMessage(`{"allow":[{"from":"File","name":"x"}]}`)})
	if checkError == nil {
		test.Fatal("a mis-cased from is accepted")
	}
	for _, want := range []string{"element 1 at allow[0]", "upstream's schema, which ESLint validates against, expects"} {
		if !strings.Contains(checkError.Error(), want) {
			test.Errorf("the refusal %q does not say %q", checkError, want)
		}
	}
}
