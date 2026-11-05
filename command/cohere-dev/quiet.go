package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// A quiet window switches the cache trim off (#cqfy7cv).
//
// Whoever holds a quiet window for a measurement opens it with `cohere-dev quiet on` and releases it with
// `cohere-dev quiet off`. Between the two the background trim (cache.go) does nothing: it neither measures
// the cache, which walks every entry, nor holds the pool and removes tens of thousands of files, which took
// the disk for 2 to 5s at a time and could land right when a window's first cold run started (2026-10-05).
// It says so in its log, naming the marker, so a measurement can show no trim ran inside it.
//
// The marker is a file beside the pool, and it ends on its own. A holder whose session dies before it says
// off would otherwise switch trimming off for good, and a cache with no trim refilled the disk at a hundred
// gigabytes an hour; a measurement taken a little after its window expired costs far less than that. So
// the marker records when it ends, quietDefault from when it was opened unless --for says otherwise, and
// never more than quietLongest. An expired marker is ignored, and the log and cohere-dev status say so.

// quietFile is the marker's name in the pool directory.
const quietFile = "quiet"

// quietDefault is how long a quiet window lasts when --for does not say: longer than the windows held so
// far, which ran 10 to 45 minutes.
const quietDefault = time.Hour

// quietLongest is the longest a quiet window may be opened for.
const quietLongest = 4 * time.Hour

// quietWindow is an open marker: when it ends, and who opened it and why.
type quietWindow struct {
	until  time.Time
	holder string
}

func quietPath(directory string) string {
	return filepath.Join(directory, quietFile)
}

// readQuiet reads the marker, if there is one. Its first line is when it ends, and its second who opened it.
func readQuiet(directory string) (quietWindow, bool, error) {
	contents, err := os.ReadFile(quietPath(directory))
	if os.IsNotExist(err) {
		return quietWindow{}, false, nil
	}
	if err != nil {
		return quietWindow{}, false, err
	}
	until, holder, _ := strings.Cut(strings.TrimSpace(string(contents)), "\n")
	ends, err := time.Parse(time.RFC3339, strings.TrimSpace(until))
	if err != nil {
		return quietWindow{}, false, fmt.Errorf("the quiet window marker %s does not start with when it ends: %q", quietPath(directory), until)
	}
	return quietWindow{until: ends, holder: strings.TrimSpace(holder)}, true, nil
}

// quietHolds reports whether a quiet window holds now, and describes the marker either way, for the trim's
// log: why it held back, or that an expired marker was passed over. A marker that cannot be read holds,
// since someone meant to open a window, and the line says what is wrong with it.
func quietHolds(directory string, now time.Time) (bool, string) {
	window, present, readQuietError := readQuiet(directory)
	if readQuietError != nil {
		return true, readQuietError.Error() + "; treated as open until it is fixed or removed with cohere-dev quiet off"
	}
	if !present {
		return false, ""
	}
	if now.Before(window.until) {
		return true, fmt.Sprintf("a quiet window holds until %s (%s): %s", window.until.Format("15:04:05"), quietPath(directory), window.holder)
	}
	return false, fmt.Sprintf("a quiet window marker expired at %s and was passed over (%s): %s", window.until.Format("15:04:05"),
		quietPath(directory), window.holder)
}

// quiet is `cohere-dev quiet on [--for <duration>] [why]`, `quiet off`, and bare `quiet`, which says whether
// one is open.
func quiet(arguments []string) int {
	directory, poolDirectoryError := poolDirectory()
	if poolDirectoryError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", poolDirectoryError)
		return 1
	}
	if len(arguments) == 0 {
		fmt.Fprintln(streams.Output, quietStatus(directory, time.Now()))
		return 0
	}
	switch arguments[0] {
	case "on":
		return quietOn(directory, arguments[1:], time.Now())
	case "off":
		return quietOff(directory, streams.Output)
	}
	return usage()
}

// quietOff removes the marker, and says to output what it released: the window it was, a marker that could
// not be read, which off removes all the same, or that none was open.
func quietOff(directory string, output *bufio.Writer) int {
	window, present, readQuietError := readQuiet(directory)
	if removeError := os.Remove(quietPath(directory)); removeError != nil && !os.IsNotExist(removeError) {
		fmt.Fprintf(streams.Errors, "cohere-dev: releasing the quiet window: %v\n", removeError)
		return 1
	}
	switch {
	case readQuietError != nil:
		fmt.Fprintf(output, "quiet window released; its marker could not be read: %v\n", readQuietError)
	case present:
		fmt.Fprintf(output, "quiet window released; it was %s\n", window.holder)
	default:
		fmt.Fprintln(output, "no quiet window was open")
	}
	return 0
}

func quietOn(directory string, arguments []string, now time.Time) int {
	length := quietDefault
	if len(arguments) > 0 && arguments[0] == "--for" {
		if len(arguments) < 2 {
			fmt.Fprintf(streams.Errors, "cohere-dev: --for wants a duration, like 30m\n")
			return 2
		}
		parsed, parseDurationError := time.ParseDuration(arguments[1])
		if parseDurationError != nil || parsed <= 0 || parsed > quietLongest {
			fmt.Fprintf(streams.Errors, "cohere-dev: --for is %q, want a duration above 0 and at most %s, like 30m\n", arguments[1], quietLongest)
			return 2
		}
		length = parsed
		arguments = arguments[2:]
	}
	why := strings.Join(arguments, " ")
	if why == "" {
		why = "no reason given"
	}
	until := now.Add(length)
	// No pid: this process ends as soon as the marker is written, and so does the go run that started it.
	// Ignored: where it was opened only tells a reader of the marker; a window whose directory can't be read
	// still holds, and still says when it ends and why.
	working, _ := os.Getwd()
	holder := fmt.Sprintf("opened in %s at %s: %s", working, now.Format("15:04:05"), why)
	temporary := filepath.Join(directory, "."+quietFile)
	if writeError := os.WriteFile(temporary, []byte(until.Format(time.RFC3339)+"\n"+holder+"\n"), 0o644); writeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: opening the quiet window: %v\n", writeError)
		return 1
	}
	if renameError := os.Rename(temporary, quietPath(directory)); renameError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: opening the quiet window: %v\n", renameError)
		return 1
	}
	fmt.Fprintf(streams.Output, "quiet window open until %s: the Go build cache trim holds off; release it with cohere-dev quiet off\n",
		until.Format("15:04:05"))
	return 0
}

// quietStatus is the quiet window's line in cohere-dev status.
func quietStatus(directory string, now time.Time) string {
	holds, line := quietHolds(directory, now)
	switch {
	case holds:
		return "quiet window: open, the cache trim holds off; " + line
	case line != "":
		return "quiet window: none open; " + line
	}
	return "quiet window: none open"
}
