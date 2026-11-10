package gitignore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// What a repository's index decides for a walk, digested, is what a run cache keys the index on (#m1daxxk): the
// entries the ignore rules exclude, and every gitlink, each by its path and mode. These hold that against git itself
// and against the index samples the Swift engine reads too, so the two engines' readers agree on what is tracked.

// swiftSampleIgnore is the `.gitignore` of the repository the Swift engine's samples came from (swiftSamples).
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

// TestTheGoReaderAgreesWithTheSwiftReaderOnItsSamples: every SHA-256 sample tracks, by Go's reader, exactly what the
// Swift engine's tests require of its reader, and digests the four files `*.swift` excludes, whatever version wrote
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

// digestRepository is a real repository for the digest to follow through what git does to its index.
type digestRepository struct {
	test *testing.T
	root string
}

// newDigestRepository is a repository with an ignore file excluding `*.log` and `build/`, one committed source file,
// and an ignored log beside it, untracked.
func newDigestRepository(test *testing.T) *digestRepository {
	test.Helper()
	if _, lookPathError := exec.LookPath("git"); lookPathError != nil {
		test.Skip("no git on PATH")
	}
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	repository := &digestRepository{test: test, root: root}
	repository.git("init", "-q", "-b", "main")
	writeTree(test, root, map[string]string{
		".gitignore":    "*.log\nbuild/\n",
		"main.go":       "package main\n",
		"debug.log":     "a log\n",
		"build/out.txt": "built\n",
	})
	repository.git("add", ".gitignore", "main.go")
	repository.git("commit", "-q", "-m", "first")
	return repository
}

// git runs git in the repository with no configuration but its own, and an author for commits.
func (repository *digestRepository) git(arguments ...string) string {
	repository.test.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=cohere", "-c", "user.email=cohere@localhost",
		"-c", "core.excludesFile=/dev/null"}, arguments...)...)
	command.Dir = repository.root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"HOME="+repository.root)
	output, runError := command.CombinedOutput()
	if runError != nil {
		repository.test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), runError, output)
	}
	return string(output)
}

// digest is the repository's digest for a walk of its root, read anew, as a later run reads it.
func (repository *digestRepository) digest() string {
	repository.test.Helper()
	matcher, newError := New(repository.root)
	if newError != nil {
		repository.test.Fatal(newError)
	}
	digest, digestError := matcher.TrackedIgnoredDigest("")
	if digestError != nil {
		repository.test.Fatal(digestError)
	}
	return digest
}

// indexBytes is the index as it is on disk, so a case can show git rewrote it.
func (repository *digestRepository) indexBytes() []byte {
	repository.test.Helper()
	contents, readError := os.ReadFile(filepath.Join(repository.root, ".git", "index"))
	if readError != nil {
		repository.test.Fatal(readError)
	}
	return contents
}

// TestTheDigestFollowsWhatTheIndexDecides walks one repository through what git does to an index: what changes which
// paths the ignore rules exclude are tracked changes the digest, and what rewrites the index without changing that
// keeps it, each shown to have rewritten the index so the case is the one it claims.
func TestTheDigestFollowsWhatTheIndexDecides(test *testing.T) {
	test.Parallel()
	repository := newDigestRepository(test)
	first := repository.digest()

	// `git add -f` of an ignored file tracks a path the rules exclude.
	repository.git("add", "-f", "debug.log")
	forced := repository.digest()
	if forced == first {
		test.Error("tracking an ignored file with `git add -f` keeps the digest")
	}
	// `git rm --cached` of it untracks the path again.
	repository.git("rm", "-q", "--cached", "debug.log")
	removed := repository.digest()
	if removed == forced {
		test.Error("untracking an ignored file with `git rm --cached` keeps the digest")
	}
	if removed != first {
		test.Error("the index back where it began digests differently from where it began")
	}

	// `git status` rewrites an index whose entries' stat data moved, and decides nothing new.
	before := repository.indexBytes()
	later := time.Now().Add(2 * time.Second)
	changeTimesError := os.Chtimes(filepath.Join(repository.root, "main.go"), later, later)
	if changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	repository.git("status", "--porcelain")
	if bytes.Equal(repository.indexBytes(), before) {
		test.Fatal("`git status` left the index as it was, so this case proves nothing")
	}
	if repository.digest() != first {
		test.Error("a `git status` that rewrote the index changed the digest")
	}

	// `git add` of a source file, changed or new, tracks nothing the rules exclude: that file's own bytes are an
	// input of their own.
	writeTree(test, repository.root, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"new.go":  "package main\n",
	})
	before = repository.indexBytes()
	repository.git("add", "main.go", "new.go")
	if bytes.Equal(repository.indexBytes(), before) {
		test.Fatal("`git add` left the index as it was, so this case proves nothing")
	}
	if repository.digest() != first {
		test.Error("`git add` of files the rules do not exclude changed the digest")
	}

	// A `.gitignore` edit that newly excludes a tracked path changes what that path's tracking decides.
	writeTree(test, repository.root, map[string]string{".gitignore": "*.log\nbuild/\nnew.go\n"})
	if repository.digest() == first {
		test.Error("a `.gitignore` edit that newly excludes a tracked path keeps the digest")
	}
}

// TestTheDigestTellsAGitlinkFromAFile: an ignored path tracked as a file and the same path tracked as a gitlink digest
// differently, since a `dir/` pattern matches a gitlink as a directory and not a file, and a go.work root reads its
// submodules from gitlinks. So does a gitlink the rules do not exclude, appearing.
func TestTheDigestTellsAGitlinkFromAFile(test *testing.T) {
	test.Parallel()
	repository := newDigestRepository(test)
	commit := strings.TrimSpace(repository.git("rev-parse", "HEAD"))
	repository.git("add", "-f", "debug.log")
	asFile := repository.digest()
	repository.git("rm", "-q", "--cached", "debug.log")
	repository.git("update-index", "--add", "--cacheinfo", "160000,"+commit+",debug.log")
	if repository.digest() == asFile {
		test.Error("an ignored path tracked as a gitlink digests as the same path tracked as a file")
	}

	withoutModule := repository.digest()
	repository.git("update-index", "--add", "--cacheinfo", "160000,"+commit+",modules/library")
	if repository.digest() == withoutModule {
		test.Error("a gitlink the rules do not exclude, appearing, keeps the digest")
	}
}

// TestTheDigestCoversOnlyItsScope: a walk of a directory below the root is decided by the entries below it, so an
// ignored file tracked outside the directory keeps its digest, and one tracked inside changes it.
func TestTheDigestCoversOnlyItsScope(test *testing.T) {
	test.Parallel()
	repository := newDigestRepository(test)
	writeTree(test, repository.root, map[string]string{"service/trace.log": "trace\n"})
	scoped := func() string {
		matcher, newError := New(repository.root)
		if newError != nil {
			test.Fatal(newError)
		}
		digest, digestError := matcher.TrackedIgnoredDigest("service")
		if digestError != nil {
			test.Fatal(digestError)
		}
		return digest
	}
	first := scoped()
	repository.git("add", "-f", "debug.log")
	if scoped() != first {
		test.Error("an ignored file tracked outside the scope changed the scope's digest")
	}
	repository.git("add", "-f", "service/trace.log")
	if scoped() == first {
		test.Error("an ignored file tracked inside the scope kept the scope's digest")
	}
}
