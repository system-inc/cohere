package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// A `--dev` cohere reports the commit its tree was at and whether anything in it was uncommitted, and it
// sits at one stable path beside the hash it was built from. Go stamped those facts itself, read apart from
// that hash, so a binary built in one state went on being served to a tree in another (#50wtb2s): built
// dirty, it said so after the work was committed, and built clean, it said clean beside a dirty tree, and a
// clean commit sends the Swift engine to that commit rather than the working tree. Each step here changes
// only the tree's state, never a Go source, and reads what the served binary says about itself.
func TestADevelopmentBinaryReportsTheStateOfTheTreeItIsServedTo(test *testing.T) {
	test.Parallel()
	fixture := newCommittedFixture(test)
	version := func() (string, bool) {
		test.Helper()
		binaryPath, built, resolveWorkingTreeError := ResolveWorkingTree(fixture.paths, "./command/cohere")
		if resolveWorkingTreeError != nil {
			test.Fatalf("resolving the working tree's binary: %v", resolveWorkingTreeError)
		}
		return run(test, binaryPath, "--version"), built
	}
	const modified = "uncommitted changes"

	clean, built := version()
	if !built || strings.Contains(clean, modified) ||
		!strings.Contains(clean, "commit:         "+release.ShortCommit(fixture.head(test))) {
		test.Fatalf("a build from a clean tree (built=%v) says:\n%s", built, clean)
	}

	notes := filepath.Join(fixture.superproject, "notes.txt")
	writeFile(test, notes, "a file nobody committed, outside the Go sources\n")
	dirty, built := version()
	if !strings.Contains(dirty, modified) {
		test.Fatalf("the dirty tree was served a binary that claims a clean build (built=%v):\n%s", built, dirty)
	}

	if removeError := os.Remove(notes); removeError != nil {
		test.Fatal(removeError)
	}
	cleanAgain, built := version()
	if strings.Contains(cleanAgain, modified) {
		test.Fatalf("the clean tree was served the dirty tree's binary (built=%v):\n%s", built, cleanAgain)
	}

	// A commit that touches no Go source still moves the commit the binary names.
	writeFile(test, filepath.Join(fixture.superproject, "README.md"), "a commit outside the Go sources\n")
	fixture.commit(test, "README.md")
	committed, built := version()
	if strings.Contains(committed, modified) ||
		!strings.Contains(committed, "commit:         "+release.ShortCommit(fixture.head(test))) {
		test.Fatalf("after a commit the binary (built=%v) does not name it:\n%s", built, committed)
	}
}

// The vendored compiler is compiled into cohere, so a compiler checkout that differs from the commit's pin
// makes the tree modified, though git status of cohere alone leaves submodules out.
func TestACompilerCheckoutOffItsPinMakesTheDevelopmentTreeModified(test *testing.T) {
	test.Parallel()
	fixture := newCommittedFixture(test)
	compiler := filepath.Join(fixture.superproject, "TypeScript")
	pin, readHeadError := readHead(compiler)
	if readHeadError != nil {
		test.Fatal(readHeadError)
	}
	if _, isModified := developmentTreeState(fixture.superproject, pin); isModified {
		test.Fatal("a clean tree with its compiler at the pin reads as modified")
	}

	writeFile(test, filepath.Join(compiler, "tsc", "checker", "checker.go"),
		"package checker\n\nfunc Value() string { return \"uncommitted\" }\n")
	if _, isModified := developmentTreeState(fixture.superproject, pin); !isModified {
		test.Error("an uncommitted edit inside the compiler left the tree reading clean")
	}
	gitCommand(test, compiler, "commit", "-q", "-am", "moved off the pin")
	moved, readHeadError := readHead(compiler)
	if readHeadError != nil {
		test.Fatal(readHeadError)
	}
	if _, isModified := developmentTreeState(fixture.superproject, moved); !isModified {
		test.Error("a compiler checkout moved off the commit's pin left the tree reading clean")
	}
}
