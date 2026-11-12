package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A root reached through a symbolic link checks what its real path does (#yyv7nr2): the census walks the root, which
// a walk enters only as a directory and never as a link to one, so a linked root once held no file while go loaded
// every package, and the run checked nothing and called itself complete. Resolved where the command line is read, the
// root is its real path, and the run at the link writes the stream the run at the real path writes, findings and
// counts alike.
func TestARootReachedThroughALinkChecksWhatItsRealPathDoes(test *testing.T) {
	test.Parallel()

	real := copyFixture(test, filepath.Join("testdata", "copied-rules-withheld"))
	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	linked := filepath.Join(base, "linked")
	if linkError := os.Symlink(real, linked); linkError != nil {
		test.Fatalf("linking the root: %v", linkError)
	}
	_, atReal := runOver(test, real, exitFindings, "--no-fix")
	engine, atLink := runOverRaw(test, linked, exitFindings, "--no-cache", "--no-fix")
	if engine.described.record.FilesInPackage == 0 || engine.described.record.Root != real {
		test.Fatalf("the run at the link counted %d files under %s, want the real path's %s and its files",
			engine.described.record.FilesInPackage, engine.described.record.Root, real)
	}
	if normalized := normalize(atLink, real); normalized != atReal {
		test.Errorf("the run at the link wrote\n%s\nand at the real path\n%s", normalized, atReal)
	}
}

// A run whose census counted no Go file while its load reached packages in the workspace is never complete, and says
// why (#yyv7nr2): the two read one tree, so a census of nothing is one that never walked it, and every count it feeds
// would read 0 over work never done. With the census emptied over a module of two files, the types record names both
// files the load reached, the summary is incomplete and exits 1, and the note names the census. The control, the same
// module with its census whole, names none.
func TestACensusOfNothingBesideALoadOfPackagesIsNeverComplete(test *testing.T) {
	test.Parallel()

	emptied := func(engine *pipeline) {
		engine.described.census.Files, engine.described.census.Ours = nil, nil
		engine.described.inScope, engine.described.readableOurs = nil, nil
		engine.described.inScopeSet = map[string]bool{}
		engine.described.record.FilesInPackage, engine.described.record.FilesOurs = 0, 0
		engine.described.record.FilesInScope = 0
	}
	engine, stream, noted := runOverNoted(test, copyFixture(test, filepath.Join("testdata", "clean")), exitFindings,
		emptied, "--no-cache", "--no-fix")
	if len(engine.censusMissed) != 2 || !strings.Contains(stream, `"censusMissed":[`) ||
		!strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the types record to name the 2 files the census missed and the run incomplete, got %v:\n%s",
			engine.censusMissed, stream)
	}
	if !strings.Contains(noted, "that its census never counted") {
		test.Errorf("want a note naming the census, got\n%s", noted)
	}
	_, control := runOver(test, copyFixture(test, filepath.Join("testdata", "clean")), exitClean, "--no-fix")
	if strings.Contains(control, "censusMissed") {
		test.Errorf("the control, its census whole, names files the census missed:\n%s", control)
	}
}

// Every file the load reached is a file the census counted, or the run is incomplete (#hd5bsqh): with one file of a
// module left out of its census, and only that one, the run checks the rest, names that file and its package in the
// types record, and calls itself incomplete, where the same module counted whole is complete.
func TestAFileTheLoadReachedAndTheCensusNeverCountedLeavesTheRunIncomplete(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "clean"))
	var left string
	leftOut := func(engine *pipeline) {
		left = engine.described.census.Files[0]
		engine.described.census.Files = engine.described.census.Files[1:]
	}
	engine, stream, _ := runOverNoted(test, root, exitFindings, leftOut, "--no-cache", "--no-fix")
	if len(engine.censusMissed) != 1 || engine.censusMissed[0].File != left || engine.censusMissed[0].Package == "" {
		test.Fatalf("want %s named as the one file the census missed, with its package, got %v", left,
			engine.censusMissed)
	}
	if !strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the run incomplete:\n%s", stream)
	}
}

// A file the build constraints leave out is one the census counts too, so a file go read and left out that the census
// never counted is named as well (#hd5bsqh): over a module with a file that builds only on plan9, left out of the
// census, the run names that file, which the load holds among its ignored files and never among the ones it compiles.
// Without that, a module whose every file builds only elsewhere, reached through a link, would be missed in silence.
func TestAFileTheConstraintsLeftOutAndTheCensusNeverCountedIsNamed(test *testing.T) {
	test.Parallel()

	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	for name, contents := range map[string]string{
		"go.mod":       "module example.com/constrained\n\ngo 1.27\n",
		"here.go":      "// Package constrained is one.\npackage constrained\n\n// Here is one.\nconst Here = 1\n",
		"elsewhere.go": "//go:build plan9\n\npackage constrained\n\n// Elsewhere is two.\nconst Elsewhere = 2\n",
	} {
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	elsewhere := filepath.Join(root, "elsewhere.go")
	leftOut := func(engine *pipeline) {
		engine.described.census.Files = slices.DeleteFunc(slices.Clone(engine.described.census.Files),
			func(file string) bool { return file == elsewhere })
	}
	engine, _, _ := runOverNoted(test, root, exitFindings, leftOut, "--no-cache", "--no-fix")
	if len(engine.censusMissed) != 1 || engine.censusMissed[0].File != elsewhere {
		test.Errorf("want %s named as the one file the census missed, got %v", elsewhere, engine.censusMissed)
	}
}

// A module a go.work uses through a symbolic link is counted as go reads it (#hd5bsqh): go follows a link at a use, and
// names the module's files through it, so the census walks the module's real directory and names its files through
// the link too. Over a workspace of a real module and a linked one, each with a finding, the run finds both, names
// the linked module's file through the link, misses no file, and is complete.
func TestAModuleAGoWorkUsesThroughALinkIsCounted(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	root, elsewhere := filepath.Join(base, "root"), filepath.Join(base, "elsewhere")
	for name, contents := range map[string]string{
		"root/go.work":        "go 1.27\n\nuse (\n\t./real\n\t./linked\n)\n",
		"root/real/go.mod":    "module example.com/real\n\ngo 1.27\n",
		"root/real/real.go":   "// Package real is one.\npackage real\n\n// REAL_ONE shouts.\nconst REAL_ONE = 1\n",
		"elsewhere/go.mod":    "module example.com/linked\n\ngo 1.27\n",
		"elsewhere/linked.go": "// Package linked is two.\npackage linked\n\n// LINKED_TWO shouts.\nconst LINKED_TWO = 2\n",
	} {
		path := filepath.Join(base, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if linkError := os.Symlink(elsewhere, filepath.Join(root, "linked")); linkError != nil {
		test.Fatalf("linking the module: %v", linkError)
	}
	engine, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--no-fix")
	throughLink := filepath.Join(root, "linked", "linked.go")
	if len(engine.censusMissed) != 0 || !strings.Contains(stream, `"complete":true`) {
		test.Errorf("want no file missed and the run complete, got %v:\n%s", engine.censusMissed, stream)
	}
	for _, file := range []string{filepath.Join(root, "real", "real.go"), throughLink} {
		if !strings.Contains(stream, `"kind":"finding"`) || !strings.Contains(stream, `"file":"`+file+`"`) {
			test.Errorf("want a finding in %s:\n%s", file, stream)
		}
	}
}
