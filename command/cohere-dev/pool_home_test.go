package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/testing/build_inputs"
)

// A run with `HOME` set to a scratch directory still uses the machine's pool, the one under the account's own
// home, so it waits its turn with everyone else's and starts no trim of its own (#d0x1fhp). test's closing
// gate set `HOME` to a scratch directory with the go cache passed through, and ran in a pool of its own.
// Status reads the same poolDirectory every token is taken from, and names it. An account whose home in the user
// database can't be written, /var/empty in an integrate job's sandbox on Shackler (J-5xseq90), has no machine's pool to
// make, so the test skips there, naming the home.
func TestARunUnderAScratchHomeUsesTheMachinesPool(test *testing.T) {
	test.Parallel()
	account, currentError := user.Current()
	if currentError != nil {
		test.Fatal(currentError)
	}
	if poolError := poolCanBeMade(account.HomeDir); poolError != nil {
		test.Skipf("the account's home in the user database, %s, can't be written, so the machine's pool can't be "+
			"made there: %v", account.HomeDir, poolError)
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	scratch := scratchHome(test)
	status := exec.Command(wrapper, "status")
	status.Env = append(outsideThePool(os.Environ()), "HOME="+scratch)
	output, outputError := status.CombinedOutput()
	if outputError != nil {
		test.Fatalf("cohere-dev status: %v\n%s", outputError, output)
	}
	machine := filepath.Join(userCacheDirectoryIn(account.HomeDir), "cohere", "test-slots")
	if !strings.Contains(string(output), "("+machine+")") {
		test.Errorf("under a scratch home the pool is not the machine's, %s:\n%s", machine, output)
	}
	if _, statError := os.Stat(filepath.Join(userCacheDirectoryIn(scratch), "cohere")); !os.IsNotExist(statError) {
		test.Errorf("a scratch home grew a pool of its own (%v)", statError)
	}
}

// poolCanBeMade says why the machine's pool can't be made under home, nil when it can: it makes the pool's directory,
// as poolDirectory does for any run, so the answer is the run's own.
func poolCanBeMade(home string) error {
	return os.MkdirAll(filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots"), 0o755)
}

// The skip's condition: a home that can't be written can't hold the machine's pool, and one that can holds it.
func TestAPoolCanBeMadeOnlyUnderAHomeThatCanBeWritten(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("a directory's mode bits don't keep its owner from writing it on windows")
	}
	if os.Geteuid() == 0 {
		test.Skip("root writes a directory whatever its mode bits say")
	}
	readOnly := test.TempDir()
	if chmodError := os.Chmod(readOnly, 0o555); chmodError != nil {
		test.Fatal(chmodError)
	}
	// Writable again before the test's cleanup removes it.
	test.Cleanup(func() {
		if chmodError := os.Chmod(readOnly, 0o755); chmodError != nil {
			test.Error(chmodError)
		}
	})
	if poolError := poolCanBeMade(readOnly); poolError == nil {
		test.Errorf("a pool was made under the home %s, which can't be written", readOnly)
	}
	if poolError := poolCanBeMade(test.TempDir()); poolError != nil {
		test.Errorf("no pool was made under a home that can be written: %v", poolError)
	}
}

// A trim refuses a cache outside the user cache directory its pool lives in, since holding that pool keeps
// out none of the builds reading it: it removes nothing, and says so in its pool's log (#d0x1fhp). The setup
// is test's from 04:44: a scratch `HOME`, and a go cache passed in from elsewhere. With the cache beside the
// pool, the trim tests trim under the same scratch `HOME`, so this is the refusal's half of both ways.
func TestATrimRefusesACacheItsPoolDoesNotGuard(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the trim holds tokens, a Unix lock")
	}
	wrapper := filepath.Join(test.TempDir(), "cohere-dev")
	build_inputs.Record(test, ".", ".")
	if output, outputError := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); outputError != nil {
		test.Fatalf("building: %v\n%s", outputError, output)
	}
	home := test.TempDir()
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	if createDirectoryError := os.MkdirAll(slots, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	// Elsewhere: not under the pool home's user cache directory.
	cache := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(cache, "README"),
		[]byte("This directory holds cached build artifacts from the Go build system.\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	for index := 0; index < 10; index++ {
		path := filepath.Join(
			cache,
			fmt.Sprintf("%02x", index),
			fmt.Sprintf("%02x%s-d", index, strings.Repeat("a", 62)),
		)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, make([]byte, 1024), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
		old := time.Now().Add(-3 * time.Hour)
		if changeTimesError := os.Chtimes(path, old, old); changeTimesError != nil {
			test.Fatal(changeTimesError)
		}
	}
	scratch := scratchHome(test)
	trim := exec.Command(wrapper, trimCacheVerb)
	trim.Env = append(outsideThePool(os.Environ()), homeVariable+"="+home, "HOME="+scratch, "GOCACHE="+cache,
		tokensVariable+"=2", cacheCapVariable+"="+strconv.FormatFloat(6*1024.0/(1<<30), 'g', -1, 64))
	output, outputError := trim.CombinedOutput()
	if exited, ok := outputError.(*exec.ExitError); !ok || exited.ExitCode() != 2 {
		test.Errorf("the trim of a cache its pool does not guard did not exit 2 (%v):\n%s", outputError, output)
	}
	remaining := 0
	walkError := filepath.WalkDir(cache, func(path string, entry os.DirEntry, pathError error) error {
		if pathError == nil && !entry.IsDir() && entry.Name() != "README" {
			remaining++
		}
		return pathError
	})
	if walkError != nil {
		test.Fatalf("counting the cache's entries: %v", walkError)
	}
	if remaining != 10 {
		test.Errorf("the trim removed entries from a cache its pool does not guard: %d of 10 left", remaining)
	}
	log := readMaybeMissing(test, filepath.Join(slots, "gocache-trim.log"))
	if !strings.Contains(
		string(log),
		"not trimmed: the Go build cache "+cache+" is outside "+userCacheDirectoryIn(home),
	) {
		test.Errorf("the pool's log does not say why the cache was not trimmed:\n%s", log)
	}
	if _, statError := os.Stat(filepath.Join(userCacheDirectoryIn(scratch), "cohere")); !os.IsNotExist(statError) {
		test.Errorf("the trim made a pool under the scratch home (%v)", statError)
	}
}

// scratchHome is a home of nothing, for `HOME` in a run that must not follow it. Go's telemetry is off in it:
// a go command run beneath it counts into its home, and the background writer raced the test's cleanup
// ("directory not empty", one run in three).
func scratchHome(test *testing.T) string {
	test.Helper()
	home := test.TempDir()
	for _, configuration := range []string{
		filepath.Join(home, "Library", "Application Support"),
		filepath.Join(home, ".config"),
	} {
		telemetry := filepath.Join(configuration, "go", "telemetry")
		if createDirectoryError := os.MkdirAll(telemetry, 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off"), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return home
}
