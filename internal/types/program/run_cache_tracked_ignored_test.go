package program_test

import (
	"bytes"
	"errors"
	"os"
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
// rest. Each index is one real git wrote, recorded under testdata with the commands that made it, and laid into a
// temporary tree, so no test here starts git: the matcher reads only an index's entries, never its stat data
// against the files on disk, so laid bytes read as git's own.

// trackedIgnoredIndexes holds the recorded indexes; its README.md names the git and the commands that wrote them.
const trackedIgnoredIndexes = "testdata/tracked-ignored-index"

// trackedIgnoredFiles is the tree the indexes were recorded over: an ignore file, the source file a run reads,
// another tracked beside it that the run never reads, whose stat data a `git status` refreshed, and an ignored log.
var trackedIgnoredFiles = map[string]string{
	".gitignore": "*.log\n",
	"main.go":    "package main\n",
	"other.go":   "package main\n",
	"debug.log":  "a log\n",
}

// trackedIgnoredTree is a repository a run read: its index, its ignore file and a source file, with another source
// file tracked beside it that the run never read.
type trackedIgnoredTree struct {
	test  *testing.T
	root  string
	index string
}

// newTrackedIgnoredTree is the recorded tree laid in a temporary directory, its index as the first commit left it.
func newTrackedIgnoredTree(test *testing.T) *trackedIgnoredTree {
	test.Helper()
	root, resolveSymlinksError := filepath.EvalSymlinks(test.TempDir())
	if resolveSymlinksError != nil {
		test.Fatal(resolveSymlinksError)
	}
	tree := &trackedIgnoredTree{test: test, root: root, index: filepath.Join(root, ".git", "index")}
	writeTrackedIgnoredFiles(test, root)
	tree.lay("committed.index")
	return tree
}

// writeTrackedIgnoredFiles writes the recorded tree's files into directory.
func writeTrackedIgnoredFiles(test *testing.T, directory string) {
	test.Helper()
	for name, contents := range trackedIgnoredFiles {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// lay puts the recorded index named name in place of the tree's index.
func (tree *trackedIgnoredTree) lay(name string) {
	tree.test.Helper()
	contents, readError := os.ReadFile(filepath.Join(trackedIgnoredIndexes, name))
	if readError != nil {
		tree.test.Fatal(readError)
	}
	tree.layBytes(contents)
}

// layBytes puts contents in place of the tree's index the way git does, written to index.lock and renamed over it.
// The rename gives the index a new inode, so a run keyed on its stat sees it moved even where the clock is too coarse
// to move its modification time, and two indexes of one size cannot read as one file.
func (tree *trackedIgnoredTree) layBytes(contents []byte) {
	tree.test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(tree.index), 0o755); makeError != nil {
		tree.test.Fatal(makeError)
	}
	lock := tree.index + ".lock"
	if writeError := os.WriteFile(lock, contents, 0o644); writeError != nil {
		tree.test.Fatal(writeError)
	}
	if renameError := os.Rename(lock, tree.index); renameError != nil {
		tree.test.Fatal(renameError)
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

// rewriteIndexWithStatus lays the index a `git status` wrote when it refreshed the stat data of a tracked file the
// run never read, and fails the test if the index's bytes did not move, since the case would then prove nothing.
func (tree *trackedIgnoredTree) rewriteIndexWithStatus() {
	tree.test.Helper()
	before, readError := os.ReadFile(tree.index)
	if readError != nil {
		tree.test.Fatal(readError)
	}
	tree.lay("status-rewritten.index")
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

	tree.lay("tracks-ignored.index")
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
	tree.layBytes(contents)
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
	// A worktree as `git worktree add` leaves one, as much of it as the matcher reads: its files, and a `.git` file
	// whose pointer names its git directory in the main checkout by the real path, as git writes it, holding the
	// recorded index. The git directory's commondir is left out, since without it the matcher reads the same SHA-1
	// index, and the main checkout's own files decide nothing here.
	worktree := filepath.Join(linked, "worktree")
	if makeError := os.MkdirAll(worktree, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	writeTrackedIgnoredFiles(test, worktree)
	gitDirectory := filepath.Join(resolved, "main", ".git", "worktrees", "worktree")
	pointer := []byte("gitdir: " + gitDirectory + "\n")
	if writeError := os.WriteFile(filepath.Join(worktree, ".git"), pointer, 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	// The index as the caller spells it, through the link, and the file git's pointer names, by its real path.
	tree := &trackedIgnoredTree{test: test, root: worktree,
		index: filepath.Join(linked, "main", ".git", "worktrees", "worktree", "index")}
	tree.lay("committed.index")
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
