package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// duplicatesProject is a project holding one computation written three times, one copy kept by a suppression with
// its reason, a suppression that keeps nothing, and a function the hash refuses.
func duplicatesProject(test *testing.T, extra map[string]string) string {
	test.Helper()
	score := func(name string, values string, weight string, total string, product string) string {
		return "export function " + name + "(" + values + ": number[], " + weight + ": number): number {\n" +
			"    let " + total + " = 0;\n" +
			"    for (const value of " + values + ") {\n" +
			"        if (value > 10) {\n" +
			"            " + total + " += " + product + ";\n" +
			"        } else {\n" +
			"            " + total + " -= value;\n" +
			"        }\n" +
			"    }\n" +
			"    const scaled = " + total + " * 3 + " + weight + " - 7;\n" +
			"    return scaled / (" + values + ".length + 1);\n" +
			"}\n"
	}
	root := test.TempDir()
	files := map[string]string{
		"tsconfig.json":       incrementalFixtureConfiguration,
		"CohereSettings.json": `{"rules":{}}`,
		".gitignore":          "tsconfig.tsbuildinfo\n",
		"scoreOne.ts":         score("scoreOne", "values", "weight", "total", "value * weight"),
		"scoreTwo.ts":         score("scoreTwo", "readings", "factor", "sum", "factor * value"),
		"scorePort.ts": "// cohere-disable-next-line nexus/consistency-no-duplicate-function -- port of upstream, " +
			"kept in its shape\n" + score("scorePort", "values", "weight", "total", "value * weight") + "\n" +
			"// cohere-disable-next-line nexus/consistency-no-duplicate-function -- nothing duplicates this\n" +
			"export function lonely(value: number): number {\n    return value + 1;\n}\n\n" +
			"export function loadLater(): Promise<unknown> {\n    return import('./scoreOne');\n}\n",
	}
	for name, contents := range extra {
		files[name] = contents
	}
	writeTree(test, root, files)
	return root
}

// duplicatesLines are a --json run's duplicates lines by kind, and its summary.
type duplicatesLines struct {
	groups   []duplicateJson
	refused  []duplicateRefusedJson
	coverage []duplicateCoverageJson
	summary  map[string]json.RawMessage
}

func readDuplicatesLines(test *testing.T, output string) duplicatesLines {
	test.Helper()
	var lines duplicatesLines
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var envelope struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal([]byte(line), &envelope) != nil {
			continue
		}
		var decodeError error
		switch envelope.Kind {
		case "duplicate":
			var group duplicateJson
			decodeError = json.Unmarshal([]byte(line), &group)
			lines.groups = append(lines.groups, group)
		case "duplicateRefused":
			var refused duplicateRefusedJson
			decodeError = json.Unmarshal([]byte(line), &refused)
			lines.refused = append(lines.refused, refused)
		case "duplicateCoverage":
			var coverage duplicateCoverageJson
			decodeError = json.Unmarshal([]byte(line), &coverage)
			lines.coverage = append(lines.coverage, coverage)
		case "summary":
			decodeError = json.Unmarshal([]byte(line), &lines.summary)
		}
		if decodeError != nil {
			test.Fatalf("decoding %s: %v", line, decodeError)
		}
	}
	return lines
}

// The report over a real run: the three copies are one group, the port kept with its reason, the suppression that
// keeps nothing listed as dead and never a finding, `import()` refused by name, and a second run reads every file
// from the cache and hashes none, with the same report.
func TestTheTypeScriptDuplicatesReport(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := duplicatesProject(test, nil)
	output, exitCode := runCohere(test, binary, root, "--no-fix", "--no-format", "--json", "--duplicates")
	if exitCode != 0 {
		test.Fatalf("exit %d on a clean project:\n%s", exitCode, output)
	}
	first := readDuplicatesLines(test, output)
	if len(first.groups) != 1 || len(first.coverage) != 1 {
		test.Fatalf("%d groups and %d coverage lines, where one of each was expected:\n%s", len(first.groups),
			len(first.coverage), output)
	}
	var members []string
	for _, member := range first.groups[0].Members {
		described := filepath.Base(member.Path) + " " + member.Name
		if member.Kept != "" {
			described += " kept: " + member.Kept
		}
		members = append(members, described)
	}
	expected := []string{"scoreOne.ts scoreOne", "scorePort.ts scorePort kept: port of upstream, kept in its shape",
		"scoreTwo.ts scoreTwo"}
	if !slices.Equal(members, expected) || first.groups[0].Size < 11 {
		test.Errorf("the group is %v at size %d, where %v at 11 and up was expected", members, first.groups[0].Size,
			expected)
	}
	coverage := first.coverage[0]
	if !coverage.Ran || coverage.Files != 3 || coverage.Hashed != 4 || coverage.Refused["dynamicImport"] != 1 {
		test.Errorf("the coverage is %+v, where 3 files, 4 hashed and one import() refused were expected", coverage)
	}
	if len(first.refused) != 1 || first.refused[0].Name != "loadLater" {
		test.Errorf("the refused lines are %+v, where loadLater alone was expected", first.refused)
	}
	if len(coverage.DeadSuppressions) != 1 || coverage.DeadSuppressions[0].Line != 15 {
		test.Errorf("the dead suppressions are %+v, where the one above lonely, line 15, was expected",
			coverage.DeadSuppressions)
	}
	if coverage.Cache.Hashed != 3 || coverage.Cache.Read != 0 {
		test.Errorf("a first run read %d files from the cache and hashed %d, where it hashed all 3",
			coverage.Cache.Read, coverage.Cache.Hashed)
	}

	output, exitCode = runCohere(test, binary, root, "--no-fix", "--no-format", "--json", "--duplicates")
	second := readDuplicatesLines(test, output)
	if exitCode != 0 || len(second.coverage) != 1 {
		test.Fatalf("the second run exited %d:\n%s", exitCode, output)
	}
	if second.coverage[0].Cache.Read != 3 || second.coverage[0].Cache.Hashed != 0 {
		test.Errorf("a second run over unchanged files read %d from the cache and hashed %d, where it read all 3",
			second.coverage[0].Cache.Read, second.coverage[0].Cache.Hashed)
	}
	firstGroups, firstError := json.Marshal(first.groups)
	secondGroups, secondError := json.Marshal(second.groups)
	if firstError != nil || secondError != nil {
		test.Fatalf("encoding the groups: %v, %v", firstError, secondError)
	}
	if string(firstGroups) != string(secondGroups) {
		test.Errorf("the cached run grouped differently:\n%s\n%s", firstGroups, secondGroups)
	}
}

// A file's cached functions are keyed on its shape fingerprint, and never served past what its digests read: an edit
// inside a function's body re-hashes that function, and a re-export that starts naming another declaration re-hashes
// the file that calls through it, whose callee is now another. Each is seen in what the report says, not only in what
// the cache counts: the edited copy leaves its group, and the caller starts duplicating a function that calls the new
// callee directly.
func TestTheTypeScriptDuplicatesCacheMovesWithWhatADigestReads(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	callScore := func(name string, imported string, from string) string {
		return "import { " + imported + " } from '" + from + "';\n\nexport function " + name +
			"(values: number[]): number {\n    return " + imported + "(values, 2) + 1;\n}\n"
	}
	root := duplicatesProject(test, map[string]string{
		"barrel.ts":    "export { scoreOne as score } from './scoreOne';\n",
		"caller.ts":    callScore("callScore", "score", "./barrel"),
		"callerTwo.ts": callScore("callScoreTwo", "scoreTwo", "./scoreTwo"),
	})
	report := func(step string) duplicatesLines {
		test.Helper()
		output, exitCode := runCohere(test, binary, root, "--no-fix", "--no-format", "--json", "--duplicates")
		lines := readDuplicatesLines(test, output)
		if exitCode != 0 || len(lines.coverage) != 1 {
			test.Fatalf("%s: exit %d:\n%s", step, exitCode, output)
		}
		return lines
	}
	members := func(lines duplicatesLines) []string {
		var names []string
		for _, group := range lines.groups {
			for _, member := range group.Members {
				names = append(names, member.Name)
			}
		}
		slices.Sort(names)
		return names
	}
	if first := report("the first run"); first.coverage[0].Cache.Hashed != 6 {
		test.Fatalf("the first run hashed %d files, where all 6 were expected", first.coverage[0].Cache.Hashed)
	}
	// A first run's shapes are its files' bytes until their declaration signatures graduate after its verdict, so the
	// run after it keys the importers anew once. From there the keys hold.
	report("the run after the first, as the shapes graduate")
	settled := report("a warm run")
	if settled.coverage[0].Cache.Hashed != 0 || settled.coverage[0].Cache.Read != 6 {
		test.Fatalf("a warm run over unchanged files hashed %d and read %d, where it read all 6",
			settled.coverage[0].Cache.Hashed, settled.coverage[0].Cache.Read)
	}

	writeTree(test, root, map[string]string{"scoreOne.ts": strings.Replace(readForTest(test,
		filepath.Join(root, "scoreOne.ts")), "value > 10", "value > 11", 1)})
	edited := report("after a body edit")
	if names := members(edited); !slices.Equal(names, []string{"scorePort", "scoreTwo"}) {
		test.Errorf("after scoreOne's body changed, the groups hold %v, where scoreOne left them", names)
	}

	writeTree(test, root, map[string]string{"barrel.ts": "export { scoreTwo as score } from './scoreTwo';\n"})
	retargeted := report("after the re-export moved")
	if retargeted.coverage[0].BelowFloor != edited.coverage[0].BelowFloor+1 {
		test.Errorf("after the barrel's re-export moved to scoreTwo, %d groups were under the floor, where %d were: "+
			"callScore calls scoreTwo now, as callScoreTwo does, and a stale entry hides that",
			retargeted.coverage[0].BelowFloor, edited.coverage[0].BelowFloor+1)
	}
}
