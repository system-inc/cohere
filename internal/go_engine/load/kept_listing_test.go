package load

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// listingFiles are a module of two packages, base and user, user importing base and both with a test.
var listingFiles = map[string]string{
	"go.mod":       "module example.com/listing\n\ngo 1.24\n",
	"base/base.go": "package base\n\n// Base is one.\nfunc Base() int { return 1 }\n",
	"base/base_test.go": "package base\n\nimport \"testing\"\n\n// TestBase calls Base.\n" +
		"func TestBase(test *testing.T) {\n\ttest.Parallel()\n\t_ = Base()\n}\n",
	"user/user.go": "package user\n\nimport \"example.com/listing/base\"\n\n// User calls Base.\n" +
		"func User() int { return base.Base() }\n",
}

// writeListingFiles writes files into root.
func writeListingFiles(test *testing.T, root string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatalf("making the directory of %s: %v", name, makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
}

// loadSummary is what a load says of its packages and diagnostics, one line each, sorted, so two loads compare.
func loadSummary(test *testing.T, root string, listings ListingStore) ([]string, bool) {
	test.Helper()
	loaded, loadError := PackagesCached(root, []string{"./..."}, []string{"GOWORK=off"}, nil,
		Caching{Listings: listings})
	if loadError != nil {
		test.Fatalf("loading %s: %v", root, loadError)
	}
	var summary []string
	for _, loadedPackage := range loaded.Packages {
		imports := make([]string, 0, len(loadedPackage.Imports))
		for path, imported := range loadedPackage.Imports {
			imports = append(imports, path+"="+imported.ID)
		}
		slices.Sort(imports)
		summary = append(summary, fmt.Sprintf("%s files %v embeds %v ignored %v imports %v", loadedPackage.ID,
			loadedPackage.GoFiles, loadedPackage.EmbedFiles, loadedPackage.IgnoredFiles, imports))
	}
	for _, diagnostic := range loaded.Diagnostics {
		summary = append(summary, fmt.Sprintf("diagnostic %+v", diagnostic))
	}
	slices.Sort(summary)
	return summary, loaded.ListingKept
}

// go list's answer is kept until something it reads moves. An edit below a file's imports keeps it; an edit to a
// file's imports, its build constraint or its embeds, a Go file, an assembly file or a package added, or the module
// file changed, asks go list again. Either way the load is the one a load without the kept answer gives.
func TestGoListsAnswerIsKeptUntilWhatItReadsMoves(test *testing.T) {
	test.Parallel()

	for _, edit := range []struct {
		name  string
		files map[string]string
		kept  bool
	}{
		{name: "a body edited", kept: true, files: map[string]string{
			"base/base.go": "package base\n\n// Base is two.\nfunc Base() int { return 2 }\n\n" +
				"// Comment added below.\n",
		}},
		{name: "an import added", files: map[string]string{
			"user/user.go": "package user\n\nimport (\n\t\"strings\"\n\n\t\"example.com/listing/base\"\n)\n\n" +
				"// User calls Base.\nfunc User() int { return base.Base() + len(strings.Repeat(\"a\", 1)) }\n",
		}},
		{name: "a build constraint added", files: map[string]string{
			"base/base_test.go": "//go:build ignore\n\npackage base\n\nimport \"testing\"\n\n" +
				"// TestBase calls Base.\n" +
				"func TestBase(test *testing.T) {\n\ttest.Parallel()\n\t_ = Base()\n}\n",
		}},
		{name: "a file added", files: map[string]string{
			"base/more.go": "package base\n\n// More is two.\nfunc More() int { return 2 }\n",
		}},
		{name: "an assembly file added", files: map[string]string{
			"base/stub.s": "// An assembly file go list names among the package's files.\n",
		}},
		{name: "a package added", files: map[string]string{
			"extra/extra.go": "package extra\n\n// Extra is three.\nfunc Extra() int { return 3 }\n",
		}},
		{name: "an embed added", files: map[string]string{
			"base/data.txt": "data\n",
			"base/embed.go": "package base\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar data string\n\n" +
				"// Data is the embedded file.\nfunc Data() string { return data }\n",
		}},
		{name: "the module file changed", files: map[string]string{
			"go.mod": "module example.com/listing\n\ngo 1.25\n",
		}},
	} {
		test.Run(edit.name, func(test *testing.T) {
			test.Parallel()

			root, resolveError := filepath.EvalSymlinks(test.TempDir())
			if resolveError != nil {
				test.Fatalf("resolving the temporary directory: %v", resolveError)
			}
			writeListingFiles(test, root, listingFiles)
			listings := &listingMap{listings: map[[sha256.Size]byte][]byte{}}
			if _, kept := loadSummary(test, root, listings); kept {
				test.Fatalf("want the first load to ask go list")
			}
			if _, kept := loadSummary(test, root, listings); !kept {
				test.Fatalf("want the second load, with nothing moved, to keep go list's answer")
			}
			writeListingFiles(test, root, edit.files)
			edited, kept := loadSummary(test, root, listings)
			if kept != edit.kept {
				test.Errorf("want the answer kept %t after the edit, got %t", edit.kept, kept)
			}
			fresh, _ := loadSummary(test, root, nil)
			if strings.Join(edited, "\n") != strings.Join(fresh, "\n") {
				test.Fatalf("after the edit\n%s\nwithout a kept answer\n%s", strings.Join(edited, "\n"),
					strings.Join(fresh, "\n"))
			}
		})
	}
}
