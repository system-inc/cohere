package format_files

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A walk at a project below its repository's root reads the repository's ignore files and index, as git
// check-ignore does from a subdirectory (#14skmqy): a file the project's own pattern matches and git tracks is
// offered, a `.gitignore` above the project still applies, and an index the walk cannot read is named, never silent.

// subprojectRepository is a repository whose root ignores `*.generated.ts`, holding a project in app/ whose own
// `.gitignore` ignores `scratch.ts`. app/kept.generated.ts and app/scratch.ts are force-added, so git tracks both;
// app/other.generated.ts and app/stray/scratch.ts are not.
func subprojectRepository(test *testing.T) (repository string, project string) {
	test.Helper()
	if _, lookPathError := exec.LookPath("git"); lookPathError != nil {
		test.Skip("no git on PATH")
	}
	repository = settingsTree(test, `[]`, `[]`, map[string]string{
		".gitignore":              "*.generated.ts\n",
		"app/.gitignore":          "scratch.ts\n",
		"app/index.ts":            "export const index = 1;\n",
		"app/kept.generated.ts":   "export const kept = 1;\n",
		"app/other.generated.ts":  "export const other = 1;\n",
		"app/scratch.ts":          "export const scratch = 1;\n",
		"app/stray/scratch.ts":    "export const stray = 1;\n",
		"app/CohereSettings.json": `{"extends": "../nexus/NexusCohereSettings.json", "rules": {}}`,
	})
	git := func(arguments ...string) {
		test.Helper()
		command := exec.Command("git", append([]string{"-c", "user.email=fixture@example.com", "-c",
			"user.name=fixture", "-c", "core.excludesFile=/dev/null"}, arguments...)...)
		command.Dir = repository
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		if output, runError := command.CombinedOutput(); runError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, runError, output)
		}
	}
	git("init", "--quiet")
	git("add", "-A")
	git("add", "-f", "app/kept.generated.ts", "app/scratch.ts")
	git("commit", "--quiet", "-m", "fixture")
	return repository, filepath.Join(repository, "app")
}

func TestASubprojectWalkReadsItsRepositorysIndexAndIgnoreFiles(test *testing.T) {
	test.Parallel()
	repository, project := subprojectRepository(test)
	enumeration, enumerateError := Enumerate(project, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	for _, offered := range []string{"index.ts", "kept.generated.ts", "scratch.ts"} {
		if !slices.Contains(enumeration.Files, filepath.Join(project, offered)) {
			test.Errorf("the walk did not offer %s: %v", offered, enumeration.Files)
		}
	}
	for _, left := range []string{"other.generated.ts", filepath.Join("stray", "scratch.ts")} {
		if slices.Contains(enumeration.Files, filepath.Join(project, left)) {
			test.Errorf("the walk offered %s, which git ignores: %v", left, enumeration.Files)
		}
	}
	if enumeration.IndexRoot != repository || enumeration.IndexScope != "app" || enumeration.IndexFallback != "" {
		test.Errorf("the walk read the index at %q scoped %q, fallback %q, want %s scoped app",
			enumeration.IndexRoot, enumeration.IndexScope, enumeration.IndexFallback, repository)
	}
	if !slices.Contains(enumeration.IgnoreFiles, filepath.Join(repository, ".gitignore")) {
		test.Errorf("IgnoreFiles does not name the repository root's .gitignore: %v", enumeration.IgnoreFiles)
	}
}

// TestASubprojectWalkWhoseIndexCannotBeReadSaysSo: a split index, which the reader does not merge, leaves the walk
// deciding by the ignore files alone, so the tracked kept.generated.ts is counted ignored, and the walk says why.
func TestASubprojectWalkWhoseIndexCannotBeReadSaysSo(test *testing.T) {
	test.Parallel()
	repository, project := subprojectRepository(test)
	command := exec.Command("git", "update-index", "--split-index")
	command.Dir = repository
	if output, runError := command.CombinedOutput(); runError != nil {
		test.Fatalf("git update-index --split-index: %v\n%s", runError, output)
	}
	enumeration, enumerateError := Enumerate(project, handlesEveryLanguage)
	if enumerateError != nil {
		test.Fatal(enumerateError)
	}
	if !strings.Contains(enumeration.IndexFallback, "split index") {
		test.Errorf("the walk's fallback reads %q, want it to name the split index", enumeration.IndexFallback)
	}
	if slices.Contains(enumeration.Files, filepath.Join(project, "kept.generated.ts")) {
		test.Errorf("a walk that read no index offered a file only the index keeps: %v", enumeration.Files)
	}
	if enumeration.Index != "" {
		test.Errorf("a walk that fell back names %s as an index to key on", enumeration.Index)
	}
}
