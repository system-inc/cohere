package go_engine

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A staticcheck.conf applies to a project kept under the user's cache directory, where Loom's keeper puts the shared
// box's temporary directory (#sc5qmy7). staticcheck's config analyzer skipped every file under os.UserCacheDir(),
// meant for the files of Go's build cache, so it skipped every file of such a project and gave it the default
// configuration whatever its staticcheck.conf said. Here the project is copied into a temporary directory under a
// stand-in user cache directory, beside a staticcheck.conf allowing the fixture's dot import, and ST1001 reports
// nothing, cached and under `--no-cache` alike.
//
// Not parallel: it moves the process's own `TMPDIR` and the variable os.UserCacheDir reads, `HOME` on darwin and
// `XDG_CACHE_HOME` elsewhere, which every go command the engine runs reads too.
func TestAStaticcheckConfAppliesToAProjectUnderTheUserCacheDirectory(test *testing.T) {
	if runtime.GOOS == "windows" || runtime.GOOS == "plan9" {
		// os.UserCacheDir reads neither variable there, so the stand-in would not be the user's cache directory.
		test.Skipf("on %s the user's cache directory is not read from `HOME` or `XDG_CACHE_HOME`", runtime.GOOS)
	}
	pinGoSettings(test)
	standIn, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatalf("resolving the temporary directory: %v", resolveError)
	}
	if runtime.GOOS == "darwin" {
		test.Setenv("HOME", standIn)
	} else {
		test.Setenv("XDG_CACHE_HOME", standIn)
	}
	userCache, cacheError := os.UserCacheDir()
	if cacheError != nil {
		test.Fatalf("reading the stand-in user cache directory: %v", cacheError)
	}
	if !strings.HasPrefix(userCache, standIn+string(filepath.Separator)) && userCache != standIn {
		test.Fatalf("the user cache directory is %s, outside the stand-in %s", userCache, standIn)
	}
	temporary := filepath.Join(userCache, "temporary")
	if makeError := os.MkdirAll(temporary, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	test.Setenv("TMPDIR", temporary)

	// A subtest, since a test makes its temporary directories in the one it made first, before `TMPDIR` moved, and a
	// subtest makes its own.
	// Not parallel: its parent moved the process's `TMPDIR` and user cache directory, which no other test may share.
	test.Run("under the stand-in", func(test *testing.T) {
		root := copyFixture(test, filepath.Join("testdata", "staticcheck-conf"))
		if !strings.HasPrefix(root, userCache+string(filepath.Separator)) {
			test.Fatalf("the project is at %s, outside the user cache directory %s", root, userCache)
		}
		configuration := filepath.Join(root, "staticcheck.conf")
		allowing := []byte("dot_import_whitelist = [\"strings\"]\n")
		if writeError := os.WriteFile(configuration, allowing, 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", configuration, writeError)
		}
		cached := runCachedEngine(test, root)
		uncached := runCachedEngine(test, root, "--no-cache")
		if cached.withoutCounts != uncached.withoutCounts {
			test.Fatalf("cached\n%s\nunder --no-cache\n%s", cached.withoutCounts, uncached.withoutCounts)
		}
		if strings.Contains(cached.stream, `"rule":"cohere-go/ST1001"`) {
			test.Fatalf("the staticcheck.conf allowing the dot import did not apply under the user cache "+
				"directory:\n%s", cached.stream)
		}
	})
}

// pinGoSettings sets the go command's build cache, module cache, path and settings file in the environment as go env
// reports them now, for the rest of the test, so moving `HOME` or `XDG_CACHE_HOME`, under which go finds each by
// default, moves none of them: the engine's go commands read the caches and settings every other test does.
func pinGoSettings(test *testing.T) {
	test.Helper()
	output, outputError := exec.Command("go", "env", "-json", "GOCACHE", "GOMODCACHE", "GOPATH", "GOENV").Output()
	if outputError != nil {
		test.Fatalf("reading the go environment: %v", outputError)
	}
	var settings map[string]string
	if decodeError := json.Unmarshal(output, &settings); decodeError != nil {
		test.Fatalf("reading the go environment: %v", decodeError)
	}
	for name, value := range settings {
		test.Setenv(name, value)
	}
}
