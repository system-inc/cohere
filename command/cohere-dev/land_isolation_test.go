package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// plantedInputs is the planted inventory table: alpha a house rule, SA0001 gamma's port and delta delta's in the
// registry's ports table, and U9999 zeta's, which only zeta's code spells.
const plantedInputs = "# The planted inventory.\n" +
	"cohere-go/SA0001 Composes\n" +
	"cohere-go/U9999 Composes\n" +
	"cohere-go/alpha PerUnit\n" +
	"cohere-go/delta Composes\n"

// plantedRules is a module's rules as the isolation step reads them: alpha and beta import shared, which imports
// deep; gamma imports nothing; delta's fixture is a module of its own; registry.go sits in the rules directory itself,
// importing gamma and delta for its ports table, with the inventory's table and a fixture of its own in its testdata,
// and other.go outside it. Each rule directory has a testdata directory, and shared and deep have none.
var plantedRules = map[string]string{
	"internal/go_engine/rules/rule_inputs.txt":                              plantedInputs,
	"internal/go_engine/rules/testdata/src/unused_fixture/fixture.go":       "package unused_fixture\n",
	"internal/go_engine/rules/epsilon_rule/analyzer.go":                     "package epsilon_rule\n",
	"internal/go_engine/rules/epsilon_rule/testdata/src/flagged/flagged.go": "package flagged\n",
	"internal/go_engine/rules/zeta/analyzer.go":                             "package zeta\n\nconst Name = \"U9999\"\n",
	"internal/go_engine/rules/zeta/testdata/src/flagged/flagged.go":         "package flagged\n",
	"go.mod": "module example.com/planted\n\ngo 1.24\n",
	"internal/go_engine/rules/alpha/analyzer.go": "package alpha\n\n" +
		"import _ \"example.com/planted/internal/go_engine/rules/shared\"\n",
	"internal/go_engine/rules/alpha/testdata/src/flagged/flagged.go": "package flagged\n",
	"internal/go_engine/rules/beta/analyzer.go": "package beta\n\n" +
		"import _ \"example.com/planted/internal/go_engine/rules/shared\"\n",
	"internal/go_engine/rules/beta/testdata/src/flagged/flagged.go":  "package flagged\n",
	"internal/go_engine/rules/gamma/analyzer.go":                     "package gamma\n",
	"internal/go_engine/rules/gamma/analyzer_test.go":                "package gamma\n",
	"internal/go_engine/rules/gamma/testdata/src/flagged/flagged.go": "package flagged\n",
	"internal/go_engine/rules/vet/delta/analyzer.go":                 "package delta\n",
	"internal/go_engine/rules/vet/delta/testdata/parity/go.mod":      "module example.com/parity\n",
	"internal/go_engine/rules/vet/delta/testdata/parity/parity.go":   "package parity\n",
	"internal/go_engine/rules/shared/shared.go": "package shared\n\n" +
		"import _ \"example.com/planted/internal/go_engine/rules/deep\"\n",
	"internal/go_engine/rules/deep/deep.go": "package deep\n",
	"internal/go_engine/rules/registry.go": "package rules\n\nimport (\n" +
		"\t\"example.com/planted/internal/go_engine/rules/gamma\"\n" +
		"\t\"example.com/planted/internal/go_engine/rules/vet/delta\"\n)\n\n" +
		"var ports = map[string]port{\n\t\"SA0001\": {analyzer: gamma.Analyzer, listener: &gamma.Listener},\n" +
		"\t\"delta\": {analyzer: delta.Analyzer},\n}\n",
	"internal/other/other.go": "package other\n",
}

// plantedChange commits plantedRules in a repository of its own, then writes each of edits' files with its contents,
// and commits that too, as a landing's merge: the repository, and the second commit.
func plantedChange(test *testing.T, edits map[string]string) (string, string) {
	test.Helper()
	repository := test.TempDir()
	run := func(arguments ...string) string {
		command := exec.Command("git", arguments...)
		command.Dir = repository
		output, runError := command.CombinedOutput()
		if runError != nil {
			test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), runError, output)
		}
		return strings.TrimSpace(string(output))
	}
	for name, contents := range plantedRules {
		path := filepath.Join(repository, filepath.FromSlash(name))
		if createError := os.MkdirAll(filepath.Dir(path), 0o755); createError != nil {
			test.Fatal(createError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	run("init", "-q")
	run("-c", "user.name=planted", "-c", "user.email=planted@example.com", "add", ".")
	run("-c", "user.name=planted", "-c", "user.email=planted@example.com", "commit", "-q", "-m", "base")
	for file, contents := range edits {
		changed := filepath.Join(repository, filepath.FromSlash(file))
		if writeError := os.WriteFile(changed, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	run("-c", "user.name=planted", "-c", "user.email=planted@example.com", "commit", "-q", "-a", "-m", "change")
	return repository, run("rev-parse", "HEAD")
}

// A landing isolates the rule directories its diff against main touches: a rule's own file, test file included, and
// a file of its fixtures each isolate that rule; a shared helper under the rules isolates every rule that imports
// it, through another helper too; and a file outside the rules, or the registry in the rules directory itself,
// isolates none, leaving the module's tests their core of the isolation. The registry imports gamma, and gamma's
// change isolates gamma alone. A change to the inventory's table isolates each rule whose declaration it widens: a
// port's line flipped to PerUnit or FileSyntax, found through the registry's ports table, a house rule's new line,
// found by its name, and a rule only its own code spells; a flip to Composes isolates nothing.
func TestALandIsolatesTheRuleDirectoriesItsDiffTouches(test *testing.T) {
	test.Parallel()
	rules := "internal/go_engine/rules/"
	changed := func(file string) map[string]string {
		return map[string]string{file: plantedRules[file] + "// changed\n"}
	}
	flipped := func(from string, to string) map[string]string {
		return map[string]string{rules + "rule_inputs.txt": strings.Replace(plantedInputs, from, to, 1)}
	}
	for _, planted := range []struct {
		name  string
		edits map[string]string
		want  []string
	}{
		{"a rule's file", changed(rules + "gamma/analyzer.go"), []string{rules + "gamma"}},
		{"a rule's test file", changed(rules + "gamma/analyzer_test.go"), []string{rules + "gamma"}},
		{"a fixture's file", changed(rules + "alpha/testdata/src/flagged/flagged.go"), []string{rules + "alpha"}},
		{"a module fixture's file", changed(rules + "vet/delta/testdata/parity/go.mod"), []string{rules + "vet/delta"}},
		{"a shared helper", changed(rules + "shared/shared.go"), []string{rules + "alpha", rules + "beta"}},
		{"a helper's helper", changed(rules + "deep/deep.go"), []string{rules + "alpha", rules + "beta"}},
		{"the registry", changed(rules + "registry.go"), nil},
		{"a file outside the rules", changed("internal/other/other.go"), nil},
		{"a port flipped to PerUnit", flipped("SA0001 Composes", "SA0001 PerUnit"), []string{rules + "gamma"}},
		{"a port flipped to FileSyntax", flipped("delta Composes", "delta FileSyntax"), []string{rules + "vet/delta"}},
		{"a house rule's new line", flipped("cohere-go/alpha PerUnit\n",
			"cohere-go/alpha PerUnit\ncohere-go/epsilon-rule PerUnit\n"), []string{rules + "epsilon_rule"}},
		{"a spelled rule flipped", flipped("U9999 Composes", "U9999 PerUnit"), []string{rules + "zeta"}},
		{"a flip to Composes", flipped("alpha PerUnit", "alpha Composes"), nil},
	} {
		repository, commit := plantedChange(test, planted.edits)
		isolated, isolatedError := landingRuleDirectories(repository, commit)
		if isolatedError != nil || !slices.Equal(isolated, planted.want) {
			test.Errorf("%s: want %q isolated, got %q (%v)", planted.name, planted.want, isolated, isolatedError)
		}
	}
}

// The step runs the isolation test by name and hands it its directories by variable, so the test must have that name
// and read both variables: a -run that names no test runs none and passes.
func TestTheGateRunsTheIsolationTestThatExists(test *testing.T) {
	test.Parallel()
	source, readError := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(unitIsolationFile)))
	if readError != nil {
		test.Fatal(readError)
	}
	for _, wanted := range []string{"func " + unitIsolationTest + "(test *testing.T)", `"COHERE_UNIT_ISOLATION"`,
		`"COHERE_UNIT_ISOLATION_RULES"`, `setting != "rules"`} {
		if !strings.Contains(string(source), wanted) {
			test.Errorf("%s holds no %s, which the gate's step relies on", unitIsolationFile, wanted)
		}
	}
}
