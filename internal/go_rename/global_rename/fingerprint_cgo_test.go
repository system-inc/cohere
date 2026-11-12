package global_rename

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_rename/rename"
	"github.com/system-inc/cohere/internal/go_rename/workspace"
	"github.com/system-inc/cohere/internal/write_root"

	"golang.org/x/tools/go/packages"
)

// The cross-package fingerprint keys a cgo package's translations by their sources too: a rename of text's
// FormatURL, which a plain file of the cgo package archive calls, with a comment of archive's cgo file respelled so
// cgo's translations are read under new hash names after the rewrite, is proven with 0 mismatches and written.
func TestARenameReachingACgoPackageIsProvenAcrossItsTranslationsNewNames(test *testing.T) {
	test.Parallel()
	cgo_testing.RequireTranslation(test)
	fixture := newFixture(test, map[string]string{
		"internal/text/text.go": "package text\n\n// FormatURL formats an address.\nfunc FormatURL(value int) string " +
			"{\n\tif value > 0 {\n\t\treturn \"/\"\n\t}\n\treturn \"\"\n}\n",
		"internal/archive/names.go": "package archive\n\nimport \"fixture/internal/text\"\n\n" +
			"// Label names a value.\nfunc Label(value int) string {\n\treturn text.FormatURL(Twice(value))\n}\n",
	})
	archive := filepath.Join(fixture.root, "internal", "archive", "archive.go")
	// The comment naming the root keeps another test's translation of the same bytes, cached under -trimpath with
	// its own directory in its //line directives, from being served here, so the translations this test reads are
	// ones cgo made in this directory and moved by the rewrite.
	source := "// Written under " + fixture.root + ".\n\npackage archive\n\n/*\nstatic int twice(int value) { " +
		"return value * 2; }\n*/\nimport \"C\"\n\n// Twice doubles a value through C.\n" +
		"func Twice(value int) int {\n\t" +
		"return int(C.twice(C.int(value)))\n}\n"
	if directoryError := os.MkdirAll(filepath.Dir(archive), 0o755); directoryError != nil {
		test.Fatal(directoryError)
	}
	if writeError := os.WriteFile(archive, []byte(source), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/text", Name: "FormatURL", Kind: KindFunction, NewName: "FormatUrl"},
	), Options{})
	requireAccepted(test, result, 1)
	// Never expected: a comment's word, which moves cgo's translations to new names.
	result.CommentEdits = append(result.CommentEdits, rename.Edit{
		Filename: filepath.Join(loaded.Root, "internal", "archive", "archive.go"),
		Offset:   strings.Index(source, "a value"), OldText: "a value", NewText: "a number",
	})
	files, rewriteError := RewrittenFiles(result)
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	reloaded, reloadError := workspace.LoadWithOverlay(fixture.root, nil, files)
	if reloadError != nil {
		test.Fatal(reloadError)
	}
	if moved := cacheFiles(test, fixture.root, loaded.Roots); len(moved) == 0 ||
		moved == cacheFiles(test, fixture.root, reloaded.Roots) {
		test.Fatalf("cgo's translations are read under the same names %q after the rewrite, so this proves nothing",
			moved)
	}
	applyClean(test, fixture, loaded, result)
	if written := fixture.read(test, "internal/archive/names.go"); !strings.Contains(written, "text.FormatUrl(") {
		test.Fatalf("names.go reads\n%s", written)
	}
}

// cacheFiles is every file of roots read from outside fixtureRoot, sorted and joined: cgo's translations, which go list
// reads from the build cache wherever `GOCACHE` puts it. Each file is judged by where it really is, never by the
// cache's name. Go's default cache directory is named go-build, but Home's land gate gives `GOCACHE` a directory of
// its own, and a match on that name found no translation there, so this test failed on Linux for its cache path alone
// (#k3kn30s).
func cacheFiles(test *testing.T, fixtureRoot string, roots []*packages.Package) string {
	test.Helper()
	writeRoot, newError := write_root.New(fixtureRoot)
	if newError != nil {
		test.Fatal(newError)
	}
	var names []string
	for _, root := range roots {
		for _, file := range root.Syntax {
			name := root.Fset.PositionFor(file.Pos(), false).Filename
			_, inside, insideError := writeRoot.Inside(name)
			if insideError != nil {
				test.Fatal(insideError)
			}
			if !inside && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	slices.Sort(names)
	return strings.Join(names, " ")
}
