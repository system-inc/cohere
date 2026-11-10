package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/system-inc/cohere/internal/write_root"
)

// rootOf is the WriteRoot of the directory fileName sits in, the root every test here that is not about roots writes
// under, so a test of the engine's fixing reads as it always did.
func rootOf(test testing.TB, fileName string) write_root.WriteRoot {
	test.Helper()
	root, rootError := write_root.New(filepath.Dir(fileName))
	if rootError != nil {
		test.Fatalf("resolving the root of %s: %v", fileName, rootError)
	}
	return root
}

// linkTree is a run's root beside a directory outside it, the shapes codebase's probe on #hwz114g found the
// edit engine writing through (#td3zta2). Every file holds `let unchanged = 1;\n`, which renamingProposal
// rewrites, so a file the engine wrote reads differently and one it left reads exactly so.
type linkTree struct {
	root    string
	outside string
}

const linkedSource = "let unchanged = 1;\n"

// newLinkTree makes root and outside as siblings under one temporary directory, each holding real.ts.
func newLinkTree(test *testing.T) linkTree {
	test.Helper()
	base := test.TempDir()
	tree := linkTree{root: filepath.Join(base, "root"), outside: filepath.Join(base, "outside")}
	for _, directory := range []string{tree.root, tree.outside, filepath.Join(tree.root, "real")} {
		if makeError := os.MkdirAll(directory, 0o755); makeError != nil {
			test.Fatal(makeError)
		}
	}
	for _, file := range []string{
		filepath.Join(tree.outside, "Outside.ts"),
		filepath.Join(tree.root, "real", "Real.ts"),
	} {
		if writeError := os.WriteFile(file, []byte(linkedSource), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return tree
}

// link makes name a symbolic link to target, as `ln -s target name` does.
func link(test *testing.T, target string, name string) {
	test.Helper()
	if linkError := os.Symlink(target, name); linkError != nil {
		test.Fatal(linkError)
	}
}

// writeRoot is the tree's root as a WriteRoot.
func (tree linkTree) writeRoot(test *testing.T) write_root.WriteRoot {
	test.Helper()
	root, rootError := write_root.New(tree.root)
	if rootError != nil {
		test.Fatal(rootError)
	}
	return root
}

// renamingProposal proposes `let unchanged` become `const changed` while the text still says `let unchanged`.
func renamingProposal() Propose {
	return proposeWhileContains("let unchanged", "const changed", "renamer")
}

// readText is file's contents, through any link.
func readText(test *testing.T, file string) string {
	test.Helper()
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	return string(contents)
}

// assertIsLink fails unless name is still a symbolic link to target.
func assertIsLink(test *testing.T, name string, target string) {
	test.Helper()
	information, statError := os.Lstat(name)
	if statError != nil {
		test.Fatal(statError)
	}
	if information.Mode()&os.ModeSymlink == 0 {
		test.Fatalf("%s was a link and is now a plain file: the write replaced the link rather than writing its target",
			name)
	}
	pointsAt, readError := os.Readlink(name)
	if readError != nil {
		test.Fatal(readError)
	}
	if pointsAt != target {
		test.Fatalf("%s points at %s, want %s", name, pointsAt, target)
	}
}

// realPathOf is file's real path, every link resolved, as the engine reports it.
func realPathOf(test *testing.T, file string) string {
	test.Helper()
	resolved, resolveError := filepath.EvalSymlinks(file)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	return resolved
}

// The first shape the probe found: a directory in the root linked to one outside it. The fix ran, a file
// behind the link changed, and the run exited 0 naming no gap. The file is fixed in memory as any other and
// never written, and the result names where it really is. Fails without the outside check in WriteAtomically,
// and without resolving the file's directory in write_root.Inside.
func TestAFileBehindADirectoryLinkOutsideTheRootIsNotWritten(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	link(test, tree.outside, filepath.Join(tree.root, "linked"))
	reached := filepath.Join(tree.root, "linked", "Outside.ts")

	result, fixError := FixFile(tree.writeRoot(test), reached, renamingProposal(), DefaultMaxPasses)
	if fixError != nil {
		test.Fatalf("a file outside the root is a gap, not a failure: %v", fixError)
	}
	if got := readText(test, filepath.Join(tree.outside, "Outside.ts")); got != linkedSource {
		test.Fatalf("the file outside the root was rewritten through the link:\n%s", got)
	}
	if result.Changed || len(result.Applied) == 0 {
		test.Fatalf("want the fix computed and not written (Changed false, Applied kept), got Changed %v, Applied %d",
			result.Changed, len(result.Applied))
	}
	if want := realPathOf(test, reached); result.NotWritten != want {
		test.Fatalf("NotWritten is %q, want the real path %q", result.NotWritten, want)
	}
}

// The second shape: a file in the root that is itself a link, to a file inside the root. A rename over the
// link replaced it with a plain file holding the fixed text, so the link was gone and its target never
// changed. The target takes the fix, and the link stays a link. Fails when the rename is made over the name
// as given rather than the real path.
func TestAFileLinkInsideTheRootIsWrittenThroughAndStaysALink(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	target := filepath.Join("real", "Real.ts")
	linked := filepath.Join(tree.root, "Linked.ts")
	link(test, target, linked)

	result, fixError := FixFile(tree.writeRoot(test), linked, renamingProposal(), DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	if !result.Changed || result.NotWritten != "" {
		test.Fatalf("want the file written through its link, got Changed %v, NotWritten %q", result.Changed,
			result.NotWritten)
	}
	assertIsLink(test, linked, target)
	if got := readText(test, filepath.Join(tree.root, "real", "Real.ts")); got != "const changed = 1;\n" {
		test.Fatalf("the link's target did not take the fix:\n%s", got)
	}
}

// The third shape: a file in the root that is a link to a file outside it. Neither the link nor its target
// is written: the link stays a link, its target holds what it held, and the result names the target.
func TestAFileLinkOutsideTheRootIsNotWrittenAndStaysALink(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	target := filepath.Join(tree.outside, "Outside.ts")
	linked := filepath.Join(tree.root, "LinkedFile.ts")
	link(test, target, linked)

	result, fixError := FixFile(tree.writeRoot(test), linked, renamingProposal(), DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	assertIsLink(test, linked, target)
	if got := readText(test, target); got != linkedSource {
		test.Fatalf("the file outside the root was rewritten through the link:\n%s", got)
	}
	if want := realPathOf(test, target); result.Changed || result.NotWritten != want {
		test.Fatalf("want not written, naming %q, got Changed %v, NotWritten %q", want, result.Changed,
			result.NotWritten)
	}
}

// A directory link that stays inside the root is the root's own: a file behind it is written, the link stays
// a link, and the file it reaches takes the fix. The guard refuses by where a file really is, never by
// whether a link was on the way.
func TestAFileBehindADirectoryLinkInsideTheRootIsWritten(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	linkedDirectory := filepath.Join(tree.root, "alias")
	link(test, "real", linkedDirectory)

	result, fixError := FixFile(tree.writeRoot(test), filepath.Join(linkedDirectory, "Real.ts"), renamingProposal(),
		DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	if !result.Changed || result.NotWritten != "" {
		test.Fatalf("want written, got Changed %v, NotWritten %q", result.Changed, result.NotWritten)
	}
	assertIsLink(test, linkedDirectory, "real")
	if got := readText(test, filepath.Join(tree.root, "real", "Real.ts")); got != "const changed = 1;\n" {
		test.Fatalf("the file behind the inside link did not take the fix:\n%s", got)
	}
}

// The plain case, and a root that is itself reached through a link, as macOS's /tmp is: a plain file in it is
// written as always. A root left unresolved would hold none of its own files, and this fails then.
func TestAPlainFileIsWrittenUnderARootReachedThroughALink(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	rootLink := filepath.Join(filepath.Dir(tree.root), "root-link")
	link(test, tree.root, rootLink)
	root, rootError := write_root.New(rootLink)
	if rootError != nil {
		test.Fatal(rootError)
	}
	plain := filepath.Join(rootLink, "real", "Real.ts")

	result, fixError := FixFile(root, plain, renamingProposal(), DefaultMaxPasses)
	if fixError != nil {
		test.Fatal(fixError)
	}
	if !result.Changed || result.NotWritten != "" {
		test.Fatalf("want a plain file written, got Changed %v, NotWritten %q", result.Changed, result.NotWritten)
	}
	if got := readText(test, filepath.Join(tree.root, "real", "Real.ts")); got != "const changed = 1;\n" {
		test.Fatalf("the plain file did not take the fix:\n%s", got)
	}
}

// WriteAtomically itself refuses a file outside the root with an *OutsideRootError naming both paths, and
// creates nothing beside it, so no caller can write past the root by going around the engine. The zero
// WriteRoot refuses every file, so a caller that never said where it may write writes nothing.
func TestWriteAtomicallyRefusesOutsideTheRootAndCreatesNothing(test *testing.T) {
	test.Parallel()
	tree := newLinkTree(test)
	link(test, tree.outside, filepath.Join(tree.root, "linked"))
	reached := filepath.Join(tree.root, "linked", "Outside.ts")

	writeError := WriteAtomically(tree.writeRoot(test), reached, "const changed = 1;\n")
	var outside *write_root.OutsideRootError
	if !errors.As(writeError, &outside) {
		test.Fatalf("want an *OutsideRootError, got %v", writeError)
	}
	if outside.File != reached || outside.RealPath != realPathOf(test, reached) {
		test.Fatalf("the refusal names %q at %q, want %q at %q", outside.File, outside.RealPath, reached,
			realPathOf(test, reached))
	}
	entries, readError := os.ReadDir(tree.outside)
	if readError != nil {
		test.Fatal(readError)
	}
	if len(entries) != 1 {
		test.Fatalf("the refusal left something beside the outside file: %v", entries)
	}

	plain := filepath.Join(tree.root, "real", "Real.ts")
	if zeroError := WriteAtomically(write_root.WriteRoot{}, plain, "const changed = 1;\n"); !errors.As(zeroError,
		&outside) {
		test.Fatalf("the zero WriteRoot wrote %s: %v", plain, zeroError)
	}
	if got := readText(test, plain); got != linkedSource {
		test.Fatalf("the zero WriteRoot wrote %s:\n%s", plain, got)
	}
}

// The summary names each file not written with its real path, sorted, and its line says so, so a fix run that
// withheld a write never reads as every rewrite landing.
func TestTheSummaryNamesEachFileNotWritten(test *testing.T) {
	test.Parallel()
	summary := Summarize([]FileResult{
		{FileName: "/root/b.ts", NotWritten: "/elsewhere/b.ts", Converged: true},
		{FileName: "/root/a.ts", NotWritten: "/elsewhere/a.ts", Converged: true},
		{FileName: "/root/c.ts", Converged: true},
	})
	want := []NotWrittenFile{
		{FileName: "/root/a.ts", RealPath: "/elsewhere/a.ts"},
		{FileName: "/root/b.ts", RealPath: "/elsewhere/b.ts"},
	}
	if len(summary.NotWritten) != len(want) || summary.NotWritten[0] != want[0] || summary.NotWritten[1] != want[1] {
		test.Fatalf("NotWritten is %v, want %v", summary.NotWritten, want)
	}
	if line := summary.String(); line != "fix: 0 of 3 files rewritten, 0 fixes applied, 0 refused, "+
		"2 not written (reached through a link outside the root)" {
		test.Fatalf("the summary line reads %q", line)
	}
}
