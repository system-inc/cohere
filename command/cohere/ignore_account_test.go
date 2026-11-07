package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every ignore says what it kept out of the run (#cxeq6jw): --verbose lists each entry with its reason, kind,
// file and how many paths it matched, a count of zero included, and --json carries the same. Each entry that
// matches nothing here is generated output, which is never a dead-ignore finding, so these hold whether or not
// the finding is in force.

// ignoreFixtureSettings ignores a directory that exists and one that does not, both build output.
const ignoreFixtureSettings = `{"rules": {}, "ignorePatterns": [` +
	`{"pattern": "generated/**", "reason": "Written by the schema tool.", "kind": "generated"}, ` +
	`{"pattern": "build/**", "reason": "Build output, not built yet.", "kind": "generated"}]}`

// ignoreFixture is one TypeScript project whose generated/ directory the settings ignore.
func ignoreFixture(test *testing.T) string {
	test.Helper()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":        incrementalFixtureConfiguration,
		"CohereSettings.json":  ignoreFixtureSettings,
		"index.ts":             "export const value = 1;\n",
		"generated/Schema.ts":  "export const schema = 2;\n",
		".gitignore":           "tsconfig.tsbuildinfo\n",
		"generated/Schema2.ts": "export const other = 3;\n",
	})
	return root
}

func TestVerboseListsEveryIgnoreWithItsCount(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)

	test.Run("every entry, a count of zero included", func(test *testing.T) {
		test.Parallel()
		root := ignoreFixture(test)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 0 {
			test.Fatalf("exit %d, want 0:\n%s", code, output)
		}
		// generated/ itself, taken whole, and its two files.
		for _, want := range []string{
			`ignore: "generated/**" in CohereSettings.json, generated, matched 3 paths: Written by the schema tool.`,
			`ignore: "build/**" in CohereSettings.json, generated, matched 0 paths: Build output, not built yet.`,
		} {
			if !strings.Contains(output, want) {
				test.Errorf("--verbose did not list %q:\n%s", want, output)
			}
		}
	})

	test.Run("--json carries the list on the summary", func(test *testing.T) {
		test.Parallel()
		root := ignoreFixture(test)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache", "--json")
		if code != 0 {
			test.Fatalf("exit %d, want 0:\n%s", code, output)
		}
		var summary summaryJson
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			var kind struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal([]byte(line), &kind) != nil || kind.Kind != "summary" {
				continue
			}
			if decodeError := json.Unmarshal([]byte(line), &summary); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
		settings := filepath.Join(root, "CohereSettings.json")
		want := []ignoreAccount{
			{
				Pattern: "generated/**",
				Reason:  "Written by the schema tool.",
				Kind:    "generated",
				File:    settings,
				Matched: 3,
			},
			{Pattern: "build/**", Reason: "Build output, not built yet.", Kind: "generated", File: settings},
		}
		if !reflect.DeepEqual(summary.IgnorePatterns, want) {
			test.Errorf("the summary's ignorePatterns are\n  %+v\nwant\n  %+v\n%s",
				summary.IgnorePatterns, want, output)
		}
		if summary.Verdict != "pass" {
			test.Errorf("the summary reads %s, want pass", summary.Verdict)
		}
	})

	// Lint matches a glob against files, so `**/*.snapshot` ignores nothing inside a directory that happens to be
	// named cases.snapshot, and discovery does not count the directory for it.
	test.Run("a directory a glob names without taking it whole is not a match", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json": incrementalFixtureConfiguration,
			"CohereSettings.json": `{"rules": {}, "ignorePatterns": [{"pattern": "**/*.snapshot", ` +
				`"reason": "Snapshots a test writes.", "kind": "generated"}]}`,
			"index.ts":               "export const value = 1;\n",
			"cases.snapshot/Case.ts": "export const snapshotCase = 2;\n",
			".gitignore":             "tsconfig.tsbuildinfo\n",
		})
		output, _ := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if !strings.Contains(output, `ignore: "**/*.snapshot" in CohereSettings.json, generated, matched 0 paths`) {
			test.Errorf("the directory named cases.snapshot was counted for the glob:\n%s", output)
		}
	})

	// A submodule is checked as part of the program above it, so an ignore that matches only inside one keeps its
	// files out of the run, as api-phi-health's `**/graphql/schemas/**` is in its libraries. Inside a repository of
	// its own the count does not enter a directory a pattern takes whole, so schemas/ counts once.
	test.Run("an entry that matches only inside a nested repository counts its paths", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":       incrementalFixtureConfiguration,
			"CohereSettings.json": `{"rules": {}, "ignorePatterns": ["**/graphql/schemas/**"]}`,
			"index.ts":            "export const value = 1;\n",
			".gitignore":          "tsconfig.tsbuildinfo\n",
			"library/.git":        "gitdir: ../.git/modules/library\n",
			"library/source/graphql/schemas/Schema.ts": "export const schema = 2;\n",
		})
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 0 {
			test.Errorf("exit %d, want 0:\n%s", code, output)
		}
		if !strings.Contains(output, `ignore: "**/graphql/schemas/**" in CohereSettings.json, matched 1 path`) {
			test.Errorf("--verbose did not count the submodule's paths:\n%s", output)
		}
	})

	// Discovery never enters testdata, and a program's include reaches it all the same, as adamic's fixtures are, so an
	// entry that names a path there is counted where it points. Exit 0 says the program held the fixture and the entry
	// kept it out: the folder rule reports a file under testdata/ otherwise.
	test.Run("an entry naming a path the walk never enters counts its paths", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeTree(test, root, map[string]string{
			"tsconfig.json":                     incrementalFixtureConfiguration,
			"CohereSettings.json":               `{"rules": {}, "ignorePatterns": ["source/testdata/refuse/**"]}`,
			"index.ts":                          "export const value = 1;\n",
			".gitignore":                        "tsconfig.tsbuildinfo\n",
			"source/testdata/refuse/Refused.ts": "export const refused = 2;\n",
		})
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 0 {
			test.Errorf("exit %d, want 0:\n%s", code, output)
		}
		if !strings.Contains(output, `ignore: "source/testdata/refuse/**" in CohereSettings.json, matched 1 path`) {
			test.Errorf("--verbose did not count the directory the entry names:\n%s", output)
		}
	})
}

// With several projects each summary is a project's, so the list is a record of the run's own, written once.
func TestSeveralProjectsCarryTheIgnoresOnce(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"CohereSettings.json":    ignoreFixtureSettings,
		"first/tsconfig.json":    incrementalFixtureConfiguration,
		"first/index.ts":         "export const first = 1;\n",
		"second/tsconfig.json":   incrementalFixtureConfiguration,
		"second/index.ts":        "export const second = 2;\n",
		"generated/Schema.ts":    "export const schema = 2;\n",
		".gitignore":             "tsconfig.tsbuildinfo\n",
		"generated/Generated.ts": "export const generated = 3;\n",
	})

	output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
	if code != 0 {
		test.Fatalf("exit %d, want 0:\n%s", code, output)
	}
	if !strings.Contains(output, `ignore: "generated/**" in CohereSettings.json, generated, matched 3 paths`) {
		test.Errorf("--verbose did not list the live entry:\n%s", output)
	}

	output, code = runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache", "--json")
	if code != 0 {
		test.Fatalf("exit %d under --json, want 0:\n%s", code, output)
	}
	records := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record ignorePatternsJson
		if json.Unmarshal([]byte(line), &record) != nil || record.Kind != "ignorePatterns" {
			continue
		}
		records++
		if len(record.IgnorePatterns) != 2 || record.IgnorePatterns[1].Matched != 0 {
			test.Errorf("the record holds %+v, want both entries, the second at 0", record.IgnorePatterns)
		}
	}
	if records != 1 {
		test.Errorf("%d ignorePatterns records, want 1:\n%s", records, output)
	}
}

// The schema describes what the summary's list and the run's own record write, field for field, as
// TestOutputSchemaMatchesTheTypes holds the other records to. Both lists name one entry definition by reference.
func TestTheOutputSchemaDescribesTheIgnores(test *testing.T) {
	test.Parallel()
	raw, readError := os.ReadFile("../../schema/CohereOutput.schema.json")
	if readError != nil {
		test.Fatal(readError)
	}
	type referenced struct {
		Reference string `json:"$ref"`
	}
	var schema struct {
		OneOf []referenced            `json:"oneOf"`
		Defs  map[string]schemaObject `json:"$defs"`
	}
	if decodeError := json.Unmarshal(raw, &schema); decodeError != nil {
		test.Fatal(decodeError)
	}
	var references struct {
		Defs struct {
			Summary struct {
				Properties struct {
					IgnorePatterns struct {
						Items referenced `json:"items"`
					} `json:"ignorePatterns"`
				} `json:"properties"`
			} `json:"summary"`
			IgnorePatterns struct {
				Properties struct {
					IgnorePatterns struct {
						Items referenced `json:"items"`
					} `json:"ignorePatterns"`
				} `json:"properties"`
			} `json:"ignorePatterns"`
		} `json:"$defs"`
	}
	if decodeError := json.Unmarshal(raw, &references); decodeError != nil {
		test.Fatal(decodeError)
	}
	entry := "#/$defs/ignoreEntry"
	if references.Defs.Summary.Properties.IgnorePatterns.Items.Reference != entry ||
		references.Defs.IgnorePatterns.Properties.IgnorePatterns.Items.Reference != entry {
		test.Errorf("the summary's list and the record's do not both name %s: %+v", entry, references.Defs)
	}
	recorded := false
	for _, line := range schema.OneOf {
		recorded = recorded || line.Reference == "#/$defs/ignorePatterns"
	}
	if !recorded {
		test.Error("the schema's lines do not include the ignorePatterns record")
	}
	for name, check := range map[string]struct {
		object schemaObject
		value  any
	}{
		"ignoreEntry":    {schema.Defs["ignoreEntry"], ignoreAccount{}},
		"ignorePatterns": {schema.Defs["ignorePatterns"], ignorePatternsJson{}},
	} {
		fields, required := jsonFields(reflect.TypeOf(check.value))
		if got := schemaPropertyNames(check.object.Properties); strings.Join(got, ",") != strings.Join(fields, ",") {
			test.Errorf("%s: the schema's properties are %v, the type writes %v", name, got, fields)
		}
		schemaRequired := append([]string(nil), check.object.Required...)
		sort.Strings(schemaRequired)
		if strings.Join(schemaRequired, ",") != strings.Join(required, ",") {
			test.Errorf("%s: the schema requires %v, the type always writes %v", name, schemaRequired, required)
		}
	}
}

// The list under --verbose names a set by its name and a file relative to the root, and says when an entry gave
// no reason.
func TestAnIgnoreLineSaysWhereItCameFromAndWhy(test *testing.T) {
	test.Parallel()
	var buffer strings.Builder
	out := bufio.NewWriter(&buffer)
	for _, account := range []ignoreAccount{
		{
			Pattern:   "**/.next/**",
			Reason:    "Next's build output.",
			Kind:      "generated",
			File:      "cohere:next",
			Matched:   12,
			shownFile: "cohere:next",
		},
		{
			Pattern:   "data/**",
			Check:     "types",
			File:      "/repository/CohereSettings.json",
			Matched:   1,
			shownFile: "CohereSettings.json",
		},
	} {
		if _, writeError := out.WriteString(account.line()); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if flushError := out.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
	want := `  ignore: "**/.next/**" in cohere:next, generated, matched 12 paths: Next's build output.` + "\n" +
		`  ignore: "data/**" in CohereSettings.json, read by the types check, matched 1 path: (no reason)` + "\n"
	if buffer.String() != want {
		test.Errorf("got\n%s\nwant\n%s", buffer.String(), want)
	}
}
