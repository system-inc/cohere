package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/edit"
	"github.com/system-inc/cohere/internal/format/format_files"
	"github.com/system-inc/cohere/internal/types/program"
)

// nestedTree writes a project whose library is a declared submodule, with a submodule declared inside
// the library, the shape of ahra, libraries/structure and libraries/structure/libraries/nexus, and a
// clone under projects/ that nobody declared, as ahra holds six.
func nestedTree(test *testing.T) string {
	test.Helper()
	root := test.TempDir()
	for _, directory := range []string{
		".git",
		"library/.git",
		"library/inner/.git",
		"library/source",
		"modules",
		"projects/clone/.git",
	} {
		if createDirectoryError := os.MkdirAll(filepath.Join(root, directory), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}
	writeTree(test, root, map[string]string{
		"modules/Host.ts":          "export const host   =   1\n",
		"library/source/Thing.ts":  "export const thing   =   1\n",
		"library/source/Tidy.ts":   "export const tidy = 1\n",
		"library/inner/Deep.ts":    "export const deep   =   1\n",
		"library/inner/Settled.ts": "export const settled = 1\n",
		"library/.gitignore":       "ignored/\n",
		"library/ignored/Skip.ts":  "export const skip   =   1\n",
		"projects/clone/Clone.ts":  "export const clone   =   1\n",
		".gitmodules":              "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.gitmodules":      "[submodule \"inner\"]\n\tpath = inner\n\turl = ../inner\n",
	})
	return root
}

// TestARunInsideALibraryWritesTheLibrary: the repository a run writes is the nearest one at or above
// where it started, inside the project; the project itself everywhere else.
func TestARunInsideALibraryWritesTheLibrary(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	for _, testCase := range []struct {
		start string
		want  string
	}{
		{".", "."},
		{"modules", "."},
		{"library", "library"},
		{"library/source", "library"},
		{"library/inner", "library/inner"},
	} {
		got := writeRepositoryRoot(filepath.Join(root, testCase.start), root)
		if got != filepath.Join(root, testCase.want) {
			test.Errorf(
				"a run started in %s writes %s, want %s",
				testCase.start,
				got,
				filepath.Join(root, testCase.want),
			)
		}
	}

	// A project that is not a repository at all writes where it always did.
	plain := test.TempDir()
	if createDirectoryError := os.MkdirAll(filepath.Join(plain, "source"), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if got := writeRepositoryRoot(filepath.Join(plain, "source"), plain); got != plain {
		test.Errorf("a plain directory's run writes %s, want the project %s", got, plain)
	}
}

// TestARunWritesOnlyItsOwnRepository: from inside a library, a file in the project above and a file in
// a repository nested in the library are both somebody else's, named; the library's own file is not.
func TestARunWritesOnlyItsOwnRepository(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	library := filepath.Join(root, "library")

	if why := unwritableRepository(library, filepath.Join(library, "source", "Thing.ts")); why != "" {
		test.Errorf("the library's own file was refused: %s", why)
	}
	if why := unwritableRepository(
		library,
		filepath.Join(root, "modules", "Host.ts"),
	); why != "the project outside "+library {
		test.Errorf("the project's file was not refused as outside the library: %q", why)
	}
	if why := unwritableRepository(
		library,
		filepath.Join(library, "inner", "Deep.ts"),
	); why != "nested repository inner" {
		test.Errorf("a file in a repository nested in the library was not refused under it: %q", why)
	}
	if why := unwritableRepository(
		root,
		filepath.Join(library, "source", "Thing.ts"),
	); why != "nested repository library" {
		test.Errorf("from the project, the library's file was not refused under the library: %q", why)
	}
}

// TestTheProjectReadsItsLibrariesDrift: every declared submodule is read, recursively, each with its own
// ignore layers, and each file its own run would rewrite is reported under that repository. A clone
// nobody declared is not read, and nothing is written.
func TestTheProjectReadsItsLibrariesDrift(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (format_files.Enumeration, error) {
		return format_files.Enumerate(walkRoot, engine.Handles)
	}
	before := treeSnapshot(test, root)

	check, checkNestedRepositoriesError := checkNestedRepositories(engine, root, false)
	if checkNestedRepositoriesError != nil {
		test.Fatal(checkNestedRepositoriesError)
	}

	got := []string{}
	for _, drift := range check.Drift {
		got = append(got, drift.Repository+": "+relativeIn(test, root, drift.FileName))
	}
	want := []string{"library/inner: library/inner/Deep.ts", "library: library/source/Thing.ts"}
	if strings.Join(got, ", ") != strings.Join(want, ", ") {
		test.Fatalf(
			"drift %v, want %v (the ignored file and the undeclared clone skipped, the project's own file not read here)",
			got,
			want,
		)
	}
	if check.Repositories != 2 || check.Files != 4 {
		test.Fatalf("read %d repositories and %d files, want 2 and 4", check.Repositories, check.Files)
	}
	assertTreeUnchanged(test, root, before, "checkNestedRepositories")
}

// TestTheNestedCheckKeepsEachRepositorysOwnRecord: the drift check formats only what is not on a nested
// repository's own record at its current bytes, keeps that record in the repository's own cache table and
// never the parent's, and still reports every drifted file: one recorded clean and then misformatted, and
// one whose recorded entry was written under another formatter's key or other options.
func TestTheNestedCheckKeepsEachRepositorysOwnRecord(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	library := filepath.Join(root, "library")
	inner := filepath.Join(library, "inner")
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (format_files.Enumeration, error) {
		return format_files.Enumerate(walkRoot, engine.Handles)
	}
	check := func(label string, wantChecked int, wantDrift ...string) {
		test.Helper()
		result, checkNestedRepositoriesError := checkNestedRepositories(engine, root, false)
		if checkNestedRepositoriesError != nil {
			test.Fatal(checkNestedRepositoriesError)
		}
		got := []string{}
		for _, drift := range result.Drift {
			got = append(got, relativeIn(test, root, drift.FileName))
		}
		if strings.Join(got, ", ") != strings.Join(wantDrift, ", ") {
			test.Fatalf("%s: drift %v, want %v", label, got, wantDrift)
		}
		if result.Files != 4 || result.Checked != wantChecked {
			test.Fatalf(
				"%s: %d files, %d checked, want 4 files and %d checked",
				label,
				result.Files,
				result.Checked,
				wantChecked,
			)
		}
	}
	drifted := []string{"library/inner/Deep.ts", "library/source/Thing.ts"}

	// A repository nobody has run cohere in keeps no cache, and a check that only reads it creates none:
	// every file goes through the formatter, every time. A run inside each repository makes its cache.
	for range 2 {
		check("a check before either repository keeps a cache", 4, drifted...)
	}
	for _, repository := range []string{library, inner} {
		if _, statError := os.Stat(cache_directory.Of(repository)); !errors.Is(statError, os.ErrNotExist) {
			test.Fatalf("a check that only reads %s made its cache directory (%v)", repository, statError)
		}
		if createDirectoryError := os.MkdirAll(cache_directory.Of(repository), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
	}

	// Nothing on record yet: every file goes through the formatter.
	check("the first check", 4, drifted...)

	// Each repository holds its own files and no other's; the parent holds none of them.
	entries := func(repository string) []string {
		section := readFormatSection(repository)
		if section == nil {
			return nil
		}
		names := []string{}
		for fileName := range section.Entries {
			names = append(names, relativeIn(test, root, fileName))
		}
		sort.Strings(names)
		return names
	}
	if got := entries(root); got != nil {
		test.Fatalf("the nested check wrote the parent's record: %v", got)
	}
	if got := strings.Join(entries(library), ", "); got != "library/source/Thing.ts, library/source/Tidy.ts" {
		test.Fatalf("the library's record holds %s", got)
	}
	if got := strings.Join(entries(inner), ", "); got != "library/inner/Deep.ts, library/inner/Settled.ts" {
		test.Fatalf("the inner repository's record holds %s", got)
	}

	// The settled files are on record and skipped; the drifted ones are recorded at their formatted text,
	// so their bytes on disk stay out of the record and stay reported.
	check("the second check", 2, drifted...)

	// The positive control: a file the last check recorded clean, misformatted, is drift again.
	writeTree(test, root, map[string]string{"library/source/Tidy.ts": "export const tidy   =   1\n"})
	check(
		"a recorded file misformatted",
		3,
		"library/inner/Deep.ts",
		"library/source/Thing.ts",
		"library/source/Tidy.ts",
	)
	writeTree(test, root, map[string]string{"library/source/Tidy.ts": "export const tidy = 1\n"})

	// An entry vouching for Thing.ts's misformatted bytes. Under this formatter's key and options the
	// record is believed, which is what proves the check consults it; under another key, or options the
	// file no longer formats with, it says nothing and the drift is reported.
	thing := filepath.Join(library, "source", "Thing.ts")
	contents := readForTest(test, thing)
	options, optionsFingerprintError := engine.OptionsFingerprint(thing)
	if optionsFingerprintError != nil {
		test.Fatal(optionsFingerprintError)
	}
	key, formatRecordKeyError := formatRecordKey(library)
	if formatRecordKeyError != nil {
		test.Fatal(formatRecordKeyError)
	}
	vouch := func(key string, options string) {
		test.Helper()
		section := readFormatSection(library)
		section.Key = key
		section.Entries[thing] = program.FormatEntry{Sum: formatRecordSum([]byte(contents)), Options: options}
		if writeFormatSectionError := writeFormatSection(library, section); writeFormatSectionError != nil {
			test.Fatal(writeFormatSectionError)
		}
	}
	vouch(key, options)
	check("an entry vouching for the bytes under this key", 1, "library/inner/Deep.ts")
	vouch("formatter another", options)
	check("an entry under another formatter's key", 3, drifted...)
	vouch(key, options+" changed")
	check("an entry under other options", 2, drifted...)

	// `--format-all` reads every nested file: an entry vouching for Thing.ts's misformatted bytes, under
	// this formatter's key and options, says nothing to it.
	vouch(key, options)
	all, checkNestedRepositoriesError := checkNestedRepositories(engine, root, true)
	if checkNestedRepositoriesError != nil {
		test.Fatal(checkNestedRepositoriesError)
	}
	if all.Checked != 4 || len(all.Drift) != 2 {
		test.Fatalf(
			"--format-all checked %d of 4 files and found %d drifted, want 4 and 2 (the record's word taken for Thing.ts)",
			all.Checked,
			len(all.Drift),
		)
	}
}

// TestASignedEntryStillReadsAnEditedFile is the positive control through the record's cheap path. An
// entry signed with its file's size and modification time vouches for the file without reading it, so
// an edit, which moves both, must send the file back through the formatter.
func TestASignedEntryStillReadsAnEditedFile(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	library := filepath.Join(root, "library")
	if createDirectoryError := os.MkdirAll(cache_directory.Of(library), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (format_files.Enumeration, error) {
		return format_files.Enumerate(walkRoot, engine.Handles)
	}
	// Older than the second a fresh write is left unsigned for.
	tidy := filepath.Join(library, "source", "Tidy.ts")
	past := time.Now().Add(-time.Hour)
	if changeTimesError := os.Chtimes(tidy, past, past); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	drifted := func() []string {
		test.Helper()
		check, checkNestedRepositoriesError := checkNestedRepositories(engine, root, false)
		if checkNestedRepositoriesError != nil {
			test.Fatal(checkNestedRepositoriesError)
		}
		names := []string{}
		for _, drift := range check.Drift {
			names = append(names, filepath.Base(drift.FileName))
		}
		return names
	}

	// Recorded, then signed on the next check, which finds its bytes on record.
	drifted()
	drifted()
	if entry := readFormatSection(library).Entries[tidy]; entry.ModifiedNanoseconds != past.UnixNano() {
		test.Fatalf("Tidy.ts was not signed with its modification time: %+v", entry)
	}

	writeTree(test, root, map[string]string{"library/source/Tidy.ts": "export const tidy   =   1\n"})
	if got := strings.Join(drifted(), ", "); got != "Deep.ts, Thing.ts, Tidy.ts" {
		test.Fatalf("an edit to a signed file was taken on the record's word: drift %s", got)
	}
}

// TestTheNestedLineSaysWhatItRead: the files the formatter checked and the files taken on the record's
// word are counted apart, and "would change" is said only of checked files.
func TestTheNestedLineSaysWhatItRead(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		check nestedDriftCheck
		want  string
	}{
		{nestedDriftCheck{Repositories: 2, Files: 2073},
			"nested repositories: 2 read, 2073 files: none checked by the formatter, all 2073 taken on their record's word as formatted at their current bytes"},
		{nestedDriftCheck{Repositories: 2, Files: 2073, Checked: 2073, Drift: make([]nestedDrift, 3)},
			"nested repositories: 2 read, 2073 files: 2073 checked by the formatter, 3 would change under their own run"},
		{nestedDriftCheck{Repositories: 2, Files: 2073, Checked: 23, Drift: make([]nestedDrift, 1)},
			"nested repositories: 2 read, 2073 files: 23 checked by the formatter, 1 would change under their own run; 2050 taken on their record's word as formatted at their current bytes"},
		{nestedDriftCheck{},
			"nested repositories: 0 read, 0 files: 0 checked by the formatter, 0 would change under their own run"},
	} {
		if got := nestedSummary(testCase.check); got != testCase.want {
			test.Errorf("got  %s\nwant %s", got, testCase.want)
		}
	}
}

// TestALibraryIsFormattedFromItsOwnRootAndReadFromItsProject is the ruling end to end, through the
// binary: the project's check reports the library's drift and writes nothing there; a run started
// inside the library formats and fixes the library and writes nothing in the project above it; and the
// project's check is then clean of it.
func TestALibraryIsFormattedFromItsOwnRootAndReadFromItsProject(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":            fixScopeTsconfig,
		"CohereSettings.json":      `{"extends":"./NexusCohereSettings.json","rules":{"no-debugger":"error"}}`,
		"NexusCohereSettings.json": `{"format":{"ignore":[]}}`,
		// The project's own files: a fixable finding and a formatting one, which no run inside the
		// library may touch.
		"Producer.ts":         "export function value(): number {\n  debugger;\n  return 1;\n}\n",
		"Ugly.ts":             "export const ugly   =   1\n",
		".gitmodules":         "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":        "gitdir: ../.git/modules/library\n",
		"library/Inner.ts":    "export const inner   =   1\n",
		"library/Debugged.ts": "export function debugged(): number {\n  debugger;\n  return 1;\n}\n",
	})
	project := func() map[string]string {
		snapshot := treeSnapshot(test, root)
		for path := range snapshot {
			if strings.HasPrefix(path, filepath.Join(root, "library")+string(filepath.Separator)) {
				delete(snapshot, path)
			}
		}
		return snapshot
	}
	library := filepath.Join(root, "library")

	// The project's check reads the library and reports the file its formatter would rewrite, writing
	// nothing anywhere. The library's fixable finding is lint's to report, at its position, and the run
	// says its fix was not applied.
	before := treeSnapshot(test, root)
	output, code := runCohere(test, binary, root, "--no-fix", "--format")
	if code == 0 || !strings.Contains(
		output,
		"nested repositories: 1 read, 2 files: 2 checked by the formatter, 1 would change under their own run",
	) ||
		!strings.Contains(output, "1 fix not applied: in nested repository library") ||
		!strings.Contains(output, filepath.Join(library, "Inner.ts")+
			":1:1 - the formatter would rewrite this file in nested repository library") {
		test.Fatalf("the project's check did not report the library's drift, exit %d:\n%s", code, output)
	}
	assertTreeUnchanged(test, root, before, output)

	// A run started inside the library walks the library and writes only there.
	projectBefore := project()
	output, code = runCohere(test, binary, library, "--fix", "--format")
	if code != 0 {
		test.Fatalf("the library's run exited %d:\n%s", code, output)
	}
	if !strings.Contains(output, "walked "+library) {
		test.Fatalf("the library's run did not walk the library:\n%s", output)
	}
	if got := readForTest(test, filepath.Join(library, "Inner.ts")); got != "export const inner = 1;\n" {
		test.Fatalf("the library's run did not format the library:\n%s\noutput:\n%s", got, output)
	}
	if got := readForTest(test, filepath.Join(library, "Debugged.ts")); strings.Contains(got, "debugger") {
		test.Fatalf("the library's run did not fix the library:\n%s", got)
	}
	projectAfter := project()
	for path, contents := range projectBefore {
		if projectAfter[path] != contents {
			test.Errorf("the library's run wrote %s in the project above it:\n%s", path, projectAfter[path])
		}
	}
	for path := range projectAfter {
		if _, existed := projectBefore[path]; !existed {
			test.Errorf("the library's run created %s in the project above it", path)
		}
	}

	// The project's check now reads the library clean, and its own files are still its own to fix.
	// The library's run left its record in the library, and the project's check reads it, so nothing in
	// the library is formatted again.
	output, _ = runCohere(test, binary, root, "--no-fix", "--format")
	if !strings.Contains(
		output,
		"nested repositories: 1 read, 2 files: none checked by the formatter, all 2 taken on their record's word as formatted at their current bytes",
	) {
		test.Fatalf("the library is still reported, or formatted again, after its own run formatted it:\n%s", output)
	}

	// `--format-all` reads every nested file whatever the record says.
	output, _ = runCohere(test, binary, root, "--no-fix", "--format-all")
	if !strings.Contains(
		output,
		"nested repositories: 1 read, 2 files: 2 checked by the formatter, 0 would change under their own run\n",
	) {
		test.Fatalf("--format-all took the library's record's word:\n%s", output)
	}
	if !strings.Contains(output, filepath.Join(root, "Producer.ts")+":1:1 - --fix would rewrite this file") {
		test.Fatalf("the project's own drift went missing:\n%s", output)
	}

	// The positive control, right after a check that read the file clean from the record: a misformat
	// planted in it is drift again, on this check and the next. The next may replay this one, since no input
	// moved, and its line then says so between the label and the counts: the walk no longer enters the run's own
	// .cache, whose writes made every run's inputs change (#fe4vr5s).
	writeTree(test, root, map[string]string{"library/Inner.ts": "export const inner   =   1;\n"})
	for _, label := range []string{"the check after the plant", "the check after that"} {
		output, code = runCohere(test, binary, root, "--no-fix", "--format")
		if code == 0 || !strings.Contains(
			output,
			": 1 read, 2 files: 1 checked by the formatter, 1 would change under their own run; 1 taken on their record's word as formatted at their current bytes",
		) ||
			!strings.Contains(output, filepath.Join(library, "Inner.ts")+
				":1:1 - the formatter would rewrite this file in nested repository library") {
			test.Fatalf("%s did not report the planted misformat, exit %d:\n%s", label, code, output)
		}
	}
}

// TestAFreshClonesLibrariesGetTheirCacheAtStartup: a checkout where nobody has run cohere inside a library, a
// fresh clone or a CI machine, kept no cache there, so every check formatted every library file, every run
// (#v0etgwx). The run's start now makes the cache in every library, and the next check reads each library's record:
// the library, whose .gitignore ignores .cache, and the inner one, whose rules say nothing of it, since a cache the
// run makes ignores itself (#f6y78ws). Until it did, the inner library got no cache and every one of its files was
// checked every time.
func TestAFreshClonesLibrariesGetTheirCacheAtStartup(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	writeTree(test, root, map[string]string{"library/.gitignore": "ignored/\n.cache/\n"})
	library := filepath.Join(root, "library")
	inner := filepath.Join(library, "inner")
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (format_files.Enumeration, error) {
		return format_files.Enumerate(walkRoot, engine.Handles)
	}

	prepareNestedCacheDirectories(root)
	if information, statError := os.Stat(cache_directory.Of(library)); statError != nil || !information.IsDir() {
		test.Fatalf("the library, whose .gitignore ignores .cache, has no cache after the run's start (%v)", statError)
	}
	if contents, readError := os.ReadFile(filepath.Join(cache_directory.Of(inner), ".gitignore")); readError != nil ||
		string(contents) != cache_directory.IgnoreFileContents {
		test.Fatalf("the inner library, whose rules say nothing of .cache, has no cache ignoring itself: %q (%v)",
			contents, readError)
	}

	checked := func(label string) int {
		test.Helper()
		result, checkNestedRepositoriesError := checkNestedRepositories(engine, root, false)
		if checkNestedRepositoriesError != nil {
			test.Fatal(checkNestedRepositoriesError)
		}
		if len(result.Drift) != 2 {
			test.Fatalf("%s: %d files drift, want Thing.ts and Deep.ts", label, len(result.Drift))
		}
		return result.Checked
	}
	// The first check formats all four and records each library's. The second takes each library's clean file
	// on its record's word; the two misformatted files are checked again.
	if got := checked("the first check"); got != 4 {
		test.Fatalf("the first check formatted %d files, want 4", got)
	}
	if got := checked("the second check"); got != 2 {
		test.Fatalf("the second check formatted %d files, want 2: a library's record was not read", got)
	}

	// Run again, the start leaves an existing cache as it is.
	prepareNestedCacheDirectories(root)
	if got := checked("the third check"); got != 2 {
		test.Fatalf("after a second start the check formatted %d files, want 2", got)
	}
}

// relativeIn is path relative to root, failing the test when no relative path leads there.
func relativeIn(test testing.TB, root string, path string) string {
	test.Helper()
	relative, relativeError := filepath.Rel(root, path)
	if relativeError != nil {
		test.Fatal(relativeError)
	}
	return relative
}

// TestASubmoduleTheRootsIgnorePatternsNameIsNotRead: a declared submodule the root's ignorePatterns match is
// somebody else's code the repository pins, as cohere's TypeScript is typescript-go at a commit, so the check never
// reads it, nor the submodules it declares, and says so in a note (#4wjhppw). The control is
// TestTheProjectReadsItsLibrariesDrift, the same tree with no settings file, which reads both.
func TestASubmoduleTheRootsIgnorePatternsNameIsNotRead(test *testing.T) {
	test.Parallel()
	root := nestedTree(test)
	writeTree(test, root, map[string]string{"CohereSettings.json": `{"ignorePatterns":["library/**"]}`})
	engine := prettierLike()
	engine.enumerate = func(walkRoot string) (format_files.Enumeration, error) {
		return format_files.Enumerate(walkRoot, engine.Handles)
	}

	check, checkNestedRepositoriesError := checkNestedRepositories(engine, root, false)
	if checkNestedRepositoriesError != nil {
		test.Fatal(checkNestedRepositoriesError)
	}
	if check.Repositories != 0 || check.Files != 0 || len(check.Drift) != 0 {
		test.Fatalf("read %d repositories, %d files and %d drifted, want none", check.Repositories, check.Files,
			len(check.Drift))
	}
	want := []string{"note: nested repository library not read: the root's ignorePatterns match it"}
	if !slices.Equal(check.Notes, want) {
		test.Errorf("notes %q, want %q", check.Notes, want)
	}
}

// TestANestedRepositorysFailureIsAGapAndItsOwnIgnoresApply is #zmt3tv5, through the binary. A parent run reads a
// declared submodule from the submodule's own root, under the submodule's own settings, so a misformatted file its
// ignorePatterns name is never reported here, while one they don't name is reported as its drift, the control. A
// file there the formatter cannot print, a transform failure rather than a file that does not parse, is named in the
// account and counted on the footer as a gap: it never ends the check and never decides the parent's exit, in the
// commit gate or in `--no-fix`. Adamic's root stopped on one in its cohere submodule. A run that writes never reads
// the submodule at all, and leaves it as it was.
func TestANestedRepositorysFailureIsAGapAndItsOwnIgnoresApply(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":            "{ \"compilerOptions\": { \"strict\": true, \"noEmit\": true }, \"files\": [\"Root.ts\"] }\n",
		"CohereSettings.json":      "{ \"extends\": \"./NexusCohereSettings.json\" }\n",
		"NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		".gitignore":               ".cache/\n",
		"Root.ts":                  "export const root = 1;\n",
		".gitmodules":              "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":             "gitdir: ../.git/modules/library\n",
		"library/CohereSettings.json": "{ \"extends\": \"./NexusCohereSettings.json\", " +
			"\"ignorePatterns\": [\"vendored/**\"] }\n",
		"library/NexusCohereSettings.json": "{ \"format\": { \"ignore\": [] } }\n",
		"library/vendored/Theirs.ts":       "export const theirs   =   1\n",
		"library/Unprintable.ts":           "let value: ?string = null;\n",
		"library/Drifted.ts":               "export const drifted   =   1\n",
	})
	theirs := filepath.Join(root, "library", "vendored", "Theirs.ts")
	unprintable := filepath.Join(root, "library", "Unprintable.ts")
	drifted := filepath.Join(root, "library", "Drifted.ts")

	output, _ := runCohere(test, binary, root, "--no-fix", "--format-only")
	if !strings.Contains(output, drifted) {
		test.Fatalf("the nested repository's drift was not reported, so the check proves nothing:\n%s", output)
	}
	if strings.Contains(output, theirs) {
		test.Errorf("a file the nested repository's own ignorePatterns name was read:\n%s", output)
	}
	writeTree(test, root, map[string]string{"library/Drifted.ts": "export const drifted = 1;\n"})

	for _, arguments := range [][]string{{"--no-fix", "--format-only"}, {"--no-fix", "--format-only", "--format-all"},
		{"--no-fix"}} {
		output, code := runCohere(test, binary, root, arguments...)
		if code != 0 {
			test.Errorf("%v: exit %d, want 0, the nested file a gap and not the verdict:\n%s", arguments, code, output)
		}
		for _, want := range []string{"note: " + unprintable + " not checked: in nested repository library, " +
			edit.ReasonTransformFailed, "⚠ 1 nested file not checked"} {
			if !strings.Contains(output, want) {
				test.Errorf("%v: missing %q:\n%s", arguments, want, output)
			}
		}
		if strings.Contains(output, "reading the nested repositories") {
			test.Errorf("%v: the nested check ended on the file:\n%s", arguments, output)
		}
	}

	// A run that writes never reads the submodule, which a run inside it formats: it passes, and the submodule's
	// files are as they were.
	before := treeSnapshot(test, filepath.Join(root, "library"))
	if output, code := runCohere(test, binary, root); code != 0 {
		test.Errorf("the writing run exited %d:\n%s", code, output)
	}
	assertTreeUnchanged(test, filepath.Join(root, "library"), before, "the writing run")
}

// TestWouldChangeCountsEveryFindingLineAndSaysEachScope is #hb90yfp through the binary. `wouldChange` stays every
// would-change finding line, this project's and its nested repositories', since the release cut's gate holds the
// summary's counts equal to the finding lines; `nestedWouldChange` is the nested part, and each part's scope is kept,
// so the footer sets the nested drift against the nested files read rather than the program's files.
func TestWouldChangeCountsEveryFindingLineAndSaysEachScope(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		"tsconfig.json":      fixScopeTsconfig,
		"Ugly.ts":            "export const ugly   =   1\n",
		"Tidy.ts":            "export const tidy = 1;\n",
		".gitmodules":        "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
		"library/.git":       "gitdir: ../.git/modules/library\n",
		"library/Inner.ts":   "export const inner   =   1\n",
		"library/Settled.ts": "export const settled = 1;\n",
		"library/Another.ts": "export const another   =   1\n",
		"library/Fourth.ts":  "export const fourth = 1;\n",
	})

	output, code := runCohere(test, binary, root, "--no-fix", "--format", "--no-cache", "--json")
	if code == 0 {
		test.Fatalf("a check with drift here and in the library exited 0:\n%s", output)
	}
	own, nested := 0, 0
	var summary *summaryJson
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var envelope struct {
			Kind      string `json:"kind"`
			MessageId string `json:"messageId"`
		}
		if json.Unmarshal([]byte(line), &envelope) != nil {
			continue
		}
		switch {
		case envelope.Kind == "finding" && envelope.MessageId == "would-change":
			own++
		case envelope.Kind == "finding" && envelope.MessageId == "nested-drift":
			nested++
		case envelope.Kind == "summary":
			summary = &summaryJson{}
			if decodeError := json.Unmarshal([]byte(line), summary); decodeError != nil {
				test.Fatalf("the summary line does not decode: %v\n%s", decodeError, line)
			}
		}
	}
	if summary == nil {
		test.Fatalf("no summary line:\n%s", output)
	}
	if own < 1 || nested != 2 {
		test.Fatalf("want this project's drift and the library's two files as finding lines, got %d and %d:\n%s",
			own, nested, output)
	}
	if summary.WouldChange != own+nested {
		test.Errorf("wouldChange is %d, want %d, every would-change finding line, this project's %d and the nested %d",
			summary.WouldChange, own+nested, own, nested)
	}
	if summary.NestedWouldChange != nested {
		test.Errorf("nestedWouldChange is %d, want the %d nested-drift finding lines", summary.NestedWouldChange,
			nested)
	}
	if summary.NestedWouldChangeConsidered != 4 {
		test.Errorf("nestedWouldChangeConsidered is %d, want the library's 4 files the formatter read",
			summary.NestedWouldChangeConsidered)
	}
	if summary.WouldChangeConsidered < own {
		test.Errorf("wouldChangeConsidered is %d, fewer than the %d files it found would change",
			summary.WouldChangeConsidered, own)
	}

	// The footer says each part against its own scope, and the program's count stays in the parentheses. Both runs are
	// uncached, so neither format scope is narrowed by the other's record and the two runs consider the same files.
	output, _ = runCohere(test, binary, root, "--no-fix", "--format", "--no-cache")
	want := fmt.Sprintf("%s of %s would change, 2 of 4 more in nested repositories",
		grouped(own), counted(summary.WouldChangeConsidered, "file", "files"))
	if !strings.Contains(output, want) {
		test.Errorf("the footer does not say %q:\n%s", want, output)
	}
}
