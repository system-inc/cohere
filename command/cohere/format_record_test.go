package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/format/formatfiles"
)

// The format record decides what the default format scope holds, so each test below asserts the exact
// set it puts in scope, name by name. "Some files were in scope" is what a record that skips the wrong
// ones also produces.

// recordFixture is a project on disk with a declared submodule and an undeclared clone, an engine that
// walks it for real, and the record kept in the test's own directory.
type recordFixture struct {
	root       string
	recordPath string
	engine     *fakeEngine
}

func newRecordFixture(t *testing.T) recordFixture {
	t.Helper()
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"A.ts":         "export const a = 1;\n",
		"B.ts":         "export const b = 2;\n",
		"notes.txt":    "not formatted by anything\n",
		".gitignore":   "ignored/\n",
		"ignored/C.ts": "export const c = 3;\n",
		".gitmodules":  "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		// A submodule's `.git` is a gitlink file.
		"library/.git":     "gitdir: ../.git/modules/library\n",
		"library/Inner.ts": "export const inner = 4;\n",
		// A clone nobody declared, the shape of ahra's `projects/*`.
		"projects/clone/.git/HEAD": "ref: refs/heads/main\n",
		"projects/clone/Outer.ts":  "export const outer = 5;\n",
	})
	engine := &fakeEngine{handled: []string{".ts"}}
	engine.enumerate = func(directory string) (formatfiles.Enumeration, error) {
		return formatfiles.Enumerate(directory, "", engine.Handles)
	}
	return recordFixture{root: root, recordPath: filepath.Join(t.TempDir(), "format.json"), engine: engine}
}

func (fixture recordFixture) path(name string) string {
	return filepath.Join(fixture.root, filepath.FromSlash(name))
}

// scope loads the record the way a run does and returns the default scope's files, relative and
// sorted, with the universe and the record it read.
func (fixture recordFixture) scope(t *testing.T) ([]string, []string, *formatRecord, string) {
	t.Helper()
	record := loadFormatRecordAt(fixture.recordPath, fixture.root)
	scope, universe := unformattedScope(fixture.engine, record, fixture.root, "")
	return fixture.relative(t, scope.FileNames), universe, record, scope.Description
}

func (fixture recordFixture) relative(t *testing.T, files []string) []string {
	t.Helper()
	out := []string{}
	for _, file := range files {
		relative, err := filepath.Rel(fixture.root, file)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(relative))
	}
	return out
}

// formatEverything runs the files in scope through an observed transform the way the fix phase does,
// with a formatter that finds every file already formatted, and saves the record.
func (fixture recordFixture) formatEverything(t *testing.T) {
	t.Helper()
	record := loadFormatRecordAt(fixture.recordPath, fixture.root)
	scope, universe := unformattedScope(fixture.engine, record, fixture.root, "")
	transform := record.observe(formatTransform(fixture.engine), fixture.engine.OptionsFingerprint)
	for _, fileName := range scope.FileNames {
		if _, err := transform(fileName, readForTest(t, fileName)); err != nil {
			t.Fatal(err)
		}
	}
	if err := record.save(universe); err != nil {
		t.Fatal(err)
	}
}

// aged moves a file's modification time into the past, past the second inside which no signature is
// recorded, so the next read can be answered by stat alone.
func aged(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

func assertScope(t *testing.T, got []string, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("in scope %v, expected %v", got, want)
	}
}

func TestWithNoRecordEveryFileIsInScopeAndOnlyDeclaredSubmodulesAreWalked(t *testing.T) {
	fixture := newRecordFixture(t)
	files, universe, _, description := fixture.scope(t)

	// The ignored file and the undeclared clone are not in the universe; the declared submodule is.
	assertScope(t, files, "A.ts", "B.ts", "library/Inner.ts")
	if len(universe) != 3 {
		t.Fatalf("universe %v, expected the three files in scope", fixture.relative(t, universe))
	}
	for _, want := range []string{"all 3 files, because no earlier check is on record", "submodule library", "skipped nested repositories projects/clone"} {
		if !strings.Contains(description, want) {
			t.Errorf("the scope line does not say %q:\n%s", want, description)
		}
	}
}

func TestTheRecordScopesWhatChangedSinceCohereLastLooked(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.formatEverything(t)

	files, _, _, description := fixture.scope(t)
	assertScope(t, files)
	if !strings.Contains(description, "0 of 3 files not on record as formatted") {
		t.Errorf("an unchanged tree's scope line: %s", description)
	}

	original := readForTest(t, fixture.path("A.ts"))
	writeTree(t, fixture.root, map[string]string{"A.ts": "export const a = 10;\n"})
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files, "A.ts")

	// Edited back to the bytes on record: unchanged, whatever its modification time says.
	writeTree(t, fixture.root, map[string]string{"A.ts": original})
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files)

	// A new file nobody has formatted is in scope, untracked or not: nothing here asks git.
	writeTree(t, fixture.root, map[string]string{"New.ts": "export const fresh = 6;\n"})
	// An edit inside the submodule is an edit too.
	writeTree(t, fixture.root, map[string]string{"library/Inner.ts": "export const inner = 40;\n"})
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files, "New.ts", "library/Inner.ts")
}

func TestADeletedFileLeavesTheScopeAndTheRecord(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.formatEverything(t)
	if err := os.Remove(fixture.path("B.ts")); err != nil {
		t.Fatal(err)
	}

	files, universe, record, _ := fixture.scope(t)
	assertScope(t, files)
	if err := record.save(universe); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored formatRecordFile
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	if _, kept := stored.Entries[fixture.path("B.ts")]; kept {
		t.Fatal("the record kept an entry for a deleted file")
	}
	if len(stored.Entries) != 2 {
		t.Fatalf("the record holds %d entries, expected the two files that remain", len(stored.Entries))
	}
}

// An unformatted file checked under --no-fix records the text the formatter would write, which is not
// what is on disk, so the file stays in scope and keeps being reported until it is written.
func TestAnUnformattedFileStaysInScopeUntilItIsWritten(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.engine.format = func(fileName string, text string) (string, error) {
		return strings.ReplaceAll(text, "  ", " "), nil
	}
	writeTree(t, fixture.root, map[string]string{"A.ts": "export const a  = 1;\n"})
	fixture.formatEverything(t)

	files, _, _, _ := fixture.scope(t)
	assertScope(t, files, "A.ts")

	// Written as the formatter would write it, it is on record.
	writeTree(t, fixture.root, map[string]string{"A.ts": "export const a = 1;\n"})
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files)
}

func TestAnOptionsChangeSendsItsFilesBackThroughTheFormatter(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.formatEverything(t)
	fixture.engine.options = func(fileName string) (string, error) {
		if strings.Contains(fileName, "library") {
			return "printWidth 80", nil
		}
		return "fake options", nil
	}

	files, _, _, _ := fixture.scope(t)
	assertScope(t, files, "library/Inner.ts")
}

func TestARecordFromAnotherCohereSaysNothing(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.formatEverything(t)

	contents, err := os.ReadFile(fixture.recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var stored formatRecordFile
	if err := json.Unmarshal(contents, &stored); err != nil {
		t.Fatal(err)
	}
	stored.Identity = "another cohere"
	encoded, err := json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.recordPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}

	files, _, _, description := fixture.scope(t)
	assertScope(t, files, "A.ts", "B.ts", "library/Inner.ts")
	if !strings.Contains(description, "cohere changed since the last check") {
		t.Errorf("the scope line does not say why every file is in scope: %s", description)
	}

	// An unreadable record is the same answer, never an error.
	if err := os.WriteFile(fixture.recordPath, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files, "A.ts", "B.ts", "library/Inner.ts")
}

// The stat shortcut is only taken once the bytes were read and matched, and a file whose bytes change
// is caught whether or not its size does.
func TestAVerifiedFileIsAnsweredByStatAndAnEditIsStillSeen(t *testing.T) {
	fixture := newRecordFixture(t)
	fixture.formatEverything(t)
	for _, name := range []string{"A.ts", "B.ts", "library/Inner.ts"} {
		aged(t, fixture.path(name))
	}

	// The first read verifies by content and records each signature.
	files, universe, record, _ := fixture.scope(t)
	assertScope(t, files)
	if err := record.save(universe); err != nil {
		t.Fatal(err)
	}
	reloaded := loadFormatRecordAt(fixture.recordPath, fixture.root)
	if entry := reloaded.entries[fixture.path("A.ts")]; entry.ModifiedNanoseconds == 0 {
		t.Fatal("a file read and matched was given no signature, so every run reads it again")
	}

	// Same size, new bytes.
	writeTree(t, fixture.root, map[string]string{"A.ts": "export const a = 9;\n"})
	files, _, _, _ = fixture.scope(t)
	assertScope(t, files, "A.ts")
}

func TestDeclaredSubmodulesReadsTheGitmodulesPaths(t *testing.T) {
	directory := t.TempDir()
	writeTree(t, directory, map[string]string{".gitmodules": "" +
		"[submodule \"libraries/structure\"]\n" +
		"\tpath = libraries/structure\n" +
		"\turl = git@github.com:ahraia/structure-next.git\n" +
		"\tbranch = main\n" +
		"[submodule \"quoted\"]\n" +
		"\tpath = \"libraries/with space\"\n"})

	declared, err := declaredSubmodules(directory)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{}{"libraries/structure": {}, "libraries/with space": {}}
	if !reflect.DeepEqual(declared, want) {
		t.Fatalf("declared %v, expected %v", declared, want)
	}

	if none, err := declaredSubmodules(t.TempDir()); err != nil || len(none) != 0 {
		t.Fatalf("a repository with no .gitmodules declared %v (err %v)", none, err)
	}
}
