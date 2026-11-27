package git_tree_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// workTree is a tree of files a test lays in its temporary directory, for the package to hash, stat and
// compare as git would. No git runs over it: git's answers for the same tree were recorded once, under
// testdata (recorded).
type workTree struct {
	test      *testing.T
	directory string
}

// newWorkTree is an empty tree in the test's temporary directory.
func newWorkTree(test *testing.T) *workTree {
	test.Helper()
	return &workTree{test: test, directory: test.TempDir()}
}

// path is name's path on disk.
func (tree *workTree) path(name string) string {
	return filepath.Join(tree.directory, filepath.FromSlash(name))
}

// write puts content at name in the tree with exactly permissions, whatever the umask.
func (tree *workTree) write(name, content string, permissions os.FileMode) {
	tree.test.Helper()
	if mkdirError := os.MkdirAll(filepath.Dir(tree.path(name)), 0o755); mkdirError != nil {
		tree.test.Fatal(mkdirError)
	}
	if writeError := os.WriteFile(tree.path(name), []byte(content), permissions); writeError != nil {
		tree.test.Fatal(writeError)
	}
	tree.chmod(name, permissions)
}

// chmod sets name's permissions in the tree.
func (tree *workTree) chmod(name string, permissions os.FileMode) {
	tree.test.Helper()
	if chmodError := os.Chmod(tree.path(name), permissions); chmodError != nil {
		tree.test.Fatal(chmodError)
	}
}

// link makes name in the tree a symbolic link to target.
func (tree *workTree) link(name, target string) {
	tree.test.Helper()
	if mkdirError := os.MkdirAll(filepath.Dir(tree.path(name)), 0o755); mkdirError != nil {
		tree.test.Fatal(mkdirError)
	}
	if symlinkError := os.Symlink(target, tree.path(name)); symlinkError != nil {
		tree.test.Fatal(symlinkError)
	}
}

// remove deletes name, a file or an empty directory, in the tree.
func (tree *workTree) remove(name string) {
	tree.test.Helper()
	if removeError := os.Remove(tree.path(name)); removeError != nil {
		tree.test.Fatal(removeError)
	}
}

// recorded is git's answer at name under testdata, byte for byte as git printed it. testdata/README.md
// says which command printed each, over which tree, and holds the script that ran them.
func recorded(test *testing.T, name string) []byte {
	test.Helper()
	answer, readError := os.ReadFile(filepath.Join("testdata", filepath.FromSlash(name)))
	if readError != nil {
		test.Fatal(readError)
	}
	return answer
}

// recordedLine is a one-line answer at name under testdata, with git's trailing newline trimmed.
func recordedLine(test *testing.T, name string) string {
	test.Helper()
	return strings.TrimSuffix(string(recorded(test, name)), "\n")
}
