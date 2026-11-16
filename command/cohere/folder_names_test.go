package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/configuration"
)

// folderDirectories are the findings' folders, in the order discovery keeps them.
func folderDirectories(findings []folderFinding) []string {
	directories := []string{}
	for _, finding := range findings {
		directories = append(directories, finding.Directory)
	}
	return directories
}

// The walk judges every folder it enters, not only those holding a checked file, so a docs/ of Markdown alone is
// reported, at the folder, and only the outermost offending folder is, once: a docs/ below a reported docs/ is not
// reported again (#zyq63kk). documentation/ and the formatter's internal/format/doc/ pass, and google/docs is Google
// Docs. Folders are judged by policy/NameWords.json alone, so src/docs/ reports docs, and a folder of plain words the
// shared vocabulary reads as code, Op art and a stage prop, passes: judging by the shared vocabulary turns this red.
func TestTheFolderCheckReportsTheOutermostAbbreviatedFolder(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"go.mod":                            "module example.com/root\n",
		"docs/README.md":                    "# Documentation\n",
		"docs/api/docs/inner.md":            "# Inner\n",
		"source/docs/guide.md":              "# Guide\n",
		"src/docs/guide.md":                 "# Guide\n",
		"documentation/README.md":           "# Documentation\n",
		"internal/format/doc/printer.md":    "# Printer\n",
		"modules/google/docs/GoogleDocs.md": "# Google Docs\n",
		"modules/api-docs/README.md":        "# Api\n",
		"styles/op-art-one/README.md":       "# Op art\n",
		"universes/stage-prop/README.md":    "# Stage prop\n",
	})
	found, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	want := []string{"docs", "modules/api-docs", "source/docs", "src/docs"}
	if got := folderDirectories(found.Folders); !reflect.DeepEqual(got, want) {
		test.Fatalf("reported %v, want %v", got, want)
	}
	for _, finding := range found.Folders {
		if finding.Directory == "docs" && (finding.MessageId != "abbreviatedFolderName" ||
			!strings.HasPrefix(finding.Message, `Folder name "docs" abbreviates "docs". Spell it out as `+
				`"documentation". A folder's name is read on every path`)) {
			test.Errorf("docs/ reads %s %q", finding.MessageId, finding.Message)
		}
		if finding.Directory == "src/docs" && !strings.Contains(finding.Message, `Folder name "docs" abbreviates`) {
			test.Errorf("src/docs/ reads %q", finding.Message)
		}
	}
}

// `vars` is judged in a folder's name, whole, as a prefix and as a segment, as policy/NameWords.json holds it beside
// the shared vocabulary (#1ykwdb9): Loom's wire/vars/ is the folder Kirk saw. It asks rather than spells out, since it
// can mean JavaScript's var keyword, and variables/ passes.
func TestTheFolderCheckReportsTheVariablesAbbreviation(test *testing.T) {
	test.Parallel()
	root := newRepository(test, map[string]string{
		"go.mod":                               "module example.com/root\n",
		"wire/vars/README.md":                  "# Wire\n",
		"modules/env-vars/README.md":           "# Environment\n",
		"modules/vars-reader/README.md":        "# Reader\n",
		"modules/variables/README.md":          "# Variables\n",
		"environment-variables/README.md":      "# Environment\n",
		"modules/hoisted-var-scoped/README.md": "# Hoisted\n",
	})
	found, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	want := []string{"modules/env-vars", "modules/vars-reader", "wire/vars"}
	if got := folderDirectories(found.Folders); !reflect.DeepEqual(got, want) {
		test.Fatalf("reported %v, want %v", got, want)
	}
	for _, finding := range found.Folders {
		if finding.MessageId != "abbreviatedFolderNameAdvice" || !strings.Contains(finding.Message,
			`abbreviates "vars". Use "Variables" for variables, or a name saying it means JavaScript's var keyword.`) {
			test.Errorf("%s/ reads %s %q", finding.Directory, finding.MessageId, finding.Message)
		}
	}
}

// The folder check keeps the walk's gaps: nothing .gitignore ignores, no node_modules, .build, .cache, testdata, or
// nested repository, none of which the walk enters, and beside them nothing the root's ignorePatterns take whole,
// nothing at or below vendor, Vendor or third_party, and no App Router folder. The control lifts the walk's own
// refusals, and the folders it never entered are then reported, so their silence above proves the gap.
//
// Not parallel: its control swaps the package's neverDescended and newIgnoreScope, which every discovery reads.
func TestTheFolderCheckKeepsTheWalksGaps(test *testing.T) {
	root := newRepository(test, map[string]string{
		"go.mod":                          "module example.com/root\n",
		".gitignore":                      "generated/\n",
		"generated/docs/a.md":             "# a\n",
		"node_modules/package/docs/a.md":  "# a\n",
		".build/docs/a.md":                "# a\n",
		"internal/testdata/docs/a.md":     "# a\n",
		"clone/docs/a.md":                 "# a\n",
		"refused/docs/a.md":               "# a\n",
		"vendor/module/docs/a.md":         "# a\n",
		"Vendor/Library/docs/a.md":        "# a\n",
		"third_party/library/docs/a.md":   "# a\n",
		"app/(docs)/page.md":              "# a\n",
		"app/[docs]/page.md":              "# a\n",
		"app/@docs/page.md":               "# a\n",
		"web/_docs/page.md":               "# a\n",
		"source/parameter-reading/a.md":   "# a\n",
		"source/consistency_no_var/a.md":  "# a\n",
		"source/docstrings/a.md":          "# a\n",
		"source/doc/a.md":                 "# a\n",
		"source/documentation/guide/a.md": "# a\n",
	})
	if createDirectoryError := os.Mkdir(filepath.Join(root, "clone", ".git"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	found, discoverProjectsError := discoverProjects(root, []string{"refused/**"})
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	// A private folder's name after its underscore is the project's, as the separated-words rule reads it.
	want := []string{"web/_docs"}
	if got := folderDirectories(found.Folders); !reflect.DeepEqual(got, want) {
		test.Fatalf("reported %v, want %v", got, want)
	}

	// The control: with nothing never-descended and nothing ignored, the walk enters what it refused.
	previousNames, previousScope := neverDescended, newIgnoreScope
	test.Cleanup(func() { neverDescended, newIgnoreScope = previousNames, previousScope })
	neverDescended = map[string]bool{".git": true}
	newIgnoreScope = func(string) (ignoreScope, error) { return ignoresNothing{}, nil }
	lifted, discoverProjectsError := discoverProjects(root, nil)
	if discoverProjectsError != nil {
		test.Fatal(discoverProjectsError)
	}
	reported := map[string]bool{}
	for _, finding := range lifted.Folders {
		reported[finding.Directory] = true
	}
	// node_modules is foreign to the check as well as never entered, so it stays silent here too.
	for _, planted := range []string{"generated/docs", ".build/docs", "refused/docs"} {
		if !reported[planted] {
			test.Errorf("with the refusals lifted, %s was still not reported, so its silence above proves nothing",
				planted)
		}
	}
}

// The setting is the chain's top-level one: an error where the sets leave it, as cohere:style turns it on for every
// house set that reaches it, and the project's own file warns or turns it off, under zero config as under a chain
// naming its sets. A chain that does not load is an error naming the file, never an off.
func TestTheFolderCheckReadsTheRootsSetting(test *testing.T) {
	test.Parallel()
	for name, testCase := range map[string]struct {
		settings string
		want     configuration.Severity
	}{
		"no settings file, the house stack's error": {"", configuration.SeverityError},
		"a chain naming its sets, cohere:style's error": {
			`{"extends": "cohere:typescript"}`,
			configuration.SeverityError,
		},
		"zero config with the project's own file on top": {
			`{"rules": {"` + configuration.AbbreviatedFolderName + `": "off"}, "reasons": {"` +
				configuration.AbbreviatedFolderName + `": "This project keeps its folder names as they are."}}`,
			configuration.SeverityOff,
		},
		"a chain naming its sets, warned by the project": {
			`{"extends": "cohere:typescript", "rules": {"` + configuration.AbbreviatedFolderName + `": "warn"}}`,
			configuration.SeverityWarn,
		},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			if testCase.settings != "" {
				writeTree(test, root, map[string]string{"CohereSettings.json": testCase.settings})
			}
			got, folderCheckSeverityError := folderCheckSeverity(root, "")
			if folderCheckSeverityError != nil || got != testCase.want {
				test.Errorf("severity %v (%v), want %v", got, folderCheckSeverityError, testCase.want)
			}
		})
	}
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"CohereSettings.json": `{"rules": {"` + configuration.AbbreviatedFolderName + `": "error", ` +
			`"nexus/no-such-rule": "error"}}`,
	})
	if _, refused := folderCheckSeverity(root, ""); refused == nil ||
		!strings.Contains(refused.Error(), filepath.Join(root, "CohereSettings.json")) {
		test.Errorf("a chain that does not load read %v, want an error naming the file", refused)
	}
}

// Only a run that lints reports a folder: one that only formats or only type-checks does not.
func TestTheFolderCheckRunsOnlyWhenTheRunLints(test *testing.T) {
	test.Parallel()
	for given, want := range map[string]bool{
		"":            true,
		"no-fix":      true,
		"lint":        true,
		"types":       false,
		"types lint":  true,
		"format-only": false,
	} {
		flags := map[string]bool{}
		for _, name := range strings.Fields(given) {
			flags[name] = true
		}
		if got := runLints(flags); got != want {
			test.Errorf("--%s: lints %t, want %t", given, got, want)
		}
	}
}

// folderRun is one real run's output and exit, read as its view prints it.
type folderRun struct {
	output string
	exit   int
}

// A real run over a TypeScript project with a docs/ of Markdown, the check on in the project's settings: the folder
// is printed at its path, counted in the footer as a finding, and fails the run; under `--json` it is a `finding`
// with a path and no line, and the folder check's summary fails, so a reader of the verdicts reads it too. Renaming
// the folder between two runs gives the next run a fresh verdict, green, and renaming it back red, though the
// project's own run between them is replayed from the run cache: a project's recorded run never holds the folders'
// verdict, which each run judges from its own walk. With the check off in the project's settings, the folder passes.
func TestARealRunReportsAnAbbreviatedFolderAndRenamingItIsAFreshVerdict(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	// Each settings file in the house format, so the formatter has nothing to say.
	settings := "{\n    \"extends\": \"cohere:typescript\",\n    \"rules\": {\n        \"" +
		configuration.AbbreviatedFolderName + "\": \"error\"\n    }\n}\n"
	writeTree(test, root, map[string]string{
		".gitignore":          ".cache/\n",
		"CohereSettings.json": settings,
		// Formatted, as every file here is, so the only finding is the folder's.
		"tsconfig.json": "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, " +
			"\"include\": [\"source\"] }\n",
		"source/tidy.ts": "export const tidy = 1;\n",
		"docs/README.md": "# Documentation\n",
	})
	run := func(arguments ...string) folderRun {
		output, exit := runCohere(test, binary, root, append([]string{"--no-fix"}, arguments...)...)
		return folderRun{output: output, exit: exit}
	}
	rename := func(from string, to string) {
		if renameError := os.Rename(filepath.Join(root, from), filepath.Join(root, to)); renameError != nil {
			test.Fatal(renameError)
		}
	}
	folderLine := filepath.Join(root, "docs") + "/ - error " + configuration.AbbreviatedFolderName + ": "

	red := run()
	if red.exit != 1 || !strings.Contains(red.output, folderLine) || !strings.Contains(red.output, "1 folder finding") {
		test.Fatalf("with docs/ the run exited %d, want 1 naming the folder and counting it:\n%s", red.exit,
			red.output)
	}
	oneLine, exit := runCohereWithEnvironment(test, binary, root, nil, "--no-fix", "--json")
	if exit != 1 {
		test.Errorf("--json exited %d, want 1", exit)
	}
	sawFinding, sawSummary := false, false
	for _, line := range strings.Split(strings.TrimSpace(oneLine), "\n") {
		// Line is a pointer, so a record that carries one, zero or not, is told from one that carries none.
		var record struct {
			Kind     string `json:"kind"`
			Rule     string `json:"rule"`
			Path     string `json:"path"`
			Line     *int   `json:"line"`
			Label    string `json:"label"`
			Verdict  string `json:"verdict"`
			Findings int    `json:"findings"`
		}
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if record.Rule == configuration.AbbreviatedFolderName {
			sawFinding = record.Kind == "finding" && record.Path == filepath.Join(root, "docs")
			if record.Line != nil {
				test.Errorf("the folder's record has a line: %s", line)
			}
		}
		if record.Kind == "summary" && record.Label == folderCheckLabel {
			sawSummary = record.Verdict == "fail" && record.Findings == 1
		}
	}
	if !sawFinding || !sawSummary {
		test.Errorf("--json printed the folder finding %t and its failing summary %t, want both:\n%s", sawFinding,
			sawSummary, oneLine)
	}

	rename("docs", "documentation")
	if green := run(); green.exit != 0 || strings.Contains(green.output, configuration.AbbreviatedFolderName) {
		test.Fatalf("with docs/ renamed the run exited %d, want 0 and no folder named:\n%s", green.exit,
			green.output)
	}
	// Again, so the project's run is recorded and the next replays it: the folders' verdict must still be fresh.
	if again := run(); again.exit != 0 || !strings.Contains(again.output, "(from the cached run at ") {
		test.Fatalf("the second green run exited %d, want 0 replayed from the run cache:\n%s", again.exit,
			again.output)
	}
	rename("documentation", "docs")
	if back := run(); back.exit != 1 || !strings.Contains(back.output, folderLine) {
		test.Fatalf("with docs/ back the run exited %d, want 1 naming the folder, not a replay of green:\n%s",
			back.exit, back.output)
	}

	writeTree(test, root, map[string]string{"CohereSettings.json": "{\n    \"extends\": \"cohere:typescript\",\n" +
		"    \"rules\": {\n        \"" + configuration.AbbreviatedFolderName + "\": \"off\"\n    },\n" +
		"    \"reasons\": {\n        \"" + configuration.AbbreviatedFolderName +
		"\": \"This project keeps its folder names as they are.\"\n    }\n}\n"})
	if off := run(); off.exit != 0 || strings.Contains(off.output, configuration.AbbreviatedFolderName) {
		test.Errorf("with the check off in the project's settings, the run exited %d naming the folder:\n%s",
			off.exit, off.output)
	}
}

// A root of only a Go module, whose run never loads the TypeScript chain, with a docs/ and settings the loader
// refuses: the folder check fails the run naming the settings, rather than reading its setting as off. Before it
// failed closed, this exact root read green.
func TestAFolderCheckWhoseSettingsDoNotLoadFailsTheRun(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"go.mod":         "module example.com/toy\n\ngo 1.24\n",
		"toy.go":         "package toy\n\n// Toy is one.\nconst Toy = 1\n",
		"docs/README.md": "# Documentation\n",
		"CohereSettings.json": "{\n    \"rules\": {\n        \"" + configuration.AbbreviatedFolderName +
			"\": \"error\",\n        \"nexus/no-such-rule\": \"error\"\n    }\n}\n",
	})
	output, exit := runCohere(test, binary, root, "--no-fix")
	if exit == 0 || !strings.Contains(output, "for the folder check") ||
		!strings.Contains(output, filepath.Join(root, "CohereSettings.json")) {
		test.Errorf("the run exited %d, want nonzero naming the settings the folder check could not read:\n%s",
			exit, output)
	}
}

// A run discovery does not apply to still judges its project's folders when it lints the project whole: `--directory`
// into one engine's project and `--tsconfig` each read red over a docs/ of Markdown, naming the folder, and green
// once it is renamed, as the discovery path does (#zyq63kk, front door's review). Before, both printed green.
func TestANarrowedRunJudgesItsProjectsFolders(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for name, testCase := range map[string]struct {
		directory string
		arguments []string
	}{
		"--directory into one engine's project": {"", []string{"--no-fix", "--directory", "apps/web"}},
		"--tsconfig":                            {"apps/web", []string{"--no-fix", "--tsconfig", "tsconfig.json"}},
	} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			writeTree(test, root, map[string]string{
				".gitignore": ".cache/\n",
				"apps/web/CohereSettings.json": "{\n    \"extends\": \"cohere:typescript\",\n    \"rules\": {\n" +
					"        \"" + configuration.AbbreviatedFolderName + "\": \"error\"\n    }\n}\n",
				"apps/web/tsconfig.json": "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, " +
					"\"include\": [\"source\"] }\n",
				"apps/web/source/tidy.ts": "export const tidy = 1;\n",
				"apps/web/docs/README.md": "# Documentation\n",
			})
			folder := filepath.Join(root, "apps", "web", "docs")
			output, exit := runCohere(test, binary, filepath.Join(root, testCase.directory), testCase.arguments...)
			if exit != 1 || !strings.Contains(output, folder+"/ - error "+configuration.AbbreviatedFolderName) ||
				!strings.Contains(output, "1 folder finding") {
				test.Fatalf("over docs/ the run exited %d, want 1 naming the folder and counting it:\n%s", exit,
					output)
			}
			renamed := filepath.Join(root, "apps", "web", "documentation")
			if renameError := os.Rename(folder, renamed); renameError != nil {
				test.Fatal(renameError)
			}
			output, exit = runCohere(test, binary, filepath.Join(root, testCase.directory), testCase.arguments...)
			if exit != 0 || strings.Contains(output, configuration.AbbreviatedFolderName) {
				test.Errorf("with docs/ renamed the run exited %d, want 0 and no folder named:\n%s", exit, output)
			}
		})
	}
}
