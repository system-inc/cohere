package format

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

// linkedTree is a project directory and, beside it, a shared directory the project reaches through links, both in
// one temporary directory with every symbolic link above it resolved: project/shared is a link to the shared
// directory, project/linked.go a link to a file in it, and project/inner a link to project/real, inside the project.
type linkedTree struct {
	project string
	shared  string
}

// newLinkedTree writes a linkedTree whose shared directory holds unformatted.go, which gofmt would change, long.go,
// which only the wrap would, and alone.go, which gofmt would change and project/linked.go links to; and whose
// project/real holds unformatted.go too.
func newLinkedTree(test *testing.T) linkedTree {
	test.Helper()
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	tree := linkedTree{project: filepath.Join(base, "project"), shared: filepath.Join(base, "shared")}
	for name, contents := range map[string]string{
		filepath.Join(tree.shared, "unformatted.go"):          unformattedSource,
		filepath.Join(tree.shared, "long.go"):                 callSource,
		filepath.Join(tree.shared, "alone.go"):                unformattedSource,
		filepath.Join(tree.project, "real", "unformatted.go"): unformattedSource,
	} {
		if makeError := os.MkdirAll(filepath.Dir(name), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(name, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(tree.project, "shared"):    tree.shared,
		filepath.Join(tree.project, "linked.go"): filepath.Join(tree.shared, "alone.go"),
		filepath.Join(tree.project, "inner"):     filepath.Join(tree.project, "real"),
	} {
		if linkError := os.Symlink(target, link); linkError != nil {
			test.Fatal(linkError)
		}
	}
	return tree
}

// A run that writes never writes a file whose real path is outside its root: one in a directory linked from outside,
// and one that is itself a link to a file outside, are left byte for byte as they were and named in Withheld, in the
// order given, each with its real path. Run still names each in Changed, not written, at the line a check reports,
// and never learns it as known. A directory linked from inside the root is written through, as any file inside is.
func TestAWriteOutsideTheRootIsWithheld(test *testing.T) {
	test.Parallel()

	tree := newLinkedTree(test)
	outside := filepath.Join(tree.project, "shared", "unformatted.go")
	linked := filepath.Join(tree.project, "linked.go")
	inner := filepath.Join(tree.project, "inner", "unformatted.go")
	known := DecodeKnownFiles(nil)
	result := Run([]string{outside, inner, linked}, Options{Write: true, Root: rootOf(test, tree.project),
		Known: known})
	want := Result{
		FilesConsidered: 3,
		Changed: []ChangedFile{
			{File: outside, Formatted: true, FirstDifferingLine: 5},
			{File: inner, Formatted: true, Written: true, FirstDifferingLine: 5},
			{File: linked, Formatted: true, FirstDifferingLine: 5},
		},
		Withheld: []WithheldFile{
			{File: outside, RealPath: filepath.Join(tree.shared, "unformatted.go")},
			{File: linked, RealPath: filepath.Join(tree.shared, "alone.go")},
		},
	}
	if !reflect.DeepEqual(result, want) {
		test.Fatalf("got %+v, want %+v", result, want)
	}
	requireContents(test, filepath.Join(tree.shared, "unformatted.go"), unformattedSource)
	requireContents(test, filepath.Join(tree.shared, "alone.go"), unformattedSource)
	requireContents(test, filepath.Join(tree.project, "real", "unformatted.go"), reformattedSource)
	encoded, encodeError := known.Encode()
	if encodeError != nil {
		test.Fatal(encodeError)
	}
	if again := Run([]string{outside, linked}, Options{Known: DecodeKnownFiles(encoded)}); again.Known != 0 {
		test.Fatalf("a withheld file was learned as known: %+v", again)
	}
	entries, readError := os.ReadDir(tree.shared)
	if readError != nil || len(entries) != 3 {
		test.Fatalf("want the three shared files alone in their directory, got %v (%v)", entries, readError)
	}
}

// A wrap kept for a file outside the root is withheld, neither changed nor declined, and the file keeps its bytes.
func TestAWrapOutsideTheRootIsWithheld(test *testing.T) {
	test.Parallel()

	tree := newLinkedTree(test)
	file := filepath.Join(tree.project, "shared", "long.go")
	var asked atomic.Int32
	result := RunWrap([]Source{{File: file, Contents: []byte(callSource)}},
		Options{Write: true, Root: rootOf(test, tree.project), Wrap: wrapTo(callWrapped, &asked)})
	want := Result{
		FilesConsidered: 1,
		Withheld:        []WithheldFile{{File: file, RealPath: filepath.Join(tree.shared, "long.go")}},
	}
	if asked.Load() != 1 || !reflect.DeepEqual(result, want) {
		test.Fatalf("asked the wrap %d times and got %+v, want once and %+v", asked.Load(), result, want)
	}
	requireContents(test, filepath.Join(tree.shared, "long.go"), callSource)
}

// The zero WriteRoot refuses every write, so a caller that never said where the run may write writes nothing.
func TestTheZeroWriteRootRefusesEveryWrite(test *testing.T) {
	test.Parallel()

	file := writeFile(test, unformattedSource)
	result := Run([]string{file}, Options{Write: true})
	if len(result.Withheld) != 1 || result.Withheld[0].File != file || len(result.Changed) != 1 ||
		result.Changed[0].Written {
		test.Fatalf("want the file withheld and not written, got %+v", result)
	}
	var outside *OutsideRootError
	if writeError := WriteAtomically(WriteRoot{}, file, []byte(formattedSource)); !errors.As(writeError, &outside) {
		test.Fatalf("want an OutsideRootError, got %v", writeError)
	}
	requireContents(test, file, unformattedSource)
}

// A set holding one file outside the root writes none of it, and leaves no temporary file behind: the refusal comes
// in staging, before any file is placed.
func TestASetWithAFileOutsideTheRootWritesNone(test *testing.T) {
	test.Parallel()

	tree := newLinkedTree(test)
	inside := filepath.Join(tree.project, "real", "unformatted.go")
	outside := filepath.Join(tree.project, "shared", "unformatted.go")
	partlyWritten, writeError := WriteAllAtomically(rootOf(test, tree.project),
		map[string][]byte{inside: []byte(formattedSource), outside: []byte(formattedSource)})
	var refused *OutsideRootError
	if !errors.As(writeError, &refused) || refused.RealPath != filepath.Join(tree.shared, "unformatted.go") ||
		len(partlyWritten) != 0 {
		test.Fatalf("want the set refused for the shared file, got %v, partly written %v", writeError, partlyWritten)
	}
	requireContents(test, inside, unformattedSource)
	requireContents(test, filepath.Join(tree.shared, "unformatted.go"), unformattedSource)
	for _, directory := range []string{filepath.Join(tree.project, "real"), tree.shared} {
		entries, readError := os.ReadDir(directory)
		if readError != nil {
			test.Fatal(readError)
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".go" {
				test.Errorf("the refused set left %s in %s", entry.Name(), directory)
			}
		}
	}
}

// Inside judges a file by its path relative to the root, never by a prefix of its spelling: a sibling directory whose
// name starts with the root's is outside, the root's own files and those under it are inside, as is a file reached
// through a link that stays inside, and a file that does not exist is an error.
func TestInsideJudgesByTheRealPathRelativeToTheRoot(test *testing.T) {
	test.Parallel()

	tree := newLinkedTree(test)
	sibling := tree.project + "s"
	if makeError := os.MkdirAll(sibling, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	siblingFile := filepath.Join(sibling, "file.go")
	if writeError := os.WriteFile(siblingFile, []byte(formattedSource), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	root := rootOf(test, tree.project)
	for _, testCase := range []struct {
		file     string
		realPath string
		inside   bool
	}{
		{siblingFile, siblingFile, false},
		{filepath.Join(tree.project, "inner", "unformatted.go"),
			filepath.Join(tree.project, "real", "unformatted.go"), true},
		{filepath.Join(tree.project, "real", "unformatted.go"),
			filepath.Join(tree.project, "real", "unformatted.go"), true},
		{filepath.Join(tree.project, "shared", "long.go"), filepath.Join(tree.shared, "long.go"), false},
		{filepath.Join(tree.project, "linked.go"), filepath.Join(tree.shared, "alone.go"), false},
	} {
		realPath, inside, resolveError := root.Inside(testCase.file)
		if resolveError != nil || realPath != testCase.realPath || inside != testCase.inside {
			test.Errorf("%s: got %s, inside %t (%v), want %s, inside %t", testCase.file, realPath, inside,
				resolveError, testCase.realPath, testCase.inside)
		}
	}
	if _, inside, resolveError := root.Inside(filepath.Join(tree.project, "missing.go")); resolveError == nil ||
		inside {
		test.Errorf("a missing file was resolved, inside %t", inside)
	}
	zeroRealPath, zeroInside, zeroError := (WriteRoot{}).Inside(filepath.Join(tree.project, "real", "unformatted.go"))
	if zeroError != nil || zeroInside || zeroRealPath != filepath.Join(tree.project, "real", "unformatted.go") {
		test.Errorf("the zero WriteRoot resolved the file to %s, inside %t (%v), want it resolved and outside",
			zeroRealPath, zeroInside, zeroError)
	}
}
