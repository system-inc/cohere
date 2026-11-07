package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
	"github.com/system-inc/cohere/policy"
)

// A dead ignore is a finding that fails the run (#cxeq6jw), at the entry a settings file wrote, unless the entry
// names generated output, which may not have been built yet, or sits in a set cohere carries.

// unusedIgnoreFixture is one TypeScript project with a live generated/ ignore beside the entries given.
func unusedIgnoreFixture(test *testing.T, entries string) string {
	test.Helper()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json": incrementalFixtureConfiguration,
		"CohereSettings.json": `{"rules": {}, "ignorePatterns": [{"pattern": "generated/**", ` +
			`"reason": "Written by the schema tool.", "kind": "generated"}` + entries + `]}`,
		"index.ts":            "export const value = 1;\n",
		"generated/Schema.ts": "export const schema = 2;\n",
		".gitignore":          "tsconfig.tsbuildinfo\n",
	})
	return root
}

func TestADeadIgnoreFailsTheRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)

	test.Run("a dead glob is a finding at its file", func(test *testing.T) {
		test.Parallel()
		root := unusedIgnoreFixture(test, `, "missing/**"`)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 1 {
			test.Fatalf("exit %d with an ignore that matches nothing, want 1:\n%s", code, output)
		}
		// The settings are one line, so the entry's line is the first.
		if !strings.Contains(output, filepath.Join(root, "CohereSettings.json")+":1:") ||
			!strings.Contains(output, `Unused ignore pattern "missing/**" (no path here matched it). [`+
				unusedIgnoreRule+`/unusedIgnore]`) {
			test.Errorf("the dead entry was not a finding at its file:\n%s", output)
		}
		if strings.Contains(output, `Unused ignore pattern "generated/**"`) {
			test.Errorf("the live entry was reported as dead:\n%s", output)
		}
		if !strings.Contains(output, "1 finding") {
			test.Errorf("the footer does not count the dead entry as a finding:\n%s", output)
		}
	})

	test.Run("a live entry is not", func(test *testing.T) {
		test.Parallel()
		output, code := runCohere(test, binary, unusedIgnoreFixture(test, ""), "--no-fix", "--no-format", "--no-cache")
		if code != 0 || strings.Contains(output, unusedIgnoreRule) {
			test.Errorf("exit %d, and a live entry read as dead:\n%s", code, output)
		}
	})

	test.Run("a generated entry that matches nothing is listed, not a finding", func(test *testing.T) {
		test.Parallel()
		root := unusedIgnoreFixture(test, `, {"pattern": "dist/**", "reason": "Built later.", "kind": "generated"}`)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 0 || strings.Contains(output, unusedIgnoreRule) {
			test.Errorf("exit %d, and build output not built yet read as dead:\n%s", code, output)
		}
		if !strings.Contains(output, `ignore: "dist/**" in CohereSettings.json, generated, matched 0 paths`) {
			test.Errorf("--verbose did not list the generated entry with its zero:\n%s", output)
		}
	})

	test.Run("an input entry that matches nothing is a finding", func(test *testing.T) {
		test.Parallel()
		root := unusedIgnoreFixture(test, `, {"pattern": "fixtures/**", "reason": "Recorded.", "kind": "input"}`)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 1 || !strings.Contains(output, `Unused ignore pattern "fixtures/**"`) {
			test.Errorf("exit %d, and an input entry that matches nothing was not a finding:\n%s", code, output)
		}
	})

	test.Run("--json carries the dead entry as a finding", func(test *testing.T) {
		test.Parallel()
		root := unusedIgnoreFixture(test, `, "missing/**"`)
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache", "--json")
		if code != 1 {
			test.Fatalf("exit %d, want 1:\n%s", code, output)
		}
		var summary summaryJson
		var findings []findingJson
		for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
			var kind struct {
				Kind string `json:"kind"`
			}
			if json.Unmarshal([]byte(line), &kind) != nil {
				continue
			}
			switch kind.Kind {
			case "summary":
				if decodeError := json.Unmarshal([]byte(line), &summary); decodeError != nil {
					test.Fatal(decodeError)
				}
			case "finding":
				var finding findingJson
				if decodeError := json.Unmarshal([]byte(line), &finding); decodeError != nil {
					test.Fatal(decodeError)
				}
				findings = append(findings, finding)
			}
		}
		settings := filepath.Join(root, "CohereSettings.json")
		if summary.Verdict != "fail" || summary.Findings != 1 {
			test.Errorf("the summary reads %s with %d findings, want fail with 1", summary.Verdict, summary.Findings)
		}
		if len(findings) != 1 || findings[0].Rule != unusedIgnoreRule || findings[0].Path != settings {
			test.Errorf("the findings are %+v, want the dead entry's alone, at %s", findings, settings)
		}
	})

	test.Run("a glob that names a directory without taking it whole is dead", func(test *testing.T) {
		test.Parallel()
		root := unusedIgnoreFixture(test, `, "**/*.snapshot"`)
		writeTree(test, root, map[string]string{"cases.snapshot/Case.ts": "export const snapshotCase = 2;\n"})
		output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
		if code != 1 || !strings.Contains(output, `Unused ignore pattern "**/*.snapshot"`) {
			test.Errorf("exit %d, and the glob that ignores nothing is not a finding:\n%s", code, output)
		}
	})
}

// With several projects the dead entry is the run's own finding, and the overall verdict fails on it whatever the
// projects found.
func TestSeveralProjectsFailOnADeadIgnore(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"CohereSettings.json":  `{"rules": {}, "ignorePatterns": ["missing/**"]}`,
		"first/tsconfig.json":  incrementalFixtureConfiguration,
		"first/index.ts":       "export const first = 1;\n",
		"second/tsconfig.json": incrementalFixtureConfiguration,
		"second/index.ts":      "export const second = 2;\n",
		".gitignore":           "tsconfig.tsbuildinfo\n",
	})
	output, code := runCohere(test, binary, root, "--no-fix", "--no-format", "--no-cache")
	if code != 1 {
		test.Fatalf("exit %d with a dead ignore over green projects, want 1:\n%s", code, output)
	}
	if !strings.Contains(output, "1 unused ignore pattern") {
		test.Errorf("the overall footer does not say the dead entry:\n%s", output)
	}
}

// An entry a set carries is a shape for every project that extends it, which no project can delete, and a
// generated one names output that may not exist yet, so neither is a finding. Any other entry on disk is.
func TestOnlyAnEntryOnDiskThatIsNotGeneratedIsAFinding(test *testing.T) {
	test.Parallel()
	settings := "/repository/CohereSettings.json"
	for name, check := range map[string]struct {
		account ignoreAccount
		unused  bool
	}{
		"a set's":           {ignoreAccount{Pattern: "**/.next/**", File: configuration.SetPrefix + "next"}, false},
		"a dead glob":       {ignoreAccount{Pattern: "dist/**", File: settings}, true},
		"a live glob":       {ignoreAccount{Pattern: "data/**", File: settings, Matched: 4}, false},
		"a dead generated":  {ignoreAccount{Pattern: "dist/**", Kind: "generated", File: settings}, false},
		"a dead input":      {ignoreAccount{Pattern: "data/**", Kind: "input", File: settings}, true},
		"a dead thirdParty": {ignoreAccount{Pattern: "vendor/**", Kind: "thirdParty", File: settings}, true},
	} {
		if got := ignoreIsUnused(check.account); got != check.unused {
			test.Errorf("%s: unused %v, want %v", name, got, check.unused)
		}
	}
}

// The finding points at the entry itself, and at each of a pattern written twice.
func TestADeadEntryIsFoundWhereItIsWritten(test *testing.T) {
	test.Parallel()
	settings := filepath.Join(test.TempDir(), "CohereSettings.json")
	contents := "{\n    \"rules\": { \"dist/**\": \"off\" },\n    \"ignorePatterns\": [\n        \"dist/**\",\n" +
		"        { \"pattern\": \"dist/**\", \"reason\": \"twice\" }\n    ]\n}\n"
	if writeError := os.WriteFile(settings, []byte(contents), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	for occurrence, want := range [][2]int{{4, 9}, {5, 22}} {
		line, column := entryPosition(settings, "dist/**", occurrence)
		if line != want[0] || column != want[1] {
			test.Errorf("occurrence %d at %d:%d, want %d:%d", occurrence, line, column, want[0], want[1])
		}
	}
	if line, column := entryPosition(settings, "absent/**", 0); line != 1 || column != 1 {
		test.Errorf("a pattern not written is at %d:%d, want the file's start", line, column)
	}
}

// The rule's name is the house scheme's (policy/RuleNaming.json), so it reads like every rule cohere writes.
func TestTheDeadIgnoreRuleIsNamedByTheScheme(test *testing.T) {
	test.Parallel()
	if _, parseError := policy.Naming.ParseHouseRuleName(unusedIgnoreRule); parseError != nil {
		test.Error(parseError)
	}
}
