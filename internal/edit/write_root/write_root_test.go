package write_root

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// containmentTree writes, in the test's own temporary directory with every symbolic link above it resolved, a root
// and beside it a shared directory and a sibling whose name starts with the root's:
//
//	root/file.go                a file of the root's own
//	root/real/file.go           a file under the root
//	root/inner -> root/real     a directory link that stays inside
//	root/inside.go -> real/...  a file link that stays inside
//	root/shared -> shared       a directory link to outside
//	root/linked.go -> shared/.. a file link to outside
//	roots/file.go               the sibling, outside however its spelling starts
//
// It returns the root and the directory holding the tree.
func containmentTree(test *testing.T) (string, string) {
	test.Helper()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	root := filepath.Join(base, "root")
	for _, file := range []string{
		filepath.Join(root, "file.go"),
		filepath.Join(root, "real", "file.go"),
		filepath.Join(base, "shared", "file.go"),
		filepath.Join(base, "roots", "file.go"),
	} {
		if makeError := os.MkdirAll(filepath.Dir(file), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(file, []byte("package fixture\n"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(root, "inner"):     filepath.Join(root, "real"),
		filepath.Join(root, "inside.go"): filepath.Join(root, "real", "file.go"),
		filepath.Join(root, "shared"):    filepath.Join(base, "shared"),
		filepath.Join(root, "linked.go"): filepath.Join(base, "shared", "file.go"),
	} {
		if linkError := os.Symlink(target, link); linkError != nil {
			test.Fatal(linkError)
		}
	}
	return root, base
}

// Inside judges a file by its real path relative to the root: the root's own files, a file under it, and a file
// reached through a directory or file link that stays inside are inside; a file reached through a directory link or a
// file link to somewhere outside is outside, named by where it really is; and a sibling whose name starts with the
// root's is outside, since `/a/bc` is not under `/a/b`. Holds answers the same of a path already resolved.
func TestInsideJudgesByTheRealPathRelativeToTheRoot(test *testing.T) {
	test.Parallel()

	root, base := containmentTree(test)
	writeRoot, newError := New(root)
	if newError != nil {
		test.Fatal(newError)
	}
	for _, testCase := range []struct {
		name     string
		file     string
		realPath string
		inside   bool
	}{
		{"the root's own file", filepath.Join(root, "file.go"), filepath.Join(root, "file.go"), true},
		{"a file under the root", filepath.Join(root, "real", "file.go"), filepath.Join(root, "real", "file.go"), true},
		{"a directory link inside", filepath.Join(root, "inner", "file.go"), filepath.Join(root, "real", "file.go"),
			true},
		{"a file link inside", filepath.Join(root, "inside.go"), filepath.Join(root, "real", "file.go"), true},
		{"a directory link outside", filepath.Join(root, "shared", "file.go"), filepath.Join(base, "shared", "file.go"),
			false},
		{"a file link outside", filepath.Join(root, "linked.go"), filepath.Join(base, "shared", "file.go"), false},
		{"a sibling sharing the root's prefix", filepath.Join(base, "roots", "file.go"),
			filepath.Join(base, "roots", "file.go"), false},
	} {
		realPath, inside, resolveError := writeRoot.Inside(testCase.file)
		if resolveError != nil || realPath != testCase.realPath || inside != testCase.inside {
			test.Errorf("%s: got %s, inside %t (%v), want %s, inside %t", testCase.name, realPath, inside,
				resolveError, testCase.realPath, testCase.inside)
		}
		if writeRoot.Holds(testCase.realPath) != testCase.inside {
			test.Errorf("%s: Holds(%s) is %t, want %t", testCase.name, testCase.realPath, !testCase.inside,
				testCase.inside)
		}
	}
	if !writeRoot.Holds(root) {
		test.Error("the root does not hold itself")
	}
	if writeRoot.Holds(base) {
		test.Error("the root holds the directory above it")
	}
}

// A root given through a link is judged by where it really is, so a file under the link is inside and a file
// reached through it is named by its real path.
func TestARootGivenThroughALinkIsResolved(test *testing.T) {
	test.Parallel()

	root, base := containmentTree(test)
	alias := filepath.Join(base, "alias")
	if linkError := os.Symlink(root, alias); linkError != nil {
		test.Fatal(linkError)
	}
	writeRoot, newError := New(alias)
	if newError != nil {
		test.Fatal(newError)
	}
	realPath, inside, resolveError := writeRoot.Inside(filepath.Join(alias, "real", "file.go"))
	if resolveError != nil || !inside || realPath != filepath.Join(root, "real", "file.go") {
		test.Errorf("got %s, inside %t (%v), want %s inside", realPath, inside, resolveError,
			filepath.Join(root, "real", "file.go"))
	}
}

// The zero WriteRoot holds nothing, so a caller that never said where the run may write writes nothing; it still
// resolves a file, so a refusal can say where the file really is.
func TestTheZeroWriteRootRefusesEveryFile(test *testing.T) {
	test.Parallel()

	root, _ := containmentTree(test)
	file := filepath.Join(root, "file.go")
	realPath, inside, resolveError := WriteRoot{}.Inside(file)
	if resolveError != nil || inside || realPath != file {
		test.Errorf("got %s, inside %t (%v), want %s resolved and outside", realPath, inside, resolveError, file)
	}
	if (WriteRoot{}).Holds(root) {
		test.Error("the zero WriteRoot holds the root")
	}
}

// A file that cannot be resolved is an error and never inside, and a root that does not exist has no WriteRoot.
func TestWhatCannotBeResolvedIsNeverInside(test *testing.T) {
	test.Parallel()

	root, _ := containmentTree(test)
	writeRoot, newError := New(root)
	if newError != nil {
		test.Fatal(newError)
	}
	if _, inside, resolveError := writeRoot.Inside(filepath.Join(root, "missing.go")); resolveError == nil || inside {
		test.Errorf("a missing file was resolved, inside %t", inside)
	}
	if _, missingError := New(filepath.Join(root, "missing")); missingError == nil {
		test.Error("a root that does not exist was resolved")
	}
}

// Copies of a WriteRoot share the directories they resolved, asked from many goroutines at once, and every answer is
// the one a single caller gets.
func TestCopiesAskedAtOnceAgree(test *testing.T) {
	test.Parallel()

	root, _ := containmentTree(test)
	writeRoot, newError := New(root)
	if newError != nil {
		test.Fatal(newError)
	}
	files := []string{filepath.Join(root, "inner", "file.go"), filepath.Join(root, "shared", "file.go")}
	var group sync.WaitGroup
	failures := make(chan error, 64)
	for index := range 64 {
		group.Go(func() {
			copied := writeRoot
			file := files[index%len(files)]
			_, inside, resolveError := copied.Inside(file)
			if resolveError != nil || inside != strings.Contains(file, "inner") {
				failures <- errors.Join(resolveError, errors.New(file+" judged otherwise than alone"))
			}
		})
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		test.Error(failure)
	}
}

// The refusal names the file as the run reached it and where it really is.
func TestTheRefusalNamesWhereTheFileReallyIs(test *testing.T) {
	test.Parallel()

	refusal := (&OutsideRootError{File: "/root/shared/file.go", RealPath: "/shared/file.go"}).Error()
	want := "/root/shared/file.go is reached through a link to /shared/file.go, outside the run's root"
	if refusal != want {
		test.Errorf("the refusal reads %q, want %q", refusal, want)
	}
}
