package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/testing/written_repository"
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
	pipeline, atLink := runOverRaw(test, linked, exitFindings, "--no-cache", "--no-fix")
	if pipeline.described.record.FilesInPackage == 0 || pipeline.described.record.Root != real {
		test.Fatalf("the run at the link counted %d files under %s, want the real path's %s and its files",
			pipeline.described.record.FilesInPackage, pipeline.described.record.Root, real)
	}
	if normalized := normalize(atLink, real); normalized != atReal {
		test.Errorf("the run at the link wrote\n%s\nand at the real path\n%s", normalized, atReal)
	}
}

// emptyCensus makes a described project's census one that counted no Go file, as a walk that never reached the tree
// would, every count it feeds read 0, so a test can show what a run does with a census that is wrong.
func emptyCensus(pipeline *pipeline) {
	pipeline.described.census.Files, pipeline.described.census.Ours = nil, nil
	pipeline.described.inScope, pipeline.described.readableOurs = nil, nil
	pipeline.described.inScopeSet = map[string]bool{}
	pipeline.described.record.FilesInPackage, pipeline.described.record.FilesOurs = 0, 0
	pipeline.described.record.FilesInScope = 0
}

// A run whose census counted no Go file while its load reached packages in the workspace is never complete, and says
// why (#yyv7nr2): the two read one tree, so a census of nothing is one that never walked it, and every count it feeds
// would read 0 over work never done. With the census emptied over a module of two files, the types record names both
// files the load reached, the summary is incomplete and exits 1, and the note names the census. The control, the same
// module with its census whole, names none.
func TestACensusOfNothingBesideALoadOfPackagesIsNeverComplete(test *testing.T) {
	test.Parallel()

	pipeline, stream, noted := runOverNoted(test, copyFixture(test, filepath.Join("testdata", "clean")), exitFindings,
		emptyCensus, "--no-cache", "--no-fix")
	if len(pipeline.censusMissed) != 2 || !strings.Contains(stream, `"censusMissed":[`) ||
		!strings.Contains(stream, `"complete":false`) {
		test.Errorf("want the types record to name the 2 files the census missed and the run incomplete, got %v:\n%s",
			pipeline.censusMissed, stream)
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
	leftOut := func(pipeline *pipeline) {
		left = pipeline.described.census.Files[0]
		pipeline.described.census.Files = pipeline.described.census.Files[1:]
	}
	pipeline, stream, _ := runOverNoted(test, root, exitFindings, leftOut, "--no-cache", "--no-fix")
	if len(pipeline.censusMissed) != 1 ||
		pipeline.censusMissed[0].File != left ||
		pipeline.censusMissed[0].Package == "" {
		test.Fatalf("want %s named as the one file the census missed, with its package, got %v", left,
			pipeline.censusMissed)
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
	leftOut := func(pipeline *pipeline) {
		pipeline.described.census.Files = slices.DeleteFunc(slices.Clone(pipeline.described.census.Files),
			func(file string) bool { return file == elsewhere })
	}
	pipeline, _, _ := runOverNoted(test, root, exitFindings, leftOut, "--no-cache", "--no-fix")
	if len(pipeline.censusMissed) != 1 || pipeline.censusMissed[0].File != elsewhere {
		test.Errorf("want %s named as the one file the census missed, got %v", elsewhere, pipeline.censusMissed)
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
	pipeline, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--no-fix")
	throughLink := filepath.Join(root, "linked", "linked.go")
	if len(pipeline.censusMissed) != 0 || !strings.Contains(stream, `"complete":true`) {
		test.Errorf("want no file missed and the run complete, got %v:\n%s", pipeline.censusMissed, stream)
	}
	for _, file := range []string{filepath.Join(root, "real", "real.go"), throughLink} {
		if !strings.Contains(stream, `"kind":"finding"`) || !strings.Contains(stream, `"file":"`+file+`"`) {
			test.Errorf("want a finding in %s:\n%s", file, stream)
		}
	}
}

// writeTreeAt writes files under base, each name slash separated, making their directories, and makes each of
// repositories a git repository of its own, so a fixture holds the repositories its ignore files and nested clones
// need. It writes each without running git (written_repository.Write), so none reads the machine's git configuration.
func writeTreeAt(test *testing.T, base string, files map[string]string, repositories ...string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(base, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for _, repository := range repositories {
		writeError := written_repository.Write(filepath.Join(base, filepath.FromSlash(repository)))
		if writeError != nil {
			test.Fatalf("writing the repository in %s: %v", repository, writeError)
		}
	}
}

// A repository of its own below a module, with no go.mod, as a vendored clone or a checked-out submodule is, holds
// packages go reads as the module's (#hd5bsqh): the census counts their files and excludes them as in a repository of
// their own, so the run is complete, never read incomplete for files it was never meant to check, and the clone's
// finding is not the project's.
func TestARepositoryOfItsOwnBelowAModuleIsCountedAndExcluded(test *testing.T) {
	test.Parallel()

	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	writeTreeAt(test, root, map[string]string{
		"go.mod":               "module example.com/nested\n\ngo 1.27\n",
		"nested.go":            "// Package nested is one.\npackage nested\n",
		"clone/inner/inner.go": "// Package inner is two.\npackage inner\n\n// INNER_ONE shouts.\nconst INNER_ONE = 1\n",
	}, ".", "clone")
	pipeline, stream := runOverRaw(test, root, exitClean, "--no-cache", "--no-fix")
	inner := filepath.Join(root, "clone", "inner", "inner.go")
	if len(pipeline.censusMissed) != 0 || !strings.Contains(stream, `"complete":true`) {
		test.Errorf("want nothing missed and the run complete, got %v:\n%s", pipeline.censusMissed, stream)
	}
	if !slices.ContainsFunc(pipeline.described.record.Excluded, func(excluded contract.ExcludedFile) bool {
		return excluded.File == inner && excluded.Reason == project.InRepositoryOfItsOwn
	}) {
		test.Errorf(
			"want %s excluded as in a repository of its own, got %+v",
			inner,
			pipeline.described.record.Excluded,
		)
	}
}

// A linked use's files are judged by the ignore files of the repository holding their real path, as git judges them,
// never by the root's at the path through the link, below which git never looks (#hd5bsqh). Over a root whose
// .gitignore names `linked/kept.go` and a linked module whose own repository ignores `ignored.go`, both with a
// finding: the root's pattern governs nothing through the link, so kept.go is ours and its finding stands, and the
// real repository's rule excludes ignored.go, named by its ignore file.
func TestALinkedUsesFilesAreJudgedByTheRepositoryHoldingTheirRealPath(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	shouting := func(name string) string {
		return "package linked\n\n// " + name + " shouts.\nconst " + name + " = 1\n"
	}
	writeTreeAt(test, base, map[string]string{
		"root/go.work":         "go 1.27\n\nuse (\n\t./real\n\t./linked\n)\n",
		"root/.gitignore":      "linked/kept.go\n",
		"root/real/go.mod":     "module example.com/real\n\ngo 1.27\n",
		"root/real/real.go":    "// Package real is one.\npackage real\n",
		"elsewhere/go.mod":     "module example.com/linked\n\ngo 1.27\n",
		"elsewhere/.gitignore": "ignored.go\n",
		"elsewhere/linked.go":  "// Package linked is two.\npackage linked\n",
		"elsewhere/kept.go":    shouting("KEPT_ONE"),
		"elsewhere/ignored.go": shouting("IGNORED_ONE"),
	}, "root", "elsewhere")
	root := filepath.Join(base, "root")
	if linkError := os.Symlink(filepath.Join(base, "elsewhere"), filepath.Join(root, "linked")); linkError != nil {
		test.Fatalf("linking the module: %v", linkError)
	}
	pipeline, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--no-fix")
	kept, ignored := filepath.Join(root, "linked", "kept.go"), filepath.Join(root, "linked", "ignored.go")
	if len(pipeline.censusMissed) != 0 || !strings.Contains(stream, `"complete":true`) {
		test.Errorf("want nothing missed and the run complete, got %v:\n%s", pipeline.censusMissed, stream)
	}
	if !slices.Contains(pipeline.described.census.Ours, kept) || !strings.Contains(stream, `"file":"`+kept+`"`) {
		test.Errorf("want %s ours, the root's pattern governing nothing through the link, and its finding:\n%s",
			kept, stream)
	}
	if !slices.ContainsFunc(pipeline.described.record.Excluded, func(excluded contract.ExcludedFile) bool {
		return excluded.File == ignored && excluded.Reason == project.IgnoredByGit &&
			strings.HasSuffix(excluded.Source, "elsewhere/.gitignore:1")
	}) {
		test.Errorf("want %s ignored by its own repository's .gitignore, got %+v", ignored,
			pipeline.described.record.Excluded)
	}
}

// A linked use into the same repository is judged at its real path too (#hd5bsqh), where a judgment at the path
// through the link goes wrong both ways: the root's .gitignore names `vendored/ignored.go`, which git ignores at the
// real path, and `linked/kept.go`, which git never applies, since it never looks below a link. So ignored.go is
// excluded and its finding is never the project's, and kept.go is ours and its finding stands.
func TestALinkedUseIntoTheSameRepositoryIsJudgedAtItsRealPath(test *testing.T) {
	test.Parallel()

	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	shouting := func(name string) string {
		return "package vendored\n\n// " + name + " shouts.\nconst " + name + " = 1\n"
	}
	writeTreeAt(test, root, map[string]string{
		"go.work":              "go 1.27\n\nuse (\n\t./real\n\t./linked\n)\n",
		".gitignore":           "vendored/ignored.go\nlinked/kept.go\n",
		"real/go.mod":          "module example.com/real\n\ngo 1.27\n",
		"real/real.go":         "// Package real is one.\npackage real\n",
		"vendored/go.mod":      "module example.com/vendored\n\ngo 1.27\n",
		"vendored/vendored.go": "// Package vendored is two.\npackage vendored\n",
		"vendored/kept.go":     shouting("KEPT_ONE"),
		"vendored/ignored.go":  shouting("IGNORED_ONE"),
	}, ".")
	if linkError := os.Symlink(filepath.Join(root, "vendored"), filepath.Join(root, "linked")); linkError != nil {
		test.Fatalf("linking the module: %v", linkError)
	}
	pipeline, stream := runOverRaw(test, root, exitFindings, "--no-cache", "--no-fix")
	kept, ignored := filepath.Join(root, "linked", "kept.go"), filepath.Join(root, "linked", "ignored.go")
	// A finding names its file, as the project record's excluded files do too, so a finding is read by its kind.
	foundIn := func(file string) bool {
		return slices.ContainsFunc(strings.Split(stream, "\n"), func(line string) bool {
			return strings.HasPrefix(line, `{"kind":"finding"`) && strings.Contains(line, `"file":"`+file+`"`)
		})
	}
	if !slices.Contains(pipeline.described.census.Ours, kept) || !foundIn(kept) {
		test.Errorf("want %s ours, its link-named pattern governing nothing, and its finding:\n%s", kept, stream)
	}
	if foundIn(ignored) || !slices.ContainsFunc(pipeline.described.record.Excluded,
		func(excluded contract.ExcludedFile) bool {
			return excluded.File == ignored && excluded.Reason == project.IgnoredByGit
		}) {
		test.Errorf("want %s ignored at its real path and no finding of it, got %+v:\n%s", ignored,
			pipeline.described.record.Excluded, stream)
	}
}
