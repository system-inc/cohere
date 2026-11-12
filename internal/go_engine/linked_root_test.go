package go_engine

import (
	"os"
	"path/filepath"
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
// would read 0 over work never done. With the census emptied over a module of two packages, the types record names the
// packages the load reached, the summary is incomplete and exits 1, and the note names the census. The control, the
// same module with its census whole, writes no such count.
func TestACensusOfNothingBesideALoadOfPackagesIsNeverComplete(test *testing.T) {
	test.Parallel()

	emptied := func(engine *pipeline) {
		engine.described.census.Files, engine.described.census.Ours = nil, nil
		engine.described.inScope, engine.described.readableOurs = nil, nil
		engine.described.inScopeSet = map[string]bool{}
		engine.described.record.FilesInPackage, engine.described.record.FilesOurs = 0, 0
		engine.described.record.FilesInScope = 0
	}
	_, stream, noted := runOverNoted(test, copyFixture(test, filepath.Join("testdata", "clean")), exitFindings,
		emptied, "--no-cache", "--no-fix")
	if !strings.Contains(stream, `"censusMissedPackages":`) || !strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the types record to name the packages the census missed and the run incomplete:\n%s", stream)
	}
	if !strings.Contains(noted, "its census counted no Go file there, so the run checked none of them") {
		test.Errorf("want a note naming the census, got\n%s", noted)
	}
	_, control := runOver(test, copyFixture(test, filepath.Join("testdata", "clean")), exitClean, "--no-fix")
	if strings.Contains(control, "censusMissedPackages") {
		test.Errorf("the control, its census whole, names packages the census missed:\n%s", control)
	}
}
