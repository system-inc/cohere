package main

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/format/format_files"
)

// The format record decides what the default format scope holds, so each test below asserts the exact
// set it puts in scope, name by name. "Some files were in scope" is what a record that skips the wrong
// ones also produces.

// recordFixture is a project on disk with a declared submodule and an undeclared clone, and an engine
// that walks it for real. The record lives in the real cache table, under a home of the test's own.
type recordFixture struct {
	root   string
	engine *fakeEngine
}

func newRecordFixture(test *testing.T) recordFixture {
	test.Helper()
	test.Setenv("HOME", test.TempDir())
	test.Setenv("XDG_CACHE_HOME", "")
	root := test.TempDir()
	writeTree(test, root, map[string]string{
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
	engine.enumerate = func(directory string) (format_files.Enumeration, error) {
		return format_files.Enumerate(directory, engine.Handles)
	}
	return recordFixture{root: root, engine: engine}
}

func (fixture recordFixture) path(name string) string {
	return filepath.Join(fixture.root, filepath.FromSlash(name))
}

// scope loads the record the way a run does and returns the default scope's files, relative and
// sorted, with the universe and the record it read.
func (fixture recordFixture) scope(test *testing.T) ([]string, []string, *formatRecord, string) {
	test.Helper()
	record := loadFormatRecord(fixture.root)
	scope, universe := unformattedScope(fixture.engine, record, fixture.root)
	return fixture.relative(test, scope.FileNames), universe, record, scope.Description
}

func (fixture recordFixture) relative(test *testing.T, files []string) []string {
	test.Helper()
	out := []string{}
	for _, file := range files {
		relative, relativePathError := filepath.Rel(fixture.root, file)
		if relativePathError != nil {
			test.Fatal(relativePathError)
		}
		out = append(out, filepath.ToSlash(relative))
	}
	return out
}

// formatEverything runs the files in scope through an observed transform the way the fix phase does,
// with a formatter that finds every file already formatted, and saves the record.
func (fixture recordFixture) formatEverything(test *testing.T) {
	test.Helper()
	record := loadFormatRecord(fixture.root)
	scope, universe := unformattedScope(fixture.engine, record, fixture.root)
	transform := record.observe(formatTransform(fixture.engine), fixture.engine.OptionsFingerprint)
	for _, fileName := range scope.FileNames {
		if _, transformError := transform(fileName, readForTest(test, fileName), nil); transformError != nil {
			test.Fatal(transformError)
		}
	}
	if saveError := record.save(universe); saveError != nil {
		test.Fatal(saveError)
	}
}

// aged moves a file's modification time into the past, past the second inside which no signature is
// recorded, so the next read can be answered by stat alone.
func aged(test *testing.T, path string) {
	test.Helper()
	old := time.Now().Add(-time.Minute)
	if changeTimesError := os.Chtimes(path, old, old); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
}

func assertScope(test *testing.T, got []string, want ...string) {
	test.Helper()
	expected := want
	if want == nil {
		expected = []string{}
	}
	if !reflect.DeepEqual(got, expected) {
		test.Fatalf("in scope %v, expected %v", got, expected)
	}
}

// TestWithNoRecordEveryFileOfTheRepositoryIsInScopeAndNoSubmoduleIs: the universe stops at the
// repository's own boundary. Writes stay inside one repository (@system_cohere, 2026-10-03), so the
// declared submodule is formatted by a run inside it and only read from here, by the check (see
// checkNestedRepositories); it used to be walked into and written.
// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestWithNoRecordEveryFileOfTheRepositoryIsInScopeAndNoSubmoduleIs(test *testing.T) {
	fixture := newRecordFixture(test)
	files, universe, _, description := fixture.scope(test)

	// The ignored file, the undeclared clone and the declared submodule are all outside the universe.
	assertScope(test, files, "A.ts", "B.ts")
	if len(universe) != 2 {
		test.Fatalf("universe %v, expected the two files in scope", fixture.relative(test, universe))
	}
	for _, want := range []string{
		"all 2 files, because no earlier check is on record",
		"skipped nested repositories library, projects/clone",
	} {
		if !strings.Contains(description, want) {
			test.Errorf("the scope line does not say %q:\n%s", want, description)
		}
	}
}

// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestTheRecordScopesWhatChangedSinceCohereLastLooked(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.formatEverything(test)

	files, _, _, description := fixture.scope(test)
	assertScope(test, files)
	if !strings.Contains(description, "0 of 2 files not on record as formatted") {
		test.Errorf("an unchanged tree's scope line: %s", description)
	}

	original := readForTest(test, fixture.path("A.ts"))
	writeTree(test, fixture.root, map[string]string{"A.ts": "export const a = 10;\n"})
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files, "A.ts")

	// Edited back to the bytes on record: unchanged, whatever its modification time says.
	writeTree(test, fixture.root, map[string]string{"A.ts": original})
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files)

	// A new file nobody has formatted is in scope, untracked or not: nothing here asks git.
	writeTree(test, fixture.root, map[string]string{"New.ts": "export const fresh = 6;\n"})
	// An edit inside the submodule is the submodule's run's to format, not this one's: the project's
	// check reports it as the submodule's drift instead.
	writeTree(test, fixture.root, map[string]string{"library/Inner.ts": "export const inner = 40;\n"})
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files, "New.ts")
}

// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestADeletedFileLeavesTheScopeAndTheRecord(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.formatEverything(test)
	if removeError := os.Remove(fixture.path("B.ts")); removeError != nil {
		test.Fatal(removeError)
	}

	files, universe, record, _ := fixture.scope(test)
	assertScope(test, files)
	if saveError := record.save(universe); saveError != nil {
		test.Fatal(saveError)
	}
	stored := readFormatSection(fixture.root)
	if stored == nil {
		test.Fatal("the record was not written to the cache table")
	}
	if _, kept := stored.Entries[fixture.path("B.ts")]; kept {
		test.Fatal("the record kept an entry for a deleted file")
	}
	if len(stored.Entries) != 1 {
		test.Fatalf("the record holds %d entries, expected the one file that remains", len(stored.Entries))
	}
}

// An unformatted file checked under --no-fix records the text the formatter would write, which is not
// what is on disk, so the file stays in scope and keeps being reported until it is written.
// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestAnUnformattedFileStaysInScopeUntilItIsWritten(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.engine.format = func(fileName string, text string) (string, error) {
		return strings.ReplaceAll(text, "  ", " "), nil
	}
	writeTree(test, fixture.root, map[string]string{"A.ts": "export const a  = 1;\n"})
	fixture.formatEverything(test)

	files, _, _, _ := fixture.scope(test)
	assertScope(test, files, "A.ts")

	// Written as the formatter would write it, it is on record.
	writeTree(test, fixture.root, map[string]string{"A.ts": "export const a = 1;\n"})
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files)
}

// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestAnOptionsChangeSendsItsFilesBackThroughTheFormatter(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.formatEverything(test)
	fixture.engine.options = func(fileName string) (string, error) {
		if strings.HasSuffix(fileName, "B.ts") {
			return "printWidth 80", nil
		}
		return "fake options", nil
	}

	files, _, _, _ := fixture.scope(test)
	assertScope(test, files, "B.ts")
}

// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestARecordFromAnotherCohereSaysNothing(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.formatEverything(test)

	stored := readFormatSection(fixture.root)
	stored.Key = "another cohere"
	if writeFormatSectionError := writeFormatSection(fixture.root, stored); writeFormatSectionError != nil {
		test.Fatal(writeFormatSectionError)
	}

	// A test binary is a build nobody stamped, so it says its record is keyed on the binary file; a stamped build
	// says the formatter changed, which TestTheRecordFollowsTheFormatterNotTheBinary reads.
	files, _, _, description := fixture.scope(test)
	assertScope(test, files, "A.ts", "B.ts")
	if !strings.Contains(description, "this build carries no formatter identity, so its record is keyed on") {
		test.Errorf("the scope line does not say why every file is in scope: %s", description)
	}

	// An unreadable table is the same answer, never an error.
	if writeError := os.WriteFile(
		filepath.Join(cache_directory.Of(fixture.root), "format.gob"),
		[]byte("not a cache table"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files, "A.ts", "B.ts")
}

// The stat shortcut is only taken once the bytes were read and matched, and a file whose bytes change
// is caught whether or not its size does.
// Not parallel: it sets `HOME` and XDG_CACHE_HOME with test.Setenv through newRecordFixture, which a parallel test may
// not
func TestAVerifiedFileIsAnsweredByStatAndAnEditIsStillSeen(test *testing.T) {
	fixture := newRecordFixture(test)
	fixture.formatEverything(test)
	for _, name := range []string{"A.ts", "B.ts"} {
		aged(test, fixture.path(name))
	}

	// The first read verifies by content and records each signature.
	files, universe, record, _ := fixture.scope(test)
	assertScope(test, files)
	if saveError := record.save(universe); saveError != nil {
		test.Fatal(saveError)
	}
	reloaded := loadFormatRecord(fixture.root)
	if entry := reloaded.entries[fixture.path("A.ts")]; entry.ModifiedNanoseconds == 0 {
		test.Fatal("a file read and matched was given no signature, so every run reads it again")
	}

	// Same size, new bytes.
	writeTree(test, fixture.root, map[string]string{"A.ts": "export const a = 9;\n"})
	files, _, _, _ = fixture.scope(test)
	assertScope(test, files, "A.ts")
}

func TestDeclaredSubmodulesReadsTheGitmodulesPaths(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	writeTree(test, directory, map[string]string{".gitmodules": "" +
		"[submodule \"libraries/structure\"]\n" +
		"\tpath = libraries/structure\n" +
		"\turl = git@github.com:ahraia/structure-next.git\n" +
		"\tbranch = main\n" +
		"[submodule \"quoted\"]\n" +
		"\tpath = \"libraries/with space\"\n"})

	declared, declaredSubmodulesError := declaredSubmodules(directory)
	if declaredSubmodulesError != nil {
		test.Fatal(declaredSubmodulesError)
	}
	want := map[string]struct{}{"libraries/structure": {}, "libraries/with space": {}}
	if !reflect.DeepEqual(declared, want) {
		test.Fatalf("declared %v, expected %v", declared, want)
	}

	if none, declaredSubmodulesError := declaredSubmodules(
		test.TempDir(),
	); declaredSubmodulesError != nil || len(none) != 0 {
		test.Fatalf("a repository with no .gitmodules declared %v (err %v)", none, declaredSubmodulesError)
	}
}

// The record follows the formatter, not the binary. Three builds of this command: two from different
// commits stamped with the same formatter identity, and a third whose formatter differs. The second
// must read what the first recorded, and the third must start over. This is the property that keeps a
// lint-only cohere commit from reformatting the tree, and the one that must not keep a record across a
// printer change.
func TestTheRecordFollowsTheFormatterNotTheBinary(test *testing.T) {
	test.Parallel()
	// Every build compiles one snapshot of the source, taken once, so the three binaries differ only in
	// the two stamps the test sets. Built from the shared working tree, they could carry different trees
	// whenever another node edited it between builds, and the second build then discarded the record:
	// the test failed about one run in three, measuring the tree's churn rather than the record's rule.
	snapshot := sourceSnapshot(test)
	// The snapshot is a copy of this package's own source, read in process, but its TypeScript is a link to the
	// module's, which the builds read through it.
	build_inputs.Record(test, ".", ".")
	// The three differ only in what the linker stamps. The first builds alone, compiling every package,
	// and the other two link at once from what it left in the build cache: one after another the links
	// were most of this test's time (#nxgt2ca), and all three started together each compiled every
	// package, since go does not share a compile in flight between processes: three times the work, beside
	// the pool's budget (load 58 on 16 cores with two runs, #91m7c16). -trimpath keeps the snapshot's
	// directory out of every package's cache key, so a run reuses what the last run compiled rather than
	// compiling the whole program cold from a new directory each time.
	stamps := []struct{ selfCommit, formatter string }{
		{strings.Repeat("1", 40), "formatterA"},
		{strings.Repeat("2", 40), "formatterA"},
		{strings.Repeat("2", 40), "formatterB"},
	}
	binaries := make([]string, len(stamps))
	failures := make([]string, len(stamps))
	build := func(index int) {
		const packaging = "github.com/system-inc/cohere/internal/release/packaging"
		stamp := stamps[index]
		binaries[index] = filepath.Join(test.TempDir(), "cohere")
		command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-o", binaries[index],
			"-ldflags=-X "+
				packaging+
				".selfCommit="+
				stamp.selfCommit+
				" -X "+
				packaging+
				".formatterIdentity="+
				stamp.formatter, "./command/cohere")
		command.Dir = snapshot
		if output, outputError := command.CombinedOutput(); outputError != nil {
			failures[index] = fmt.Sprintf(
				"cannot build cohere stamped %s, %s: %v\n%s",
				stamp.selfCommit,
				stamp.formatter,
				outputError,
				output,
			)
		}
	}
	build(0)
	if failures[0] != "" {
		test.Fatal(failures[0])
	}
	var links sync.WaitGroup
	for index := 1; index < len(stamps); index++ {
		links.Go(func() { build(index) })
	}
	links.Wait()
	for _, failure := range failures {
		if failure != "" {
			test.Fatal(failure)
		}
	}
	first, lintOnly, printerChanged := binaries[0], binaries[1], binaries[2]

	root := test.TempDir()
	home := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": {} }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
		"Ugly.ts":                  "export const ugly   =   1\n",
	})
	run := func(binary string, arguments ...string) string {
		test.Helper()
		command := exec.Command(binary, verboseArguments(arguments)...)
		command.Dir = root
		command.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME=")
		output, runError := command.CombinedOutput()
		requireChildRan(test, runError)
		return string(output)
	}

	run(first, "--fix", "--format")
	if output := run(
		lintOnly,
		"--no-fix",
		"--format",
	); !strings.Contains(output, "0 of 5 files not on record as formatted") {
		test.Fatalf("a build from another commit with the same formatter did not read the record:\n%s", output)
	}
	if output := run(
		printerChanged,
		"--no-fix",
		"--format",
	); !strings.Contains(output, "all 5 files, because the formatter changed since the last check") {
		test.Fatalf("a build with another formatter trusted the record:\n%s", output)
	}
}

// A plain run, the one most people type, writes like --fix but formats only what is not on record, so an
// unchanged tree costs no formatting and no write. The fixer is the exception the record must not hide: a
// file whose bytes are on record and which a rule then rewrites is formatted anyway, since its fixed text
// is on record nowhere. The fixtures above read the scope in process; this runs the binary, with no flag but
// --verbose for the scope line.
func TestAPlainRunFormatsOnlyWhatIsNotOnRecord(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	home := test.TempDir()
	// no-debugger is on in cohere:typescript, so it is turned off until step 3 wants the fixer: Fixed.ts
	// is formatted with its debugger statement on record first. Its blank lines either side are the point,
	// since the fix deletes the statement and leaves its line's indent between two blank lines, which
	// only the formatter collapses.
	fixed := "export function value(): number {\n  const count = 1;\n\n  debugger;\n\n  return count;\n}\n"
	settings := func(severity string) string {
		return "{ \"extends\": \"./NexusCohereSettings.json\", \"rules\": { \"no-debugger\": \"" + severity + "\" } }\n"
	}
	writeTree(test, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      settings("off"),
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		".gitignore":               ".cache/\n",
		"Tidy.ts":                  "export const tidy = 1;\n",
		"Ugly.ts":                  "export const ugly   =   1\n",
		"Fixed.ts":                 fixed,
	})
	run := func() string {
		test.Helper()
		command := exec.Command(binary, verboseArguments(nil)...)
		command.Dir = root
		command.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME=")
		output, runError := command.CombinedOutput()
		requireChildRan(test, runError)
		return string(output)
	}

	// 1. With no record, every file is in scope, and the unformatted one is written.
	if output := run(); readForTest(test, filepath.Join(root, "Ugly.ts")) != "export const ugly = 1;\n" {
		test.Fatalf("a plain run did not format Ugly.ts:\n%s", output)
	}
	if readForTest(test, filepath.Join(root, "Fixed.ts")) != fixed {
		test.Fatal("Fixed.ts was rewritten while no-debugger was off, so step 3 would not start from bytes on record")
	}

	// 2. Nothing changed since, so nothing is in scope and nothing is written. The modification time is
	// read as well as the bytes, because a run that formatted a file and wrote the same bytes back would
	// pass a bytes check while doing the work the record exists to skip.
	type snapshot struct {
		contents string
		modified time.Time
	}
	taken := map[string]snapshot{}
	for _, name := range []string{"Tidy.ts", "Ugly.ts", "Fixed.ts"} {
		information, statError := os.Stat(filepath.Join(root, name))
		if statError != nil {
			test.Fatal(statError)
		}
		taken[name] = snapshot{readForTest(test, filepath.Join(root, name)), information.ModTime()}
	}
	output := run()
	if !strings.Contains(output, "format scope: 0 of 6 files not on record as formatted") {
		test.Fatalf("a plain run over an unchanged tree put files in the format scope:\n%s", output)
	}
	for name, before := range taken {
		information, statError := os.Stat(filepath.Join(root, name))
		if statError != nil {
			test.Fatal(statError)
		}
		if after := readForTest(test, filepath.Join(root, name)); after != before.contents ||
			!information.ModTime().Equal(before.modified) {
			test.Fatalf("a plain run over an unchanged tree wrote %s:\n%s", name, output)
		}
	}

	// 3. The rule turned on, Fixed.ts is fixed. Its bytes are still on record, so the scope holds only the
	// settings file just written, and Fixed.ts is formatted because the fixer proposed a change there.
	writeTree(test, root, map[string]string{
		"CohereSettings.json": settings("error"),
	})
	output = run()
	if !strings.Contains(output, "format scope: 1 of 6 files not on record as formatted") {
		test.Fatalf("the scope should hold the settings file alone, leaving Fixed.ts to the fixer:\n%s", output)
	}
	want := "export function value(): number {\n  const count = 1;\n\n  return count;\n}\n"
	if after := readForTest(test, filepath.Join(root, "Fixed.ts")); after != want {
		test.Fatalf("a file the fixer rewrote was not fixed and formatted, it reads:\n%q\n%s", after, output)
	}
}

// sourceSnapshot copies the module's source, as it is on disk now, into a directory of the test's own,
// and returns the copy's root. Builds from it cannot see an edit anyone makes to the working tree
// afterward. The compiler pin (TypeScript, a submodule nobody edits in passing) is linked rather than
// copied. It is a copy of the working tree rather than of a commit, so the test still builds the code
// someone is changing: an export of `HEAD` would quietly test the old record logic instead.
//
// It copies only what `go build` reads: test files and testdata directories are left out, since the
// build ignores both and no go:embed reaches into either. They were most of the copy's 4,000 files and
// 95 MB.
func sourceSnapshot(test *testing.T) string {
	test.Helper()
	moduleRoot, absolutePathError := filepath.Abs(filepath.Join("..", ".."))
	if absolutePathError != nil {
		test.Fatal(absolutePathError)
	}
	snapshot := test.TempDir()
	for _, name := range []string{"go.mod", "go.sum", "go.work", "go.work.sum"} {
		copySnapshotFile(test, filepath.Join(moduleRoot, name), filepath.Join(snapshot, name))
	}
	// Every directory holding source go builds: the module's own, docs among them, whose data/rules.json the
	// Go engine's file-name rule embeds (#2xt6v5r), and every module go.work names, read from go.work itself so a
	// module added there is never one the snapshot names and cannot find (#wy3dbnz).
	directories := []string{"command", "internal", "policy", "unicode_properties", "docs"}
	for _, directory := range goWorkModuleDirectories(test, filepath.Join(moduleRoot, "go.work")) {
		if !slices.Contains(directories, directory) {
			directories = append(directories, directory)
		}
	}
	for _, directory := range directories {
		source := filepath.Join(moduleRoot, directory)
		walkError := filepath.WalkDir(source, func(path string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			relative, relativePathError := filepath.Rel(moduleRoot, path)
			if relativePathError != nil {
				return relativePathError
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return os.MkdirAll(filepath.Join(snapshot, relative), 0o755)
			}
			if !entry.Type().IsRegular() || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			copySnapshotFile(test, path, filepath.Join(snapshot, relative))
			return nil
		})
		if walkError != nil {
			test.Fatalf("snapshotting %s: %v", directory, walkError)
		}
	}
	if symlinkError := os.Symlink(
		filepath.Join(moduleRoot, "TypeScript"),
		filepath.Join(snapshot, "TypeScript"),
	); symlinkError != nil {
		test.Fatal(symlinkError)
	}
	return snapshot
}

// goWorkModuleDirectories is the top directory of every module go.work's use block names beside the
// root, the root itself and TypeScript left out: the root's source is copied by name, and TypeScript is
// linked, not copied. A use line is read as go.work's own grammar has it, one path to a line inside the
// block, a // comment ignored.
func goWorkModuleDirectories(test *testing.T, goWork string) []string {
	test.Helper()
	contents, readError := os.ReadFile(goWork)
	if readError != nil {
		test.Fatal(readError)
	}
	var directories []string
	inside := false
	for line := range strings.Lines(string(contents)) {
		trimmed, _, _ := strings.Cut(strings.TrimSpace(line), "//")
		trimmed = strings.TrimSpace(trimmed)
		switch {
		case trimmed == "use (":
			inside = true
		case inside && trimmed == ")":
			inside = false
		case inside && trimmed != "":
			top, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "./"), "/")
			if top != "." && top != "" && top != "TypeScript" && !slices.Contains(directories, top) {
				directories = append(directories, top)
			}
		}
	}
	if len(directories) == 0 {
		test.Fatalf("%s names no module beside the root in a use block, so the snapshot would miss them", goWork)
	}
	return directories
}

func copySnapshotFile(test *testing.T, source string, destination string) {
	test.Helper()
	contents, readError := os.ReadFile(source)
	if readError != nil {
		if os.IsNotExist(readError) {
			return
		}
		test.Fatal(readError)
	}
	if writeError := os.WriteFile(destination, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}
