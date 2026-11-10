package program_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/gitignore"
	"github.com/system-inc/cohere/internal/types/program"
)

// An index keyed on what it decides replays through what git rewrites it for and nothing else (#m1daxxk): a recorded
// run whose index a `git status` rewrote replays, and one whose index newly tracks an ignored file does not. The record
// goes through the cache table and back, as a real run's does, so the key's root is anchored and spelled with the
// rest.

// trackedIgnoredTree is a repository a run read: its index, its ignore file and a source file, with another source
// file tracked beside it that the run never read, whose stat data a `git status` refreshes.
type trackedIgnoredTree struct {
	test  *testing.T
	root  string
	index string
}

func newTrackedIgnoredTree(test *testing.T) *trackedIgnoredTree {
	test.Helper()
	if _, lookPathError := exec.LookPath("git"); lookPathError != nil {
		test.Skip("no git on PATH")
	}
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	tree := &trackedIgnoredTree{test: test, root: root, index: filepath.Join(root, ".git", "index")}
	tree.git("init", "-q", "-b", "main")
	for name, contents := range map[string]string{
		".gitignore": "*.log\n",
		"main.go":    "package main\n",
		"other.go":   "package main\n",
		"debug.log":  "a log\n",
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	tree.git("add", ".gitignore", "main.go", "other.go")
	tree.git("commit", "-q", "-m", "first")
	return tree
}

// git runs git in the tree with no configuration but the repository's own.
func (tree *trackedIgnoredTree) git(arguments ...string) {
	tree.test.Helper()
	command := exec.Command("git", append([]string{"-c", "user.name=cohere", "-c", "user.email=cohere@localhost",
		"-c", "core.excludesFile=/dev/null"}, arguments...)...)
	command.Dir = tree.root
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "HOME="+tree.root)
	if output, runError := command.CombinedOutput(); runError != nil {
		tree.test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), runError, output)
	}
}

// recorded is a run over the tree recorded and read back through the cache table, its index keyed on what it decides
// when keyed is set and on its bytes otherwise.
func (tree *trackedIgnoredTree) recorded(keyed bool) *program.RunCache {
	tree.test.Helper()
	matcher, newError := gitignore.New(tree.root)
	if newError != nil {
		tree.test.Fatal(newError)
	}
	if matcher.IndexFile() != tree.index {
		tree.test.Fatalf("the matcher reads %s, not the index this test changes", matcher.IndexFile())
	}
	digest, digestError := matcher.TrackedIgnoredDigest("")
	if digestError != nil {
		tree.test.Fatal(digestError)
	}
	files := []string{tree.index, filepath.Join(tree.root, ".gitignore"), filepath.Join(tree.root, "main.go")}
	var keys map[string]program.TrackedIgnoredKey
	if keyed {
		keys = map[string]program.TrackedIgnoredKey{tree.index: {Root: tree.root, Digest: digest}}
	}
	// Nil roots record every holding directory, as a TypeScript run's record does.
	cache, recordError := program.RecordRunCacheWithin(nil, "key", files, nil, nil, nil, []byte("output\n"), 0,
		time.Time{}, keys)
	if recordError != nil {
		tree.test.Fatal(recordError)
	}
	directory := tree.test.TempDir()
	identity := program.CacheTableIdentity{SelfCommit: "tracked-ignored-test"}
	sections := program.CacheTableSections{Runs: []string{"run"}}
	anchor := program.NewPathAnchor(tree.root)
	table := program.NewCacheTable()
	table.Runs["run"] = cache
	if writeError := program.WriteCacheTable(directory, table, identity, sections, anchor); writeError != nil {
		tree.test.Fatal(writeError)
	}
	read, readError := program.ReadCacheTable(directory, identity, sections, anchor)
	if readError != nil {
		tree.test.Fatal(readError)
	}
	return read.Runs["run"]
}

// rewriteIndexWithStatus has `git status` rewrite the index by refreshing the stat data of a tracked file the run
// never read, and fails the test if the index's bytes did not move, since the case would then prove nothing.
func (tree *trackedIgnoredTree) rewriteIndexWithStatus() {
	tree.test.Helper()
	before, readError := os.ReadFile(tree.index)
	if readError != nil {
		tree.test.Fatal(readError)
	}
	later := time.Now().Add(2 * time.Second)
	if changeTimesError := os.Chtimes(filepath.Join(tree.root, "other.go"), later, later); changeTimesError != nil {
		tree.test.Fatal(changeTimesError)
	}
	tree.git("status", "--porcelain")
	after, readError := os.ReadFile(tree.index)
	if readError != nil {
		tree.test.Fatal(readError)
	}
	if bytes.Equal(before, after) {
		tree.test.Fatal("`git status` left the index as it was, so this case proves nothing")
	}
}

func TestAnIndexKeyedOnWhatItDecidesReplaysThroughAGitStatus(test *testing.T) {
	test.Parallel()
	tree := newTrackedIgnoredTree(test)
	keyed, unkeyed := tree.recorded(true), tree.recorded(false)
	if checkError := keyed.Check("key"); checkError != nil {
		test.Fatalf("the run misses before anything moved: %v", checkError)
	}

	tree.rewriteIndexWithStatus()
	if checkError := keyed.Check("key"); checkError != nil {
		test.Errorf("a `git status` that rewrote the index keeps a keyed run from replaying: %v", checkError)
	}
	// The same record keyed on the index's bytes misses, so the key is what replays it.
	if checkError := unkeyed.Check("key"); !errors.Is(checkError, program.ErrRunCacheMiss) {
		test.Errorf("a run keyed on the index's bytes replays after `git status` rewrote them: %v", checkError)
	}

	tree.git("add", "-f", "debug.log")
	checkError := keyed.Check("key")
	if !errors.Is(checkError, program.ErrRunCacheMiss) || !strings.Contains(checkError.Error(), "are tracked") {
		test.Errorf("a run replays over an index that newly tracks an ignored file: %v", checkError)
	}
}

// TestAKeyedIndexThatCannotBeReadAgainMisses: an index whose bytes moved to ones the reader cannot read whole, a
// checksum that no longer matches, is a miss, never a digest of less than the index says.
func TestAKeyedIndexThatCannotBeReadAgainMisses(test *testing.T) {
	test.Parallel()
	tree := newTrackedIgnoredTree(test)
	keyed := tree.recorded(true)
	contents, readError := os.ReadFile(tree.index)
	if readError != nil {
		test.Fatal(readError)
	}
	contents[len(contents)-1] ^= 0xff
	if writeError := os.WriteFile(tree.index, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	checkError := keyed.Check("key")
	if !errors.Is(checkError, program.ErrRunCacheMiss) || !errors.Is(checkError, gitignore.ErrNoTrackedIgnoredDigest) {
		test.Errorf("a keyed run replays, or misses for another reason, over an index the reader refuses: %v",
			checkError)
	}
}

// TestAWorktreesIndexNamedThroughALinkReplays: a worktree made under a directory reached through a link, as a temp
// directory is on macOS (/var is /private/var), has its index named by the path the caller holds, while git writes the
// worktree's pointer to its git directory by the real path, so the matcher reads the same file under another spelling.
// The index is keyed all the same, and replays after a `git status` rewrote it (#ny1mv0y). Skipped where the temp
// directory reaches no link, since then the two spellings are one.
func TestAWorktreesIndexNamedThroughALinkReplays(test *testing.T) {
	test.Parallel()
	linked := test.TempDir()
	resolved, resolveError := filepath.EvalSymlinks(linked)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	if resolved == linked {
		test.Skip("the temp directory is reached through no link here, so its spellings cannot differ")
	}
	main := &trackedIgnoredTree{test: test, root: filepath.Join(linked, "main")}
	if makeError := os.MkdirAll(main.root, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	main.git("init", "-q", "-b", "main")
	for name, contents := range map[string]string{".gitignore": "*.log\n", "main.go": "package main\n",
		"other.go": "package main\n"} {
		if writeError := os.WriteFile(filepath.Join(main.root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	main.git("add", "-A")
	main.git("commit", "-q", "-m", "first")
	worktree := filepath.Join(linked, "worktree")
	main.git("worktree", "add", "-q", worktree)

	// The index as the caller spells it, through the link, and the file git's pointer names, by its real path.
	tree := &trackedIgnoredTree{test: test, root: worktree,
		index: filepath.Join(linked, "main", ".git", "worktrees", "worktree", "index")}
	matcher, newError := gitignore.New(worktree)
	if newError != nil {
		test.Fatal(newError)
	}
	realIndex, resolveIndexError := filepath.EvalSymlinks(tree.index)
	if resolveIndexError != nil {
		test.Fatal(resolveIndexError)
	}
	if matcher.IndexFile() == tree.index || matcher.IndexFile() != realIndex {
		test.Fatalf("the matcher reads %s, which should be the index %s spelled another way", matcher.IndexFile(),
			tree.index)
	}
	digest, digestError := matcher.TrackedIgnoredDigest("")
	if digestError != nil {
		test.Fatal(digestError)
	}
	files := []string{tree.index, filepath.Join(worktree, "main.go")}
	cache, recordError := program.RecordRunCacheWithin(nil, "key", files, nil, nil, nil, []byte("output\n"), 0,
		time.Time{},
		map[string]program.TrackedIgnoredKey{tree.index: {Root: worktree, Digest: digest}})
	if recordError != nil {
		test.Fatal(recordError)
	}
	tree.rewriteIndexWithStatus()
	if checkError := cache.Check("key"); checkError != nil {
		test.Errorf("a worktree's index named through a link kept the run from replaying after `git status`: %v",
			checkError)
	}
}
