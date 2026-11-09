package gitignore

import (
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// writeIndex writes a version 2 index of paths, each a regular file at stage 0, into gitDirectory, with SHA-1 object
// names and the checksum git writes. The fixtures in testdata/index and the Swift engine's samples are git's own bytes;
// this writes the small indexes the matcher's tests need around a tree.
func writeIndex(test *testing.T, gitDirectory string, paths ...string) {
	test.Helper()
	sorted := slices.Clone(paths)
	sort.Strings(sorted)
	contents := []byte("DIRC")
	contents = binary.BigEndian.AppendUint32(contents, 2)
	contents = binary.BigEndian.AppendUint32(contents, uint32(len(sorted)))
	for _, path := range sorted {
		entry := make([]byte, indexEntryStatBytes+sha1.Size)
		binary.BigEndian.PutUint32(entry[24:28], 0o100644)
		entry = binary.BigEndian.AppendUint16(entry, uint16(min(len(path), indexFlagNameLength)))
		entry = append(entry, path...)
		// One to eight NUL bytes, to a multiple of eight.
		entry = append(entry, make([]byte, 8-len(entry)%8)...)
		contents = append(contents, entry...)
	}
	checksum := sha1.Sum(contents)
	contents = append(contents, checksum[:]...)
	placeIndex(test, gitDirectory, contents)
}

// placeIndex writes contents as gitDirectory's index.
func placeIndex(test *testing.T, gitDirectory string, contents []byte) {
	test.Helper()
	if makeError := os.MkdirAll(gitDirectory, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(filepath.Join(gitDirectory, "index"), contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
}

// swiftSamples is the Swift engine's git index samples, which hold what the shared fixtures in testdata/index lack: a
// SHA-256 repository at each version, a sparse index and a split index, each written by git 2.54. How git made them
// is in swift/Tests/CohereSwiftTests/GitIndexTests.swift. They are read where they live, never copied, so the two
// readers meet the same bytes.
var swiftSamples = filepath.Join("..", "..", "swift", "Tests", "GitIndexSamples")

// swiftSampleTracked is every path each SHA-256 sample tracks, in the index's order: a repository whose `.gitignore`
// says `*.swift` and `Ignored/`, with Sources and Ignored/Tracked.swift added by force.
var swiftSampleTracked = []string{
	".gitignore",
	"Ignored/Tracked.swift",
	"Sources/Alpha/One.swift",
	"Sources/Alpha/Two.swift",
	"Sources/Beta.swift",
}

// readSwiftSample is one of the Swift engine's samples. One that is not there fails the test rather than skipping
// it, since a skip would read as a reader that agrees with them.
func readSwiftSample(test *testing.T, name string) []byte {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(swiftSamples, name))
	if readError != nil {
		test.Fatalf("reading the Swift engine's index sample %s: %v", name, readError)
	}
	return contents
}

// git's own index of one small repository, at each version, yields exactly the paths git lists for it: every stage of
// a conflict as one path, a skip-worktree and an intent-to-add entry, a path with a space, and version 4's paths
// rebuilt from the long prefix each shares with the one before. The fixtures and how git wrote them are in
// testdata/index, which the Swift engine reads too.
func TestTheIndexReaderYieldsEveryTrackedPathAtEachVersion(test *testing.T) {
	test.Parallel()
	for _, version := range []int{2, 3, 4} {
		name := "version-" + strconv.Itoa(version)
		contents, readError := os.ReadFile(filepath.Join("testdata", "index", name+".index"))
		if readError != nil {
			test.Fatal(readError)
		}
		listed, readListError := os.ReadFile(filepath.Join("testdata", "index", name+".paths"))
		if readListError != nil {
			test.Fatal(readListError)
		}
		if got := binary.BigEndian.Uint32(contents[4:8]); got != uint32(version) {
			test.Fatalf("%s.index is version %d, so it proves nothing of version %d", name, got, version)
		}
		paths, parseError := parseIndex(contents, sha1.Size)
		if parseError != nil {
			test.Fatalf("%s.index: %v", name, parseError)
		}
		want := strings.Split(strings.TrimSuffix(string(listed), "\n"), "\n")
		if !slices.Equal(paths, want) {
			test.Errorf("%s.index yields %q, want %q", name, paths, want)
		}
	}
}

// A path is tracked when the index names it, and a directory when the index names a path below it, and not when it
// names only a sibling that sorts beside it.
func TestATrackedIndexAnswersForPathsAndTheDirectoriesHoldingThem(test *testing.T) {
	test.Parallel()
	index := &trackedIndex{paths: []string{"a.go", "build.go", "build/deep/kept.go", "build0", "top"}}
	for path, want := range map[string]bool{
		"a.go":               true,
		"build":              true,
		"build/deep":         true,
		"build/deep/kept.go": true,
		"build/kept.go":      false,
		"build/deep/other":   false,
		"bui":                false,
		"top":                true,
		"top/below":          false,
		"zzz":                false,
	} {
		if got := index.tracks(path); got != want {
			test.Errorf("tracks(%q) is %v, want %v", path, got, want)
		}
	}
	var unread *trackedIndex
	if unread.tracks("a.go") {
		test.Error("an index never read tracks a.go")
	}
}

// gitignore(5): "Files already tracked by Git are not affected". value.go, added with `git add -f` under `*.go`, is
// not ignored, and its untracked sibling is, by the pattern.
func TestAForceAddedFileIsNotIgnored(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{".gitignore": "*.go\n", "value.go": "", "sibling.go": ""})
	writeIndex(test, filepath.Join(root, ".git"), ".gitignore", "value.go")
	matcher, newGitignoreError := New(root)
	if newGitignoreError != nil {
		test.Fatal(newGitignoreError)
	}
	if fallback := matcher.IndexFallback(); fallback != nil {
		test.Fatalf("the index was not read: %v", fallback)
	}
	if ignored, source := matcher.Ignored("value.go", false); ignored || !source.IsZero() {
		test.Errorf("the tracked value.go reads ignored %v by %q, want not ignored by no rule", ignored, source)
	}
	if ignored, source := matcher.Ignored("sibling.go", false); !ignored || source.String() != ".gitignore:1:*.go" {
		test.Errorf("the untracked sibling.go reads ignored %v by %q, want ignored by .gitignore:1:*.go", ignored,
			source)
	}
	if got, want := matcher.IndexFile(), filepath.Join(root, ".git", "index"); got != want {
		test.Errorf("the index file is %s, want %s", got, want)
	}
}

// A tracked file inside a directory a rule excludes is not ignored, and neither is the directory, so a walk that
// prunes what Ignored excludes enters it and finds the file. The untracked files beside it stay ignored by the rule
// that excluded the directory, and a directory there holding nothing tracked is still pruned.
func TestATrackedFileInsideAnIgnoredDirectoryIsWalkedTo(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".gitignore":          "build/\n",
		"build/kept.go":       "",
		"build/scratch.go":    "",
		"build/deep/inner.go": "",
		"build/deep/loose.go": "",
		"build/cache/blob":    "",
		"main.go":             "",
	})
	writeIndex(test, filepath.Join(root, ".git"), ".gitignore", "build/kept.go",
		"build/deep/inner.go", "main.go")
	matcher, newGitignoreError := New(root)
	if newGitignoreError != nil {
		test.Fatal(newGitignoreError)
	}

	var walked []string
	var walk func(scope *Matcher, directory string)
	walk = func(scope *Matcher, directory string) {
		entries, readDirectoryError := os.ReadDir(filepath.Join(root, filepath.FromSlash(directory)))
		if readDirectoryError != nil {
			test.Fatal(readDirectoryError)
		}
		for _, entry := range entries {
			relative := joinRelative(directory, entry.Name())
			if entry.Name() == ".git" {
				continue
			}
			if ignored, _ := scope.Ignored(relative, entry.IsDir()); ignored {
				continue
			}
			if !entry.IsDir() {
				walked = append(walked, relative)
				continue
			}
			child, enterError := scope.Enter(relative)
			if enterError != nil {
				test.Fatal(enterError)
			}
			walk(child, relative)
		}
	}
	walk(matcher, "")
	want := []string{".gitignore", "build/deep/inner.go", "build/kept.go", "main.go"}
	if !slices.Equal(walked, want) {
		test.Errorf("the walk found %q, want %q", walked, want)
	}

	if ignored, source := decide(test, matcher, "build/scratch.go", false); !ignored ||
		source.String() != ".gitignore:1:build/" {
		test.Errorf("the untracked build/scratch.go reads ignored %v by %q, want ignored by .gitignore:1:build/",
			ignored, source)
	}
	if ignored, _ := decide(test, matcher, "build/cache", true); !ignored {
		test.Error("build/cache, holding nothing tracked, is not ignored, so a walk would enter it")
	}
	build, enterError := matcher.Enter("build")
	if enterError != nil {
		test.Fatal(enterError)
	}
	if excluded, source := build.Excluded(); !excluded || source.String() != ".gitignore:1:build/" {
		test.Errorf("build reads excluded %v by %q, want excluded by .gitignore:1:build/", excluded, source)
	}
}

// Without an index read faithfully the matcher decides by the ignore files alone, exactly as before it read one, and
// says why: a root with no `.git`, a repository with no index yet, a split index and a sparse index git wrote, and an
// index whose checksum does not match. The tree is the Swift samples' repository, whose index tracks
// Sources/Beta.swift and Ignored/Tracked.swift, and each still reads ignored by its pattern.
func TestWithoutAReadableIndexTheMatcherFallsBackAndSaysWhy(test *testing.T) {
	test.Parallel()
	cases := map[string]struct {
		build      func(test *testing.T, root string)
		want       error
		wantNoFile bool
	}{
		"no .git": {build: func(*testing.T, string) {
			// The tree holds no `.git`, which is the case.
		}, want: ErrNoIndex, wantNoFile: true},
		"no index yet": {build: func(test *testing.T, root string) {
			writeTree(test, root, map[string]string{".git/HEAD": "ref: refs/heads/main\n"})
		}, want: ErrNoIndex},
		"a split index": {build: func(test *testing.T, root string) {
			placeIndex(test, filepath.Join(root, ".git"), readSwiftSample(test, "sha1-split.index"))
		}, want: ErrSplitIndex},
		"a sparse index": {build: func(test *testing.T, root string) {
			placeIndex(test, filepath.Join(root, ".git"), readSwiftSample(test, "sha1-sparse.index"))
		}, want: ErrSparseIndex},
		"a checksum that does not match": {build: func(test *testing.T, root string) {
			writeIndex(test, filepath.Join(root, ".git"), swiftSampleTracked...)
			indexPath := filepath.Join(root, ".git", "index")
			contents, readError := os.ReadFile(indexPath)
			if readError != nil {
				test.Fatal(readError)
			}
			contents[len(contents)-1] ^= 0xff
			if writeError := os.WriteFile(indexPath, contents, 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}, want: ErrMalformedIndex},
	}
	for name, testCase := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			writeTree(test, root, map[string]string{
				".gitignore":            "*.swift\nIgnored/\n",
				"Sources/Beta.swift":    "",
				"Ignored/Tracked.swift": "",
			})
			testCase.build(test, root)
			matcher, newGitignoreError := New(root)
			if newGitignoreError != nil {
				test.Fatal(newGitignoreError)
			}
			if fallback := matcher.IndexFallback(); !errors.Is(fallback, testCase.want) {
				test.Errorf("the fallback is %v, want %v", fallback, testCase.want)
			}
			for path, want := range map[string]string{
				"Sources/Beta.swift":    ".gitignore:1:*.swift",
				"Ignored/Tracked.swift": ".gitignore:2:Ignored/",
			} {
				if ignored, source := decide(test, matcher, path, false); !ignored || source.String() != want {
					test.Errorf("%s reads ignored %v by %q, want ignored by %s, as before", path, ignored, source,
						want)
				}
			}
			if testCase.wantNoFile != (matcher.IndexFile() == "") {
				test.Errorf("the index file is %q", matcher.IndexFile())
			}
		})
	}
}

// A worktree's `.git` is a file naming its git directory, whose index git reads, with the object format from the
// common directory's config: here SHA-256, whose names and checksum are 32 bytes, in the index git wrote for the Swift
// samples' repository at each version. Every path it tracks is read, and Ignored/Tracked.swift, tracked inside an
// ignored directory, reads not ignored while an untracked neighbor reads ignored.
func TestASha256WorktreesIndexIsReadThroughItsGitFileAtEachVersion(test *testing.T) {
	test.Parallel()
	for _, version := range []int{2, 3, 4} {
		name := "sha256-version-" + strconv.Itoa(version) + ".index"
		contents := readSwiftSample(test, name)
		if got := binary.BigEndian.Uint32(contents[4:8]); got != uint32(version) {
			test.Fatalf("%s is version %d, so it proves nothing of version %d", name, got, version)
		}
		base := test.TempDir()
		worktree := filepath.Join(base, "worktree")
		gitDirectory := filepath.Join(base, "main", ".git", "worktrees", "worktree")
		writeTree(test, base, map[string]string{
			"main/.git/config":                       "[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha256\n",
			"main/.git/worktrees/worktree/commondir": "../..\n",
			"worktree/.git":                          "gitdir: ../main/.git/worktrees/worktree\n",
			"worktree/.gitignore":                    "*.swift\nIgnored/\n",
			"worktree/Ignored/Tracked.swift":         "",
			"worktree/Ignored/Untracked.swift":       "",
		})
		placeIndex(test, gitDirectory, contents)
		matcher, newGitignoreError := New(worktree)
		if newGitignoreError != nil {
			test.Fatal(newGitignoreError)
		}
		if fallback := matcher.IndexFallback(); fallback != nil {
			test.Fatalf("%s: the worktree's index was not read: %v", name, fallback)
		}
		if got, want := matcher.IndexFile(), filepath.Join(gitDirectory, "index"); got != want {
			test.Errorf("%s: the index file is %s, want %s", name, got, want)
		}
		if !slices.Equal(matcher.index.paths, swiftSampleTracked) {
			test.Errorf("%s yields %q, want %q", name, matcher.index.paths, swiftSampleTracked)
		}
		if ignored, _ := decide(test, matcher, "Ignored", true); ignored {
			test.Errorf("%s: Ignored, holding a tracked file, reads ignored", name)
		}
		if ignored, _ := decide(test, matcher, "Ignored/Tracked.swift", false); ignored {
			test.Errorf("%s: Ignored/Tracked.swift, tracked, reads ignored", name)
		}
		if ignored, _ := decide(test, matcher, "Ignored/Untracked.swift", false); !ignored {
			test.Errorf("%s: Ignored/Untracked.swift, untracked in an ignored directory, reads not ignored", name)
		}
	}
}

// IndexFile follows a `.git` file to its git directory, relative to the file's directory or absolute; a `.git`
// directory holds its own; and a pointer that cannot be read gives an index below the file, never there.
func TestIndexFileFollowsAGitFileToItsGitDirectory(test *testing.T) {
	test.Parallel()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	worktree := filepath.Join(base, "worktree")
	if makeError := os.MkdirAll(worktree, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	marker := filepath.Join(worktree, ".git")
	relativeGitDirectory := filepath.Join(base, "main", ".git", "worktrees", "worktree")
	absoluteGitDirectory := filepath.Join(base, "elsewhere")
	for contents, want := range map[string]string{
		"gitdir: ../main/.git/worktrees/worktree\n": filepath.Join(relativeGitDirectory, "index"),
		"gitdir: " + absoluteGitDirectory + "\n":    filepath.Join(absoluteGitDirectory, "index"),
		"not a pointer\n":                           filepath.Join(marker, "index"),
	} {
		if writeError := os.WriteFile(marker, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		if got := IndexFile(marker, false); got != want {
			test.Errorf("a .git file reading %q gives the index %s, want %s", contents, got, want)
		}
	}
	if got, want := IndexFile(filepath.Join(base, ".git"), true), filepath.Join(base, ".git", "index"); got != want {
		test.Errorf("a .git directory gives the index %s, want %s", got, want)
	}
}

// extensions.objectFormat is read as git reads a config: section and key in any case, a quoted value, a trailing
// comment, and the last assignment winning.
func TestTheObjectFormatIsReadFromTheConfiguration(test *testing.T) {
	test.Parallel()
	for configuration, want := range map[string]string{
		"":                         "",
		"[core]\n\tbare = false\n": "",
		"[extensions]\n\tobjectFormat = sha256\n":                                                       "sha256",
		"[Extensions]\n\tOBJECTFORMAT=\"sha256\" # comment\n":                                           "sha256",
		"[extensions] objectformat = sha256\n":                                                          "sha256",
		"[extensions]\n\tobjectformat = sha1\n\tobjectformat = sha256\n[core]\n\tobjectformat = sha1\n": "sha256",
		"[core]\n\tobjectformat = sha256\n":                                                             "",
	} {
		if got := configurationObjectFormat([]byte(configuration)); got != want {
			test.Errorf("%q reads the object format %q, want %q", configuration, got, want)
		}
	}
}
