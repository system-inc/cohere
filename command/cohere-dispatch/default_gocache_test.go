package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A run of the launcher in a checkout starts the background trim of the default Go build cache, and the
// trim reaches the cache the caller's `GOCACHE` names (#3sgjy0h). The cache here is not one Go made, so
// the trim refuses it, and the refusal in the prune log is the proof that the copy started, found the
// module and asked the go command for its cache. The run itself fails afterwards, having no commit to
// build, which the trim never waits on.
func TestARunStartsTheDefaultGoCacheTrim(t *testing.T) {
	binaries := t.TempDir()
	dispatcher := filepath.Join(binaries, "cohere-dispatch")
	if combined, err := exec.Command("go", "build", "-o", dispatcher, ".").CombinedOutput(); err != nil {
		t.Fatalf("building the launcher: %v\n%s", err, combined)
	}

	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module github.com/system-inc/cohere\n\ngo 1.27\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cacheDirectory := filepath.Join(module, ".cache", "cohere")
	if err := os.MkdirAll(cacheDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	notACache := t.TempDir()

	run := exec.Command(dispatcher, "--version")
	run.Dir = module
	run.Env = append(os.Environ(), "GOCACHE="+notACache, "COHERE_BINARY=", "CI=", "COHERE_WAIT=")
	output, err := run.CombinedOutput()
	if err == nil {
		t.Fatalf("a run with no commit to build succeeded, so this module is not what the test assumes:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(cacheDirectory, "default-gocache.checked")); err != nil {
		t.Fatalf("the run did not claim the default cache check: %v", err)
	}

	pruneLog := filepath.Join(cacheDirectory, "prune.log")
	want := "the default Go build cache was not bounded: refusing to trim " + notACache
	deadline := time.Now().Add(30 * time.Second)
	for {
		log, _ := os.ReadFile(pruneLog)
		if strings.Contains(string(log), want) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background trim never reported on %s; the prune log holds %q", notACache, log)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Within the interval a second run starts no second trim.
	stamp, err := os.Stat(filepath.Join(cacheDirectory, "default-gocache.checked"))
	if err != nil {
		t.Fatal(err)
	}
	again := exec.Command(dispatcher, "--version")
	again.Dir = module
	again.Env = run.Env
	again.CombinedOutput()
	if restamped, err := os.Stat(filepath.Join(cacheDirectory, "default-gocache.checked")); err != nil || !restamped.ModTime().Equal(stamp.ModTime()) {
		t.Fatalf("a second run within the interval claimed the check again: %v", err)
	}
}
