package load

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// staleWorkspaceFiles is a workspace whose go.work.sum is stale: a, the one module it uses, requires example.com/dep,
// whose sums go.work.sum lacks and no go.sum holds, so any go list of the workspace that reads dep writes them there,
// as go writes a workspace's sums whatever -mod says. Its one line is a sum nothing reads, which go keeps.
var staleWorkspaceFiles = map[string]string{
	"go.work":     "go 1.21\n\nuse ./a\n",
	"go.work.sum": "example.com/stale v1.0.0/go.mod h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\n",
	"a/go.mod":    "module example.com/a\n\ngo 1.21\n\nrequire example.com/dep v1.0.0\n",
	"a/a.go":      "package a\n\nimport \"example.com/dep\"\n\n// Value is dep's.\nconst Value = dep.Value\n",
}

// staleCgoFiles adds to staleWorkspaceFiles a package of a with a cgo file that imports dep, so a load has the cgo
// tool translate it with go list -compiled, which reads dep.
var staleCgoFiles = map[string]string{
	"a/native/native.go": "package native\n\nimport \"example.com/dep\"\n\n" +
		"// int twice(int value) { return value * 2; }\nimport \"C\"\n\n" +
		"// Twice is value doubled, by C, and dep's added.\n" +
		"func Twice(value int) int { return int(C.twice(C.int(value))) + dep.Value }\n",
}

// writeStaleWorkspace writes files as a workspace below a directory of the test's, and a module proxy beside it that
// serves example.com/dep from disk, and returns the workspace and what every go command there runs under: that proxy,
// no checksum database, and a module cache of the test's own that its cleanup can remove.
func writeStaleWorkspace(test *testing.T, files ...map[string]string) (string, []string) {
	test.Helper()
	directory := test.TempDir()
	workspace := filepath.Join(directory, "workspace")
	for _, each := range files {
		writeListingFiles(test, workspace, each)
	}
	proxy := filepath.Join(directory, "proxy")
	versions := filepath.Join(proxy, "example.com", "dep", "@v")
	writeListingFiles(test, versions, map[string]string{
		"list":        "v1.0.0\n",
		"v1.0.0.info": "{\"Version\":\"v1.0.0\",\"Time\":\"2026-01-01T00:00:00Z\"}\n",
		"v1.0.0.mod":  "module example.com/dep\n\ngo 1.21\n",
	})
	writeModuleZip(test, filepath.Join(versions, "v1.0.0.zip"), map[string]string{
		"example.com/dep@v1.0.0/go.mod": "module example.com/dep\n\ngo 1.21\n",
		"example.com/dep@v1.0.0/dep.go": "package dep\n\n// Value is one.\nconst Value = 1\n",
	})
	environment := []string{
		"GOPROXY=file://" + filepath.ToSlash(proxy),
		"GOSUMDB=off",
		"GOMODCACHE=" + filepath.Join(directory, "modules"),
		// The cache's files are written read-only otherwise, and the test's cleanup could not remove them.
		"GOFLAGS=-modcacherw",
		"GOTOOLCHAIN=local",
		// Empty, so go finds the workspace's go.work whatever the test's own environment names.
		"GOWORK=",
	}
	return workspace, environment
}

// writeModuleZip writes a module zip at path holding files, by their names in the zip.
func writeModuleZip(test *testing.T, path string, files map[string]string) {
	test.Helper()
	archive, createError := os.Create(path)
	if createError != nil {
		test.Fatal(createError)
	}
	writer := zip.NewWriter(archive)
	for name, contents := range files {
		entry, entryError := writer.Create(name)
		if entryError != nil {
			test.Fatal(entryError)
		}
		if _, writeError := entry.Write([]byte(contents)); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if closeError := writer.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	if closeError := archive.Close(); closeError != nil {
		test.Fatal(closeError)
	}
}

// readSums is the bytes of the workspace's go.work.sum.
func readSums(test *testing.T, workspace string) []byte {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(workspace, "go.work.sum"))
	if readError != nil {
		test.Fatal(readError)
	}
	return contents
}
