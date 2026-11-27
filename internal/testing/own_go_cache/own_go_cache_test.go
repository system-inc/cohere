package own_go_cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/testing/build_inputs"
)

// A go build handed an environment whose `GOCACHEPROG` names a program, as Loom's runner's does, builds into the
// test's own cache once Environment has set it, and never starts the program: here a program that doesn't exist,
// which go would fail to start, so the build passing and the cache filling show the program was never read.
func TestABuildOnItsOwnCacheIgnoresTheCallersCacheProgram(test *testing.T) {
	test.Parallel()
	module := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":  "module example.com/owncache\n\ngo 1.27\n",
		"main.go": "package main\n\nfunc main() {}\n",
	} {
		if writeError := os.WriteFile(filepath.Join(module, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	cache := test.TempDir()
	callers := append(os.Environ(), "GOCACHE="+test.TempDir(),
		"GOCACHEPROG="+filepath.Join(test.TempDir(), "no cache program here"), "GOFLAGS=", "GOWORK=off")
	build := exec.Command("go", "build", "-o", filepath.Join(test.TempDir(), "owncache"), ".")
	build.Dir = build_inputs.OutsideModule(test, module)
	build.Env = Environment(callers, cache)
	if output, buildError := build.CombinedOutput(); buildError != nil {
		test.Fatalf("go build on its own cache: %v\n%s", buildError, output)
	}
	entries, readError := os.ReadDir(cache)
	if readError != nil || len(entries) == 0 {
		test.Fatalf("the build left its own cache %s empty (%v), so it wrote somewhere else", cache, readError)
	}
}

// Environment leaves one `GOCACHE` and one `GOCACHEPROG`, its own, last, and the rest of the environment as it was,
// without writing into the slice it was handed.
func TestTheEnvironmentHoldsOneCacheOfItsOwn(test *testing.T) {
	test.Parallel()
	handed := []string{"HOME=/home", "GOCACHE=/caller", "GOCACHEPROG=program", "PATH=/bin"}
	kept := slices.Clone(handed)
	got := Environment(handed[:len(handed):len(handed)], "/own")
	want := []string{"HOME=/home", "PATH=/bin", "GOCACHE=/own", "GOCACHEPROG="}
	if !slices.Equal(got, want) {
		test.Errorf("Environment gave %q, want %q", got, want)
	}
	if !slices.Equal(handed, kept) {
		test.Errorf("Environment changed what it was handed to %q", handed)
	}
}
