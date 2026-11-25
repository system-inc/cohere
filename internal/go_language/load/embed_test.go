package load

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// embeddedFiles are a module whose package, in-package tests and external tests each embed a file of their own.
var embeddedFiles = map[string]string{
	"go.mod": "module example.com/embedded\n\ngo 1.27\n",
	"p.go":   "package p\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar Data string\n",
	"p_test.go": "package p\n\nimport (\n\t_ \"embed\"\n\t\"testing\"\n)\n\n//go:embed inside.txt\n" +
		"var inside string\n\n" +
		"func TestInside(test *testing.T) { _ = inside }\n",
	"external_test.go": "package p_test\n\nimport (\n\t_ \"embed\"\n\t\"testing\"\n)\n\n//go:embed outside.txt\n" +
		"var outside string\n\nfunc TestOutside(test *testing.T) { _ = outside }\n",
	"data.txt":    "d\n",
	"inside.txt":  "i\n",
	"outside.txt": "o\n",
}

// Each package lists the files its own `//go:embed` patterns match, which the per-package cache keys on: the package
// and "p [p.test]" p's, "p_test [p.test]" none. go list -test would add the tests' own, and the load cannot ask for
// it (see newNode), so this pins the boundary as well as the files.
func TestEachPackageListsTheFilesItEmbeds(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for name, contents := range embeddedFiles {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	loaded, loadError := Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatal(loadError)
	}
	in := func(names ...string) []string {
		var paths []string
		for _, name := range names {
			paths = append(paths, filepath.Join(directory, name))
		}
		return paths
	}
	want := map[string][]string{
		"example.com/embedded":                                  in("data.txt"),
		"example.com/embedded [example.com/embedded.test]":      in("data.txt"),
		"example.com/embedded_test [example.com/embedded.test]": nil,
	}
	found := 0
	for _, root := range loaded.Roots {
		wanted, listed := want[root.ID]
		if !listed {
			continue
		}
		found++
		if !slices.Equal(root.EmbedFiles, wanted) {
			test.Errorf("%s embeds %q, want %q", root.ID, root.EmbedFiles, wanted)
		}
	}
	if found != len(want) {
		test.Errorf("found %d of the %d packages among the roots", found, len(want))
	}
}
