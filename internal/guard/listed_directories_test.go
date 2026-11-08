package guard

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_types"
)

// listedDirectoriesChildVariable names the loader and the fixture module TestListedDirectoriesChild loads.
const listedDirectoriesChildVariable = "COHERE_LISTED_DIRECTORIES_CHILD"

// Both loaders a test reaches packages through, the engine's load and the tests' go_types, open in the test's own
// process every directory their go list read, so Go's test cache keys a test's result on its fixtures (#cmpsz8p).
// Before, go list read them in its own process and nothing was keyed: a rule's planted fixture file, which fails the
// rule's test when run uncached, read `ok (cached)` after it was added.
//
// What Go keys a result on is what the test binary logs under -test.testlogfile, so this reads that log. It runs
// itself again as a child with the flag set, the child loads a fixture through one loader, and the log must open each
// fixture package's directory, and a directory below the wildcard that holds no package, where a new package would
// appear. Three ways in: go_types, which always asks go list; load on a module, which its in-process lister answers
// and which keys itself; and load in `GOPATH` mode, as the rule fixtures' analysistest loads, where go list answers.
func TestEveryLoaderOpensWhatItsGoListRead(test *testing.T) {
	test.Parallel()
	for _, loader := range []string{"load", "go_types", "load-gopath"} {
		test.Run(loader, func(test *testing.T) {
			test.Parallel()
			module := test.TempDir()
			// In `GOPATH` mode the packages live under src, and an import names its path there.
			packages, importPrefix := "", "example.com/fixture/"
			files := map[string]string{"go.mod": "module example.com/fixture\n\ngo 1.27\n"}
			if loader == "load-gopath" {
				packages, importPrefix, files = "src/", "", map[string]string{}
			}
			files[packages+"alpha/alpha.go"] = "package alpha\n"
			files[packages+"beta/gamma/gamma.go"] = "package gamma\n\nimport _ \"" + importPrefix + "alpha\"\n"
			files[packages+"beta/notes.txt"] = "a directory with no Go file, where a new package would appear\n"
			for name, contents := range files {
				path := filepath.Join(module, name)
				if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
					test.Fatal(makeError)
				}
				if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			log := filepath.Join(test.TempDir(), "testlog")
			child := exec.Command(os.Args[0], "-test.run=^TestListedDirectoriesChild$", "-test.testlogfile="+log)
			child.Env = append(os.Environ(), listedDirectoriesChildVariable+"="+loader+":"+module)
			if output, childError := child.CombinedOutput(); childError != nil {
				test.Fatalf("the child that loads through %s failed: %v\n%s", loader, childError, output)
			}
			contents, readError := os.ReadFile(log)
			if readError != nil {
				test.Fatal(readError)
			}
			opened := map[string]bool{}
			for _, line := range strings.Split(string(contents), "\n") {
				if path, isOpen := strings.CutPrefix(line, "open "); isOpen {
					opened[resolvedPath(path)] = true
				}
			}
			for _, wanted := range []string{"alpha", "beta", "beta/gamma"} {
				if !opened[resolvedPath(filepath.Join(module, packages+wanted))] {
					test.Errorf("%s's go list read %s, and the test process never opened it, so Go's cache cannot "+
						"key on it", loader, wanted)
				}
			}
		})
	}
}

// TestListedDirectoriesChild is TestEveryLoaderOpensWhatItsGoListRead's child, and does nothing in any other run.
// Not parallel: it is the whole of the child's run, which logs everything the process opens.
func TestListedDirectoriesChild(test *testing.T) {
	loader, module, isChild := strings.Cut(os.Getenv(listedDirectoriesChildVariable), ":")
	if !isChild {
		return
	}
	var loadError error
	switch loader {
	case "load":
		_, loadError = load.Packages(module, []string{"./..."}, []string{"GOWORK=off"}, nil)
	case "load-gopath":
		_, loadError = load.Packages(module, []string{"alpha", "beta/..."},
			[]string{"GOPATH=" + module, "GO111MODULE=off", "GOWORK=off"}, nil)
	case "go_types":
		_, loadError = go_types.Load(
			go_types.Configuration{Directory: module, Environment: []string{"GOWORK=off"}},
			"./...",
		)
	default:
		loadError = fmt.Errorf("no loader named %q", loader)
	}
	if loadError != nil {
		test.Fatal(loadError)
	}
}

// resolvedPath is path with its symbolic links followed, so /var and /private/var are the same directory.
func resolvedPath(path string) string {
	if resolved, resolveError := filepath.EvalSymlinks(path); resolveError == nil {
		return resolved
	}
	return path
}
