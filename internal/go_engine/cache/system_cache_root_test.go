package cache

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// assertOneLineNotKept fails the test unless notKept is a reason, said on one line after "system cache not kept: ",
// that holds want.
func assertOneLineNotKept(test *testing.T, notKept *NotKept, want string) {
	test.Helper()
	if notKept == nil {
		test.Fatalf("want the system cache not kept for a reason holding %q, got it kept", want)
	}
	line := notKept.String()
	if !strings.HasPrefix(line, "system cache not kept: ") || strings.ContainsAny(line, "\r\n") {
		test.Errorf("want one line after %q, got %q", "system cache not kept: ", line)
	}
	if !strings.Contains(notKept.Reason(), want) {
		test.Errorf("want a reason holding %q, got %q", want, notKept.Reason())
	}
}

// `$COHERE_SYSTEM_CACHE`, when set to an absolute directory, is the root, cleaned.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, which test.Setenv refuses beside parallel tests.
func TestTheSystemCacheVariableIsTheRoot(test *testing.T) {
	chosen := test.TempDir()
	test.Setenv(systemCacheVariable, chosen+string(filepath.Separator)+"system"+string(filepath.Separator))
	directory, notKept := SystemCacheRoot()
	if notKept != nil {
		test.Fatalf("want the root kept, got %s", notKept)
	}
	if want := filepath.Join(chosen, "system"); directory != want {
		test.Errorf("want the root %s, got %s", want, directory)
	}
}

// A relative `$COHERE_SYSTEM_CACHE` is a reason the system cache is not kept, never a directory guessed from where
// the run happens to be.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, which test.Setenv refuses beside parallel tests.
func TestARelativeSystemCacheVariableIsNotKept(test *testing.T) {
	test.Setenv(systemCacheVariable, filepath.Join("relative", "cache"))
	directory, notKept := SystemCacheRoot()
	if directory != "" {
		test.Errorf("want no root, got %s", directory)
	}
	assertOneLineNotKept(test, notKept, "must be absolute")
}

// With `$COHERE_SYSTEM_CACHE` unset, the root is the user's cache directory joined with `cohere`, read from `HOME` on
// macOS and `XDG_CACHE_HOME` on Linux.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, `HOME`, `XDG_CACHE_HOME` and `LocalAppData`, which test.Setenv refuses
// beside parallel tests.
func TestTheRootIsTheUserCacheDirectoryWhenTheVariableIsUnset(test *testing.T) {
	home := test.TempDir()
	xdgCacheHome := test.TempDir()
	localAppData := test.TempDir()
	test.Setenv(systemCacheVariable, "")
	test.Setenv("HOME", home)
	test.Setenv("XDG_CACHE_HOME", xdgCacheHome)
	test.Setenv("LocalAppData", localAppData)
	var want string
	switch runtime.GOOS {
	case "darwin", "ios":
		want = filepath.Join(home, "Library", "Caches", "cohere")
	case "windows":
		want = filepath.Join(localAppData, "cohere")
	case "plan9":
		want = filepath.Join(home, "lib", "cache", "cohere")
	default:
		want = filepath.Join(xdgCacheHome, "cohere")
	}
	directory, notKept := SystemCacheRoot()
	if notKept != nil {
		test.Fatalf("want the root kept, got %s", notKept)
	}
	if directory != want {
		test.Errorf("want the root %s, got %s", want, directory)
	}
}

// With no `HOME` and no `XDG_CACHE_HOME`, as in a sealed job's empty environment, there is no user cache directory,
// and the system cache is not kept for that reason.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, `HOME`, `XDG_CACHE_HOME` and `LocalAppData`, which test.Setenv refuses
// beside parallel tests.
func TestNoHomeIsNotKept(test *testing.T) {
	test.Setenv(systemCacheVariable, "")
	test.Setenv("HOME", "")
	test.Setenv("XDG_CACHE_HOME", "")
	test.Setenv("LocalAppData", "")
	directory, notKept := SystemCacheRoot()
	if directory != "" {
		test.Errorf("want no root, got %s", directory)
	}
	assertOneLineNotKept(test, notKept, "finding the user's cache directory")
}

// A root that cannot be written is found, since finding it writes nothing, and the first write under it fails. That
// failure, and the trim's, is a reason the system cache is not kept, on one line, never an error that stops a run.
// A regular file where the root's directory goes makes every write under it fail, with no permission to change.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, which test.Setenv refuses beside parallel tests.
func TestAnUnwritableRootIsNotKeptAtTheFirstWrite(test *testing.T) {
	file := filepath.Join(test.TempDir(), "system")
	if writeError := os.WriteFile(file, []byte("a file, not a directory"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	test.Setenv(systemCacheVariable, file)
	root, notKept := SystemCacheRoot()
	if notKept != nil {
		test.Fatalf("finding the root wrote nothing, so it is found: got %s", notKept)
	}

	directory := filepath.Join(root, "go", "standard-library")
	putError := OpenAt(directory).Put(sampleKey, []byte("a standard library table"))
	if putError == nil {
		test.Fatal("a Put under a file succeeded")
	}
	assertOneLineNotKept(test, NotKeptBecause(putError), directory)
	assertOneLineNotKept(test, NotKeptBecause(errors.Join(putError, putError)), "; ")

	assertOneLineNotKept(test, TrimSystemCache(root, directory, SystemCacheSizeCap), "trimming "+directory)
	if contents, readError := os.ReadFile(file); readError != nil || string(contents) != "a file, not a directory" {
		test.Errorf("the file where the root goes was changed: %q (%v)", contents, readError)
	}
}
