package parity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

// RequireOnlyAdjudicatedUnderUserCacheDirectory is RequireOnlyAdjudicated over a differences fixture that sits under
// userCacheDirectory, absolute, which is the user's cache directory, os.UserCacheDir()'s answer, while the original
// and the port run over it, and at no other time: the fixtures load before it is set and every variable is put back
// once they have run, failed or not. It is for an original that reads that directory, as staticcheck's config
// analyzer does, skipping every file under it for one of Go's build cache, so it reads no staticcheck.conf for a
// project kept there and reports what that project's staticcheck.conf allows, where the engine's own reads it
// (#sc5qmy7). The difference is then held as any other, a `drops:` record for each finding the original alone makes.
//
// It sets the variable os.UserCacheDir reads on the machine's system: `XDG_CACHE_HOME`, set to userCacheDirectory; on
// darwin `HOME`, set to the folder two above userCacheDirectory, which must end in Library/Caches as darwin's does;
// and on windows `LocalAppData`. It fails the test when os.UserCacheDir then answers anything else, and when a
// fixture file sits outside userCacheDirectory, since the run would then prove nothing of the directory. The
// variable is the process's own, so the test that calls it is not parallel, which testing's Setenv holds it to.
func RequireOnlyAdjudicatedUnderUserCacheDirectory(
	test testing.TB,
	original *analysis.Analyzer,
	port *analysis.Analyzer,
	testdata string,
	fixture string,
	userCacheDirectory string,
	given ...Record,
) {
	test.Helper()
	if userCacheDirectory == "" {
		test.Fatal("the user cache directory to run under is empty: name the folder the fixture sits in")
	}
	requireOnlyAdjudicated(test, Target{}, original, port, testdata, fixture, userCacheDirectory, given)
}

// runUnderUserCacheDirectory runs run with userCacheDirectory as the user's cache directory (see
// RequireOnlyAdjudicatedUnderUserCacheDirectory), and runs it as it is when userCacheDirectory is empty. Every root
// file of loads must sit under userCacheDirectory.
func runUnderUserCacheDirectory(
	test testing.TB,
	userCacheDirectory string,
	loads []loadedFixtures,
	run func(),
) {
	test.Helper()
	if userCacheDirectory == "" {
		run()
		return
	}
	directory, absoluteError := filepath.Abs(userCacheDirectory)
	if absoluteError != nil {
		test.Fatalf("resolving the user cache directory %s: %v", userCacheDirectory, absoluteError)
	}
	for _, fixtures := range loads {
		for _, root := range fixtures.roots {
			for _, file := range root.GoFiles {
				if !strings.HasPrefix(file, directory+string(filepath.Separator)) {
					test.Fatalf("%s sits outside the user cache directory %s, so running under it proves nothing "+
						"of it", fixtures.fileName(file), directory)
				}
			}
		}
	}
	name, value := userCacheVariable(test, directory)
	previous, wasSet := os.LookupEnv(name)
	// Setenv refuses a parallel test, whose siblings would read the variable while it is moved, and puts it back when
	// the test ends, which only a failure here before the deferred restore below would leave to it.
	test.Setenv(name, previous)
	if !wasSet {
		// Ignored: Unsetenv fails only for a name holding "=" or a NUL, and the names userCacheVariable gives hold
		// neither.
		_ = os.Unsetenv(name)
	}
	// Ignored: as above, for Setenv.
	_ = os.Setenv(name, value)
	defer func() {
		if wasSet {
			// Ignored: as above.
			_ = os.Setenv(name, previous)
			return
		}
		// Ignored: as above.
		_ = os.Unsetenv(name)
	}()
	answered, cacheError := os.UserCacheDir()
	if cacheError != nil || answered != directory {
		test.Fatalf("with %s=%s the user cache directory is %q (%v), want %s", name, value, answered, cacheError,
			directory)
	}
	run()
}

// userCacheVariable is the variable os.UserCacheDir reads on the machine's system, and the value that makes it answer
// directory. plan9's lib/cache under `home` is none the harness has needed, so it fails the test there.
func userCacheVariable(test testing.TB, directory string) (name string, value string) {
	test.Helper()
	switch runtime.GOOS {
	case "darwin", "ios":
		library := filepath.Dir(directory)
		if filepath.Base(directory) != "Caches" || filepath.Base(library) != "Library" {
			test.Fatalf("the user cache directory %s does not end in Library/Caches, which darwin's always does",
				directory)
		}
		return "HOME", filepath.Dir(library)
	case "windows":
		return "LocalAppData", directory
	case "plan9":
		test.Fatalf("running under a user cache directory is not built for %s", runtime.GOOS)
		return "", ""
	default:
		return "XDG_CACHE_HOME", directory
	}
}
