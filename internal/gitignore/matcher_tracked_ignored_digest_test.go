package gitignore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// What a repository's index decides for a walk, digested, is what a run cache keys the index on (#m1daxxk): the
// entries the ignore rules exclude, and every gitlink, each by its path and mode. These hold that against the indexes
// git wrote at each step a repository took (digestRecordings) and against the index samples cohere-swift reads too, so
// the two languages' readers agree on what is tracked.

// swiftSampleIgnore is the `.gitignore` of the repository cohere-swift's samples came from (swiftSamples).
const swiftSampleIgnore = "*.swift\nIgnored/\n"

// sampleRepository is a repository whose index is contents, read under objectFormat, and whose root `.gitignore` is
// ignoreFile.
func sampleRepository(test *testing.T, contents []byte, objectFormat string, ignoreFile string) string {
	test.Helper()
	root := test.TempDir()
	writeTree(test, root, map[string]string{
		".git/index":  string(contents),
		".git/config": "[extensions]\n\tobjectFormat = " + objectFormat + "\n",
		".gitignore":  ignoreFile,
	})
	return root
}

// expectedDigest is TrackedIgnoredDigest's digest of entries, each a path and its mode in octal, in the order given,
// for a walk of scope: the rule written out, so a change to what the digest covers shows here.
func expectedDigest(scope string, entries ...string) string {
	digest := sha256.New()
	digest.Write([]byte(trackedIgnoredDigestFormat + "\x00" + scope + "\x00"))
	for _, entry := range entries {
		// The mode is the last word, since a path may hold a space.
		separator := strings.LastIndexByte(entry, ' ')
		digest.Write([]byte(entry[:separator] + "\x00" + entry[separator+1:] + "\n"))
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// TestTheGoReaderAgreesWithTheSwiftReaderOnItsSamples: every SHA-256 sample tracks, by Go's reader, exactly what
// cohere-swift's tests require of its reader, and digests the four files `*.swift` excludes, whatever version wrote
// them. The split and sparse samples, which the Swift reader falls back on, take no digest here.
func TestTheGoReaderAgreesWithTheSwiftReaderOnItsSamples(test *testing.T) {
	test.Parallel()
	want := expectedDigest("",
		"Ignored/Tracked.swift 100644", "Sources/Alpha/One.swift 100644", "Sources/Alpha/Two.swift 100644",
		"Sources/Beta.swift 100644")
	for _, version := range []int{2, 3, 4} {
		name := "sha256-version-" + strconv.Itoa(version) + ".index"
		root := sampleRepository(test, readSwiftSample(test, name), "sha256", swiftSampleIgnore)
		matcher, newError := New(root)
		if newError != nil {
			test.Fatal(newError)
		}
		if fallback := matcher.IndexFallback(); fallback != nil {
			test.Fatalf("%s fell back: %v", name, fallback)
		}
		if !slices.Equal(matcher.index.paths, swiftSampleTracked) {
			test.Errorf("%s tracks %q by Go's reader, and %q by Swift's", name, matcher.index.paths, swiftSampleTracked)
		}
		digest, digestError := matcher.TrackedIgnoredDigest("")
		if digestError != nil {
			test.Fatalf("%s: %v", name, digestError)
		}
		if digest != want {
			test.Errorf("%s digests %s, want the four files *.swift excludes, %s", name, digest, want)
		}
	}

	for name, reason := range map[string]error{"sha1-split.index": ErrSplitIndex, "sha1-sparse.index": ErrSparseIndex} {
		root := sampleRepository(test, readSwiftSample(test, name), "sha1", swiftSampleIgnore)
		matcher, newError := New(root)
		if newError != nil {
			test.Fatal(newError)
		}
		_, digestError := matcher.TrackedIgnoredDigest("")
		if !errors.Is(digestError, ErrNoTrackedIgnoredDigest) || !errors.Is(digestError, reason) {
			test.Errorf("%s takes a digest, or refuses it for another reason than %v: %v", name, reason, digestError)
		}
	}
}

// TestTheSharedFixturesDigestByPathAndMode: the shared fixtures under a `.gitignore` that excludes every `.txt` digest
// every entry, each stage of the conflicted path and the intent-to-add entry included, and versions 3 and 4, which
// hold the same entries, digest alike. Version 2 has no intent-to-add entry, so it differs.
func TestTheSharedFixturesDigestByPathAndMode(test *testing.T) {
	test.Parallel()
	digests := map[int]string{}
	for _, version := range []int{2, 3, 4} {
		name := "version-" + strconv.Itoa(version) + ".index"
		contents, readError := os.ReadFile(filepath.Join("testdata", "index", name))
		if readError != nil {
			test.Fatal(readError)
		}
		root := sampleRepository(test, contents, "sha1", "*.txt\n")
		matcher, newError := New(root)
		if newError != nil {
			test.Fatal(newError)
		}
		digest, digestError := matcher.TrackedIgnoredDigest("")
		if digestError != nil {
			test.Fatalf("%s: %v", name, digestError)
		}
		digests[version] = digest
	}
	long := "shared/prefix/with/a/long/directory/"
	want := expectedDigest("",
		"conflicted.txt 100644", "conflicted.txt 100644", "conflicted.txt 100644", "intended.txt 100644",
		"nested/directory/file.txt 100644", "path with space.txt 100644", long+"name/alpha.txt 100644",
		long+"name/beta.txt 100644", long+"name/gamma.txt 100644", long+"other.txt 100644", "top.txt 100644")
	if digests[3] != want || digests[4] != want {
		test.Errorf("versions 3 and 4 digest %s and %s, want every entry, %s", digests[3], digests[4], want)
	}
	if digests[2] == digests[3] {
		test.Error("version 2, which has no intent-to-add entry, digests as version 3 does")
	}
}

// digestRecordings is git's index of one small repository at each step a digest test takes it through, and the files
// of a worktree git added beside it, each as git wrote it. How git made them is in testdata/recorded/README.md.
var digestRecordings = filepath.Join("testdata", "recorded", "digest")

// readDigestRecording is one of digestRecordings.
func readDigestRecording(test *testing.T, name string) []byte {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(digestRecordings, name))
	if readError != nil {
		test.Fatal(readError)
	}
	return contents
}

// digestRepositoryTree is the tree every digest recording was made over: an ignore file excluding `*.log` and
// `build/`, one committed source file, and an ignored log beside it, untracked until a step tracks it.
var digestRepositoryTree = map[string]string{
	".gitignore":    "*.log\nbuild/\n",
	"main.go":       "package main\n",
	"debug.log":     "a log\n",
	"build/out.txt": "built\n",
}

// digestRepository is the recorded repository laid out again, for the digest to follow through what git did to its
// index: each step git took is its recorded index, placed before the digest is read.
type digestRepository struct {
	test *testing.T
	root string
}

// layDigestRepository is digestRepositoryTree with the index git wrote at step.
func layDigestRepository(test *testing.T, step string) *digestRepository {
	test.Helper()
	repository := &digestRepository{test: test, root: test.TempDir()}
	writeTree(test, repository.root, digestRepositoryTree)
	repository.place(step)
	return repository
}

// place makes the index git wrote at step the repository's own.
func (repository *digestRepository) place(step string) {
	repository.test.Helper()
	placeIndex(repository.test, filepath.Join(repository.root, ".git"), readDigestRecording(repository.test,
		step+".index"))
}

// matcher is a matcher for the repository's root, read anew, as a later run reads it, from an index it read.
func (repository *digestRepository) matcher() *Matcher {
	repository.test.Helper()
	matcher, newError := New(repository.root)
	if newError != nil {
		repository.test.Fatal(newError)
	}
	if fallback := matcher.IndexFallback(); fallback != nil {
		repository.test.Fatalf("the recorded index was not read: %v", fallback)
	}
	return matcher
}

// digest is the repository's digest for a walk of scope, "" for its root.
func (repository *digestRepository) digest(scope string) string {
	repository.test.Helper()
	digest, digestError := repository.matcher().TrackedIgnoredDigest(scope)
	if digestError != nil {
		repository.test.Fatal(digestError)
	}
	return digest
}

// TestTheDigestFollowsWhatTheIndexDecides walks one repository through what git did to its index: what changes which
// paths the ignore rules exclude are tracked changes the digest, and what rewrites the index without changing that
// keeps it, each shown to have rewritten the index so the case is the one it claims.
func TestTheDigestFollowsWhatTheIndexDecides(test *testing.T) {
	test.Parallel()
	repository := layDigestRepository(test, "committed")
	first := repository.digest("")

	// `git add -f` of an ignored file tracks a path the rules exclude.
	repository.place("forced-log")
	forced := repository.digest("")
	if forced == first {
		test.Error("tracking an ignored file with `git add -f` keeps the digest")
	}
	// `git rm --cached` of it untracks the path again.
	repository.place("removed-log")
	removed := repository.digest("")
	if removed == forced {
		test.Error("untracking an ignored file with `git rm --cached` keeps the digest")
	}
	if removed != first {
		test.Error("the index back where it began digests differently from where it began")
	}

	// `git status` rewrites an index whose entries' stat data moved, and decides nothing new.
	if bytes.Equal(readDigestRecording(test, "refreshed-by-status.index"), readDigestRecording(test,
		"removed-log.index")) {
		test.Fatal("`git status` left the index as it was, so this case proves nothing")
	}
	repository.place("refreshed-by-status")
	if repository.digest("") != first {
		test.Error("a `git status` that rewrote the index changed the digest")
	}

	// `git add` of a source file, changed or new, tracks nothing the rules exclude: that file's own bytes are an
	// input of their own.
	writeTree(test, repository.root, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"new.go":  "package main\n",
	})
	if bytes.Equal(readDigestRecording(test, "added-sources.index"), readDigestRecording(test,
		"refreshed-by-status.index")) {
		test.Fatal("`git add` left the index as it was, so this case proves nothing")
	}
	repository.place("added-sources")
	if repository.digest("") != first {
		test.Error("`git add` of files the rules do not exclude changed the digest")
	}

	// A `.gitignore` edit that newly excludes a tracked path changes what that path's tracking decides.
	writeTree(test, repository.root, map[string]string{".gitignore": "*.log\nbuild/\nnew.go\n"})
	if repository.digest("") == first {
		test.Error("a `.gitignore` edit that newly excludes a tracked path keeps the digest")
	}
}

// TestTheDigestTellsAGitlinkFromAFile: an ignored path tracked as a file and the same path tracked as a gitlink digest
// differently, since a `dir/` pattern matches a gitlink as a directory and not a file, and a go.work root reads its
// submodules from gitlinks. So does a gitlink the rules do not exclude, appearing.
func TestTheDigestTellsAGitlinkFromAFile(test *testing.T) {
	test.Parallel()
	repository := layDigestRepository(test, "forced-log")
	asFile := repository.digest("")
	repository.place("gitlink-at-log")
	if !slices.Contains(repository.matcher().index.entries, indexEntry{path: "debug.log", mode: indexModeGitlink}) {
		test.Fatal("gitlink-at-log.index holds no gitlink at debug.log, so this case proves nothing")
	}
	if repository.digest("") == asFile {
		test.Error("an ignored path tracked as a gitlink digests as the same path tracked as a file")
	}

	withoutModule := repository.digest("")
	repository.place("gitlink-at-log-and-module")
	if repository.digest("") == withoutModule {
		test.Error("a gitlink the rules do not exclude, appearing, keeps the digest")
	}
}

// TestTheDigestCoversOnlyItsScope: a walk of a directory below the root is decided by the entries below it, so an
// ignored file tracked outside the directory keeps its digest, and one tracked inside changes it.
func TestTheDigestCoversOnlyItsScope(test *testing.T) {
	test.Parallel()
	repository := layDigestRepository(test, "committed")
	writeTree(test, repository.root, map[string]string{"service/trace.log": "trace\n"})
	first := repository.digest("service")
	repository.place("forced-log")
	if repository.digest("service") != first {
		test.Error("an ignored file tracked outside the scope changed the scope's digest")
	}
	repository.place("forced-log-and-service-log")
	if repository.digest("service") == first {
		test.Error("an ignored file tracked inside the scope kept the scope's digest")
	}
}

// TestTheDigestReadsAWalksListingFromEveryWorker: a matcher a walk made WithListing, as the format walk makes one,
// takes the same digest as one that reads the disk, while its workers step sibling directories at once through the
// one listing and the one index they share. Run under -race, it is what says those shared reads are safe.
func TestTheDigestReadsAWalksListingFromEveryWorker(test *testing.T) {
	test.Parallel()
	// The index git wrote after `git add -A` over this tree and `git add -f` of two of its logs.
	repository := layDigestRepository(test, "packages")
	tree := map[string]string{".gitignore": "*.log\nbuild/\n"}
	for directory := range 24 {
		name := "package" + strconv.Itoa(directory)
		tree[name+"/source.go"] = "package " + name + "\n"
		tree[name+"/trace.log"] = "trace\n"
		tree[name+"/nested/.gitignore"] = "!keep.log\n"
		tree[name+"/nested/keep.log"] = "kept\n"
	}
	writeTree(test, repository.root, tree)
	listings := map[string][]os.DirEntry{}
	walkError := filepath.WalkDir(repository.root, func(visited string, entry os.DirEntry, pathError error) error {
		if pathError != nil || !entry.IsDir() {
			return pathError
		}
		if entry.Name() == ".git" {
			return filepath.SkipDir
		}
		entries, readError := os.ReadDir(visited)
		listings[visited] = entries
		return readError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	listed, listedError := repository.matcher().WithListing(func(directory string) ([]os.DirEntry, bool) {
		entries, found := listings[directory]
		return entries, found
	}).TrackedIgnoredDigest("")
	if listedError != nil {
		test.Fatal(listedError)
	}
	if read := repository.digest(""); listed != read {
		test.Errorf("a matcher reading a walk's listing digests %s, and one reading the disk %s", listed, read)
	}
}

// TestAWorktreeReadsItsRepositorysSharedExclude: a worktree's `.git` is a file, and git reads the exclude file its
// common directory shares with the main checkout (#vdbcswp). A path only that file lists reads ignored by the line git
// check-ignore named, by the same absolute path, and `git add -f` of it moves the digest. Its untracked neighbor is
// not ignored, so the case is the one file. The worktree is laid out as git wrote it, with relative paths: its `.git`
// file, and the gitdir, commondir and index in its git directory below the main checkout's.
func TestAWorktreeReadsItsRepositorysSharedExclude(test *testing.T) {
	test.Parallel()
	base := test.TempDir()
	mainCheckout := filepath.Join(base, "main")
	worktree := filepath.Join(base, "worktree")
	gitDirectory := filepath.Join(mainCheckout, ".git", "worktrees", "worktree")
	writeTree(test, base, map[string]string{
		"main/.git/info/exclude":                 "# shared\nsecret.go\n",
		"main/.git/worktrees/worktree/gitdir":    string(readDigestRecording(test, "worktree-gitdir")),
		"main/.git/worktrees/worktree/commondir": string(readDigestRecording(test, "worktree-commondir")),
		"worktree/.git":                          string(readDigestRecording(test, "worktree.git")),
		"worktree/.gitignore":                    digestRepositoryTree[".gitignore"],
		"worktree/main.go":                       digestRepositoryTree["main.go"],
		"worktree/secret.go":                     "package main\n",
		"worktree/plain.go":                      "package main\n",
	})
	placeIndex(test, gitDirectory, readDigestRecording(test, "worktree.index"))
	inWorktree := &digestRepository{test: test, root: worktree}

	matcher := inWorktree.matcher()
	if got, want := matcher.IndexFile(), filepath.Join(gitDirectory, "index"); got != want {
		test.Errorf("the worktree's index file is %s, want %s", got, want)
	}
	shared := filepath.ToSlash(filepath.Join(mainCheckout, ".git", "info", "exclude"))
	ignored, source := matcher.Ignored("secret.go", false)
	if want := shared + ":2:secret.go"; !ignored || source.String() != want {
		test.Errorf("secret.go reads ignored %v by %q, want ignored by %q", ignored, source.String(), want)
	}
	// git named the main checkout by its absolute path, which the recording writes `<main>`.
	verbose := strings.ReplaceAll(string(readDigestRecording(test, "worktree.check-ignore")), "<main>",
		filepath.ToSlash(mainCheckout))
	if want := source.String() + "\tsecret.go\n"; verbose != want {
		test.Errorf("git check-ignore named %q, and the matcher %q", verbose, want)
	}
	if ignored, _ := matcher.Ignored("plain.go", false); ignored {
		test.Error("plain.go, which nothing lists, reads ignored")
	}

	before := inWorktree.digest("")
	placeIndex(test, gitDirectory, readDigestRecording(test, "worktree-forced-secret.index"))
	if inWorktree.digest("") == before {
		test.Error("tracking a file only the shared exclude lists keeps the worktree's digest")
	}
}
