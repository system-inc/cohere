package program_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// listEverything is what discovery does for a build: every directory under root, listed once.
func listEverything(test *testing.T, root string) *program.DirectoryListings {
	test.Helper()
	listings := program.NewDirectoryListings()
	walkError := filepath.WalkDir(root, func(path string, entry os.DirEntry, pathError error) error {
		if pathError != nil || !entry.IsDir() {
			return pathError
		}
		entries, readDirectoryError := os.ReadDir(path)
		if readDirectoryError != nil {
			return readDirectoryError
		}
		listings.Add(path, entries)
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return listings
}

// listingsReport is what a build makes of its tree: the files the tsconfig named, and every input the run
// cache would record.
func listingsReport(
	test *testing.T,
	directory string,
	listings *program.DirectoryListings,
) (ours []string, present []string, absent []string) {
	test.Helper()
	recorder := program.NewInputRecorder()
	graph, buildError := program.Build(program.Options{
		ConfigFileName:   "tsconfig.json",
		CurrentDirectory: directory,
		Inputs:           recorder,
		Listings:         listings,
	})
	if buildError != nil {
		test.Fatalf("building: %v", buildError)
	}
	for _, sourceFile := range graph.ProjectFiles() {
		ours = append(ours, sourceFile.FileName().AsString())
	}
	slices.Sort(ours)
	present, absent, _ = recorder.Inputs()
	slices.Sort(present)
	slices.Sort(absent)
	return ours, present, absent
}

// A build that lists its directories from discovery's listings names the same files, in the same order, and
// records the same inputs as one that lists them from disk (#bjv0tg4). The tree holds what the listing has to
// answer as the disk does: nested directories, a symbolic link to a file and one to a directory (both
// followed, as the disk follows them), a dangling link (left out), and a directory the include does not
// reach. The control: a listing that has gone stale, one file short, names one file fewer, so the build
// really reads the listings and the comparison can fail.
func TestABuildListsFromDiscoverysListingsAsFromDisk(test *testing.T) {
	test.Parallel()
	directory, resolveSymlinksError := filepath.EvalSymlinks(writeProject(test, map[string]string{
		"tsconfig.json":          `{"compilerOptions": {"target": "ES2022", "strict": true, "noEmit": true}, "include": ["src/**/*.ts"]}`,
		"src/index.ts":           "export const index = 1;\n",
		"src/nested/deep/a.ts":   "export const a = 1;\n",
		"src/nested/b.ts":        "export const b = 1;\n",
		"elsewhere/linked.ts":    "export const linked = 1;\n",
		"elsewhere/dir/inner.ts": "export const inner = 1;\n",
		"outside/ignored.ts":     "export const ignored = 1;\n",
	}))
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	for link, target := range map[string]string{
		"src/linked.ts":   "../elsewhere/linked.ts",
		"src/linkeddir":   "../elsewhere/dir",
		"src/dangling.ts": "../nowhere.ts",
	} {
		if symlinkError := os.Symlink(target, filepath.Join(directory, link)); symlinkError != nil {
			test.Fatal(symlinkError)
		}
	}

	diskOurs, diskPresent, diskAbsent := listingsReport(test, directory, nil)
	listings := listEverything(test, directory)
	listedOurs, listedPresent, listedAbsent := listingsReport(test, directory, listings)
	if !slices.Equal(diskOurs, listedOurs) {
		test.Fatalf("the files named differ:\ndisk   %v\nlisted %v", diskOurs, listedOurs)
	}
	if len(diskOurs) != 5 {
		test.Fatalf("the fixture's five included files (two through links) came back as %v", diskOurs)
	}
	if !slices.Equal(diskPresent, listedPresent) || !slices.Equal(diskAbsent, listedAbsent) {
		test.Fatalf(
			"the recorded inputs differ:\ndisk   %v %v\nlisted %v %v",
			diskPresent,
			diskAbsent,
			listedPresent,
			listedAbsent,
		)
	}

	stale := program.NewDirectoryListings()
	stale.Add(filepath.Join(directory, "src", "nested"), nil)
	if staleOurs, _, _ := listingsReport(test, directory, stale); len(staleOurs) != len(diskOurs)-2 {
		test.Fatalf("control: a listing emptied of src/nested named %v, so the build did not read it", staleOurs)
	}
}

// Listing answers for a directory that was listed, by either separator, and says plainly when one was not,
// so a caller reads that one from disk rather than taking it for empty.
func TestListingAnswersForWhatWasListedAndOnlyThat(test *testing.T) {
	test.Parallel()
	directory := writeProject(
		test,
		map[string]string{"a.ts": "export const a = 1;\n", "sub/b.ts": "export const b = 1;\n"},
	)
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	listings := program.NewDirectoryListings()
	listings.Add(directory, entries)

	got, listed := listings.Listing(directory + string(filepath.Separator))
	if !listed || len(got) != 2 || got[0].Name() != "a.ts" || got[1].Name() != "sub" || !got[1].IsDir() {
		test.Fatalf("the listed directory answered %v, %t", got, listed)
	}
	if got, listed := listings.Listing(filepath.Join(directory, "sub")); listed || got != nil {
		test.Fatalf("a directory nobody listed answered %v, %t, as if it were listed", got, listed)
	}
}
