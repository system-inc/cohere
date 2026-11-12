package load

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A load at a real root whose caller's working directory came through a symbolic link to it names every input at the
// real root (#hd5bsqh): go names the go.mod and go.work it reads, and the modules a go.work uses, under the directory
// its PWD spells, so a run through the link would key its replay on other paths than a run at the real root. Each go
// command the load runs is told the workspace as the load names it, and the inputs of a load given the link as PWD are
// the inputs of a load given none.
func TestALoadNamesItsInputsAtTheRootItWasGivenWhateverItsCallersWorkingDirectory(test *testing.T) {
	test.Parallel()

	base, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	root, linked := filepath.Join(base, "real"), filepath.Join(base, "linked")
	writeListingFiles(test, base, map[string]string{
		"real/go.work":               "go 1.24\n\nuse ./module\n",
		"real/module/go.mod":         "module example.com/module\n\ngo 1.24\n",
		"real/module/value/value.go": "package value\n\n// Value is one.\nconst Value = 1\n",
	})
	if linkError := os.Symlink(root, linked); linkError != nil {
		test.Fatalf("linking the root: %v", linkError)
	}
	inputsUnder := func(environment []string) Inputs {
		loaded, loadError := PackagesCached(root, []string{"example.com/module/..."}, environment, nil, Caching{})
		if loadError != nil {
			test.Fatalf("loading %s: %v", root, loadError)
		}
		return loaded.Inputs
	}
	throughLink := inputsUnder([]string{"PWD=" + linked})
	for _, path := range slices.Concat(throughLink.Files, throughLink.Directories, throughLink.Absent) {
		if path == linked || strings.HasPrefix(path, linked+string(filepath.Separator)) {
			test.Errorf("the load named %s through the link its caller's working directory came through", path)
		}
	}
	if plain := inputsUnder(nil); !slices.Equal(plain.Files, throughLink.Files) ||
		!slices.Equal(plain.Directories, throughLink.Directories) || !slices.Equal(plain.Absent, throughLink.Absent) {
		test.Errorf("the inputs differ with the link as PWD:\n%+v\nand without:\n%+v", throughLink, plain)
	}
}
