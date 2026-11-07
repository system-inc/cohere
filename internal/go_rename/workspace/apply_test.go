package workspace

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_rename/rename"
)

// cgoFixture is a module with a cgo package: archive.go calls C, so go/packages parses cgo's translation of it from
// the build cache, and it imports text, plain Go holding a local a rename pass may rename. It returns the root,
// resolved as Load resolves it.
func cgoFixture(test *testing.T) string {
	test.Helper()
	cgo_testing.RequireTranslation(test)
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	files := map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
		"internal/archive/archive.go": "package archive\n\n/*\nstatic int twice(int value) { " +
			"return value * 2; }\n*/\n" +
			"import \"C\"\n\nimport \"fixture/internal/text\"\n\n// Twice doubles a value through C.\n" +
			"func Twice(value int) int {\n\treturn int(C.twice(C.int(value))) + len(text.Label(value))\n}\n",
		"internal/text/text.go": "package text\n\n// Label names a value.\nfunc Label(value int) string {\n\t" +
			"msg := \"value\"\n\tif value > 0 {\n\t\treturn msg\n\t}\n\treturn \"\"\n}\n",
	}
	// A comment naming the root makes the cgo file's bytes this test's own: under -trimpath go keys cgo's translation
	// without its directory, and would serve one cached for the same bytes in another test's directory, whose //line
	// directives name that directory. FileKey keys that copy alike (rename's own test pins it), but translationOf
	// finds the translation by the directory its directives name, so the test runs cgo here.
	files["internal/archive/archive.go"] = "// Written under " + root + ".\n\n" + files["internal/archive/archive.go"]
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if directoryError := os.MkdirAll(filepath.Dir(path), 0o755); directoryError != nil {
			test.Fatal(directoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// translationOf is the file of a load that is cgo's translation of archive.go: outside the root, its package clause
// named by //line as archive.go, and never one a rename may edit. It reads the directive itself, apart from
// rename.FileKey, which the test holds to account.
func translationOf(test *testing.T, loaded *Workspace, root string) string {
	test.Helper()
	source := filepath.Join(root, "internal", "archive", "archive.go")
	for _, rootsPackage := range loaded.Roots {
		for _, file := range rootsPackage.Syntax {
			read := rootsPackage.Fset.PositionFor(file.Pos(), false).Filename
			if strings.HasPrefix(read, root+string(filepath.Separator)) {
				continue
			}
			if loaded.Editable(rootsPackage, file) {
				test.Fatalf("%s, in the build cache, reads as editable", read)
			}
			if rootsPackage.Fset.PositionFor(file.Package, true).Filename == source {
				return read
			}
		}
	}
	test.Fatalf("no file of the load is cgo's translation of %s keyed by it", source)
	return ""
}

// A pass renaming a local in a package a cgo package imports, and respelling a comment of its cgo file, is proven and
// written. The rewrite changes cgo's inputs, so its translation of archive.go, which go/packages parses from the
// build cache, is read under a new hash name after it; the fingerprint keys it by archive.go both times, and reads 0
// mismatches. A second pass
// over the result has nothing to rename and writes nothing. Keyed by the cache name, the translation reads as gone
// in one load and new in the other, as Adamic's bridge/tsgo/archive did.
func TestARenameInACgoPackageIsProvenAcrossTheTranslationsNewName(test *testing.T) {
	test.Parallel()
	root := cgoFixture(test)
	loaded, loadError := Load(root, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	beforeName := translationOf(test, loaded, root)

	names := filepath.Join(root, "internal", "text", "text.go")
	source, readError := os.ReadFile(names)
	if readError != nil {
		test.Fatal(readError)
	}
	var edits []rename.Edit
	for offset := 0; ; {
		index := strings.Index(string(source[offset:]), "msg")
		if index < 0 {
			break
		}
		edits = append(edits, rename.Edit{Filename: names, Offset: offset + index, OldText: "msg", NewText: "message"})
		offset += index + len("msg")
	}
	if len(edits) != 2 {
		test.Fatalf("want msg's declaration and its use, got %d edits", len(edits))
	}
	// The pass also respells a word of a comment in archive.go, a cgo file. Here neither a plain file of the cgo
	// package nor a package it imports moves its translation's name, so this is what moves it, as something did on
	// Adamic's: the fingerprint must hold however the name moves.
	archive := filepath.Join(root, "internal", "archive", "archive.go")
	archiveSource, archiveError := os.ReadFile(archive)
	if archiveError != nil {
		test.Fatal(archiveError)
	}
	edits = append(edits, rename.Edit{Filename: archive, Offset: strings.Index(string(archiveSource), "a value"),
		OldText: "a value", NewText: "a number"})
	_, rewritten, rewriteError := rename.RewriteFile(names, edits[:2])
	if rewriteError != nil {
		test.Fatal(rewriteError)
	}
	_, archiveRewritten, archiveRewriteError := rename.RewriteFile(archive, edits[2:])
	if archiveRewriteError != nil {
		test.Fatal(archiveRewriteError)
	}
	overlaid, overlayError := LoadWithOverlay(root, nil, map[string][]byte{names: rewritten, archive: archiveRewritten})
	if overlayError != nil {
		test.Fatal(overlayError)
	}
	if afterName := translationOf(test, overlaid, root); afterName == beforeName {
		test.Fatalf("cgo's translation kept its name %s under the rewrite, so this test proves nothing", afterName)
	}

	var report bytes.Buffer
	var refusals bytes.Buffer
	output := bufio.NewWriter(&report)
	errorOutput := bufio.NewWriter(&refusals)
	exitCode := ApplyVerified(loaded, root, nil, edits, rename.GroupByFile(edits), "test", output, errorOutput)
	if flushError := output.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
	if flushError := errorOutput.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
	if exitCode != 0 || !strings.Contains(report.String(), ", 0 mismatches") {
		test.Fatalf("exit %d, want the rename proven and written:\n%s%s", exitCode, report.String(), refusals.String())
	}
	written, readAgainError := os.ReadFile(names)
	if readAgainError != nil || !bytes.Equal(written, rewritten) {
		test.Fatalf("text.go reads\n%s\nwant\n%s (%v)", written, rewritten, readAgainError)
	}

	again, againError := Load(root, nil)
	if againError != nil {
		test.Fatal(againError)
	}
	report.Reset()
	exitCode = ApplyVerified(again, root, nil, nil, nil, "test", output, errorOutput)
	if flushError := output.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
	if exitCode != 0 || !strings.Contains(report.String(), "wrote 0 files") {
		test.Fatalf("a second pass exit %d, want nothing written:\n%s%s", exitCode, report.String(), refusals.String())
	}
}
