package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/build_inputs"
)

// A quiet window holds until it ends and not after, so a holder that never says off cannot switch the trim
// off for good. A marker that cannot be read holds, and a --for past the longest is refused.
func TestAQuietWindowHoldsUntilItEnds(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	opened := time.Date(2026, 10, 6, 4, 0, 0, 0, time.Local)
	if holds, line := quietHolds(directory, opened); holds || line != "" {
		test.Fatalf("no marker held, or was described: %q", line)
	}
	if code := quietOn(
		directory,
		[]string{"--for", "30m", "a cold measurement"},
		opened,
		discarded(),
		discarded(),
	); code != 0 {
		test.Fatalf("opening a quiet window exited %d", code)
	}
	if holds, line := quietHolds(directory, opened.Add(29*time.Minute)); !holds ||
		!strings.Contains(line, "holds until 04:30:00") || !strings.Contains(line, "a cold measurement") {
		test.Errorf("29 minutes into a 30 minute window it does not hold, or does not say why: %q", line)
	}
	if holds, line := quietHolds(
		directory,
		opened.Add(31*time.Minute),
	); holds || !strings.Contains(line, "expired at 04:30:00") {
		test.Errorf("a minute after a 30 minute window ended it holds, or does not say it expired: %q", line)
	}
	if code := quietOn(directory, []string{"--for"}, opened, discarded(), discarded()); code != 2 {
		test.Errorf("a --for with no duration was not refused: exited %d", code)
	}
	if code := quietOn(directory, []string{"--for", "5h"}, opened, discarded(), discarded()); code != 2 {
		test.Errorf("a five hour window was not refused: exited %d", code)
	}
	if writeError := os.WriteFile(quietPath(directory), []byte("tomorrow\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if holds, line := quietHolds(
		directory,
		opened,
	); !holds || !strings.Contains(line, "does not start with when it ends") {
		test.Errorf("an unreadable marker does not hold, or does not say what is wrong: %q", line)
	}
}

// quiet off removes a marker it can't read, as it removes any, and says so, rather than that no window was
// open.
func TestQuietOffReleasesAMarkerItCannotRead(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	if writeError := os.WriteFile(quietPath(directory), []byte("tomorrow\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	var said bytes.Buffer
	output := bufio.NewWriter(&said)
	if code := quietOff(directory, output, discarded()); code != 0 {
		test.Fatalf("quiet off exited %d", code)
	}
	if flushError := output.Flush(); flushError != nil {
		test.Fatal(flushError)
	}
	if !strings.HasPrefix(said.String(), "quiet window released; its marker could not be read: ") {
		test.Errorf("quiet off does not say it released a marker it could not read: %q", said.String())
	}
	if _, statError := os.Stat(quietPath(directory)); !errors.Is(statError, fs.ErrNotExist) {
		test.Errorf("quiet off left the marker (%v)", statError)
	}
}

// quietTrimCase is a cohere-dev built for a test, a pool home of its own, and a Go build cache of ten 1 KB
// entries three hours old under a 6 KB cap, so a trim that runs removes some and one held off removes none.
type quietTrimCase struct {
	wrapper, slots, cache string
	environment           []string
}

func newQuietTrimCase(test *testing.T) quietTrimCase {
	test.Helper()
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
	// The cache in the pool home's user cache directory, where the trim guards it (poolGuards).
	cache := filepath.Join(userCacheDirectoryIn(home), "go-build")
	if createDirectoryError := os.MkdirAll(cache, 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
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
	environment := append(outsideThePool(os.Environ()), homeVariable+"="+home, "HOME="+scratchHome(test),
		"GOCACHE="+cache, tokensVariable+"=2", cacheCapVariable+"="+strconv.FormatFloat(6*1024.0/(1<<30), 'g', -1, 64))
	return quietTrimCase{wrapper: wrapper, slots: slots, cache: cache, environment: environment}
}

func (trimCase quietTrimCase) run(test *testing.T, arguments ...string) string {
	test.Helper()
	command := exec.Command(trimCase.wrapper, arguments...)
	command.Env = trimCase.environment
	output, outputError := command.CombinedOutput()
	if outputError != nil {
		test.Fatalf("cohere-dev %s: %v\n%s", strings.Join(arguments, " "), outputError, output)
	}
	return string(output)
}

func (trimCase quietTrimCase) size(test *testing.T) int64 {
	test.Helper()
	return cacheSize(test, trimCase.cache)
}

func (trimCase quietTrimCase) log(test *testing.T) string {
	test.Helper()
	return string(readMaybeMissing(test, filepath.Join(trimCase.slots, "gocache-trim.log")))
}

// A quiet window switches the trim off, both ways (#cqfy7cv): with the marker open a trim over its cap
// removes nothing, not even looking, and its log names the window and the marker; once the window is
// released the same trim runs.
func TestAQuietWindowSwitchesTheTrimOff(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the trim holds tokens, a Unix lock")
	}
	trimCase := newQuietTrimCase(test)

	if output := trimCase.run(
		test,
		"quiet",
		"on",
		"--for",
		"10m",
		"cache's cold runs",
	); !strings.Contains(output, "quiet window open until") {
		test.Errorf("quiet on does not say the window is open:\n%s", output)
	}
	if output := trimCase.run(
		test,
		"status",
	); !strings.Contains(output, "quiet window: open") || !strings.Contains(output, "cache's cold runs") {
		test.Errorf("cohere-dev status does not show the open window:\n%s", output)
	}
	trimCase.run(test, trimCacheVerb)
	if remaining := trimCase.size(test); remaining != 10*1024 {
		test.Errorf("the trim removed entries inside a quiet window: %d bytes left of 10 KB", remaining)
	}
	if log := trimCase.log(test); !strings.Contains(log, "not looked at: a quiet window holds until") ||
		!strings.Contains(log, quietPath(trimCase.slots)) || !strings.Contains(log, "cache's cold runs") {
		test.Errorf("the trim's log does not name the window it held off for:\n%s", log)
	}

	if output := trimCase.run(test, "quiet", "off"); !strings.Contains(output, "quiet window released") {
		test.Errorf("quiet off does not say the window was released:\n%s", output)
	}
	trimCase.run(test, trimCacheVerb)
	if remaining := trimCase.size(test); remaining > 6*1024/4*3 {
		test.Errorf("with the window released the trim left %d bytes, over three quarters of the 6 KB cap", remaining)
	}
	if log := trimCase.log(test); !strings.Contains(log, "trimmed from") {
		test.Errorf("with the window released the trim did not trim:\n%s", log)
	}
}

// A window opened while the trim waits for its token holds too: the trim has already measured, and it looks
// again once it holds its token, before it removes anything.
func TestAQuietWindowOpenedWhileTheTrimWaitsHoldsToo(test *testing.T) {
	test.Parallel()
	if runtime.GOOS == "windows" {
		test.Skip("the trim holds tokens, a Unix lock")
	}
	trimCase := newQuietTrimCase(test)

	// Both tokens held, so the trim waits its turn for one.
	held, takeSlotError := takeSlot(trimCase.slots, 2, "a run that holds a token")
	if takeSlotError != nil || held == nil {
		test.Fatalf("taking a token for the test: %v", takeSlotError)
	}
	other, takeSlotError := takeSlot(trimCase.slots, 2, "another run that holds a token")
	if takeSlotError != nil || other == nil {
		held.release()
		test.Fatalf("taking the other token for the test: %v", takeSlotError)
	}
	trim := exec.Command(trimCase.wrapper, trimCacheVerb)
	trim.Env = trimCase.environment
	if startError := trim.Start(); startError != nil {
		held.release()
		other.release()
		test.Fatal(startError)
	}
	// Measuring 10 KB takes milliseconds; two seconds puts the trim well into its wait for a token.
	time.Sleep(2 * time.Second)
	trimCase.run(test, "quiet", "on", "--for", "10m", "lint_rules' pairs")
	held.release()
	other.release()
	if waitError := trim.Wait(); waitError != nil {
		test.Fatalf("the trim failed: %v", waitError)
	}
	if remaining := trimCase.size(test); remaining != 10*1024 {
		test.Errorf("a window opened while the trim waited did not hold it: %d bytes left of 10 KB", remaining)
	}
	log := trimCase.log(test)
	if strings.Contains(log, "not looked at") {
		test.Fatalf(
			"the trim had not reached its wait for a token in two seconds, so this run tells nothing:\n%s",
			log,
		)
	}
	if !strings.Contains(log, "cap, not trimmed: a quiet window holds until") ||
		!strings.Contains(log, "lint_rules' pairs") {
		test.Errorf("the trim's log does not say the window stopped it after it took its token:\n%s", log)
	}
}

// discarded is a stream that keeps nothing, one per call, for code under test that writes to the streams it is
// handed: the package-wide ones are written only from one goroutine, and these tests run side by side.
func discarded() *bufio.Writer {
	return bufio.NewWriter(io.Discard)
}
