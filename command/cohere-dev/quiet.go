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

// A quiet window switches the cache trim off (#cqfy7cv), and is a wall every heavy job meets (#1rh8dav).
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
//
// While a window is open no test, build, vet, exec or land starts, and no running job borrows an idle token
// (borrowTokens), so a measurement runs on a machine as quiet as the window promised. A broadcast could not
// hold that: a Circle's hands never hear one, and three times on 2026-11-11 a hand ran heavy work inside a
// hold its Circle had agreed to, so a pair of timings was rerun each time. The one exception is the run the
// window is for: `quiet on --through <name>` names it, and a job given `--through <name>` passes, and so does
// everything it starts, through quietThroughVariable. A job running when the window opened finishes as it
// was, since the wall is met when a job starts and again when it takes its token: what a running job starts
// beneath its token runs in its share. cohere-dev status names the jobs running inside an open window.

// quietFile is the marker's name in the pool directory.
const quietFile = "quiet"

// quietDefault is how long a quiet window lasts when --for does not say: longer than the windows held so
// far, which ran 10 to 45 minutes.
const quietDefault = time.Hour

// quietLongest is the longest a quiet window may be opened for.
const quietLongest = 4 * time.Hour

// quietThroughFlag names the run a job is, on `quiet on`, the one run the window lets through, and on a job,
// so the window can tell.
const quietThroughFlag = "--through"

// quietThroughVariable carries the name a job passed with quietThroughFlag to everything it starts, so the
// window lets those through too.
const quietThroughVariable = "COHERE_DEV_QUIET_THROUGH"

// quietRefusedExitCode is what a job a quiet window refused exits with: sysexits' EX_TEMPFAIL, try again later.
const quietRefusedExitCode = 75

// quietWindow is an open marker: when it ends, who opened it and why, and the run it lets through, if any.
type quietWindow struct {
	until   time.Time
	holder  string
	through string
}

func quietPath(directory string) string {
	return filepath.Join(directory, quietFile)
}

// readQuiet reads the marker, if there is one. Its first line is when it ends, its second who opened it, and a
// third, when there is one, the run it lets through.
func readQuiet(directory string) (quietWindow, bool, error) {
	contents, readError := os.ReadFile(quietPath(directory))
	if os.IsNotExist(readError) {
		return quietWindow{}, false, nil
	}
	if readError != nil {
		return quietWindow{}, false, readError
	}
	until, rest, _ := strings.Cut(strings.TrimSpace(string(contents)), "\n")
	holder, through, _ := strings.Cut(rest, "\n")
	ends, parseError := time.Parse(time.RFC3339, strings.TrimSpace(until))
	if parseError != nil {
		return quietWindow{}, false, fmt.Errorf(
			"the quiet window marker %s does not start with when it ends: %q",
			quietPath(directory),
			until,
		)
	}
	return quietWindow{
		until:   ends,
		holder:  strings.TrimSpace(holder),
		through: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(through), "through:")),
	}, true, nil
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
		return true, fmt.Sprintf(
			"a quiet window holds until %s (%s): %s",
			window.until.Format("15:04:05"),
			quietPath(directory),
			window.holder,
		)
	}
	return false, fmt.Sprintf(
		"a quiet window marker expired at %s and was passed over (%s): %s",
		window.until.Format("15:04:05"),
		quietPath(directory),
		window.holder,
	)
}

// quiet is `cohere-dev quiet on [--for <duration>] [--through <name>] [why]`, `quiet off`, `quiet wait`, and
// bare `quiet`, which says whether one is open.
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
	case "wait":
		return quietWait(directory, time.Second*5)
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
	through := ""
	reasonWords := arguments
	for len(reasonWords) > 0 && (reasonWords[0] == "--for" || reasonWords[0] == quietThroughFlag) {
		if len(reasonWords) < 2 {
			fmt.Fprintf(streams.Errors, "cohere-dev: %s wants a value: --for 30m, %s engine-pairs\n",
				reasonWords[0], quietThroughFlag)
			return 2
		}
		if reasonWords[0] == quietThroughFlag {
			through = strings.TrimSpace(reasonWords[1])
			if through == "" || strings.ContainsAny(through, " \t\n") {
				fmt.Fprintf(streams.Errors, "cohere-dev: %s is %q, want one word naming the run, like engine-pairs\n",
					quietThroughFlag, reasonWords[1])
				return 2
			}
			reasonWords = reasonWords[2:]
			continue
		}
		parsed, parseDurationError := time.ParseDuration(reasonWords[1])
		if parseDurationError != nil || parsed <= 0 || parsed > quietLongest {
			fmt.Fprintf(
				streams.Errors,
				"cohere-dev: --for is %q, want a duration above 0 and at most %s, like 30m\n",
				reasonWords[1],
				quietLongest,
			)
			return 2
		}
		length = parsed
		reasonWords = reasonWords[2:]
	}
	why := strings.Join(reasonWords, " ")
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
	marker := until.Format(time.RFC3339) + "\n" + holder + "\n"
	if through != "" {
		marker += "through: " + through + "\n"
	}
	if writeError := os.WriteFile(temporary, []byte(marker), 0o644); writeError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: opening the quiet window: %v\n", writeError)
		return 1
	}
	if renameError := os.Rename(temporary, quietPath(directory)); renameError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: opening the quiet window: %v\n", renameError)
		return 1
	}
	lets := "no job starts"
	if through != "" {
		lets = "no job starts but one run with " + quietThroughFlag + " " + through
	}
	fmt.Fprintf(
		streams.Output,
		"quiet window open until %s: %s, no token is borrowed, and the Go build cache trim holds off; "+
			"release it with cohere-dev quiet off\n",
		until.Format("15:04:05"),
		lets,
	)
	return 0
}

// quietRefusal is why a quiet window refuses a job starting now, empty when it lets the job start: no window
// is open, or the job runs beneath a token, inside a running job's share (heldToken, from heldTokenVariable),
// or it names the run the window lets through (through, from quietThroughVariable, which run sets from
// quietThroughFlag). A job let through says so.
func quietRefusal(directory string, now time.Time, heldToken string, through string) string {
	if heldToken != "" {
		return ""
	}
	holds, line := quietHolds(directory, now)
	if !holds {
		return ""
	}
	// Ignored: a marker that can't be read already holds, and its line says what is wrong; it names no run to
	// let through.
	window, _, _ := readQuiet(directory)
	if window.through != "" && through == window.through {
		fmt.Fprintf(streams.Errors, "cohere-dev: let through the quiet window, the run it is for (%s %s)\n",
			quietThroughFlag, through)
		return ""
	}
	lets := "it lets no run through"
	if window.through != "" {
		lets = "it lets through only the run it is for, " + quietThroughFlag + " " + window.through
	}
	return fmt.Sprintf("%s, so no test, build, vet, exec or land starts until it closes, and %s. "+
		"Wait for it with go run ./command/cohere-dev quiet wait, then run again", line, lets)
}

// withoutThroughFlag takes quietThroughFlag and its name out of a job's arguments, so it never reaches go, and
// returns the name, empty when there is none. leading reads only the flags before exec's command, whose own
// flags are its own; reading stops at "--" or go test's -args either way.
func withoutThroughFlag(arguments []string, leading bool) ([]string, string, error) {
	kept := make([]string, 0, len(arguments))
	through := ""
	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]
		if argument == "--" || argument == "-args" || argument == "--args" {
			return append(kept, arguments[index:]...), through, nil
		}
		name, value, hasValue := strings.Cut(argument, "=")
		if name != quietThroughFlag {
			if leading && !strings.HasPrefix(argument, "--") {
				return append(kept, arguments[index:]...), through, nil
			}
			kept = append(kept, argument)
			continue
		}
		if !hasValue {
			if index+1 == len(arguments) {
				return nil, "", fmt.Errorf("%s needs the name of the run a quiet window lets through", quietThroughFlag)
			}
			index++
			value = arguments[index]
		}
		through = value
	}
	return kept, through, nil
}

// quietWait waits until no quiet window holds, looking every interval, and says what it waited for.
func quietWait(directory string, interval time.Duration) int {
	waited := false
	for {
		holds, line := quietHolds(directory, time.Now())
		if !holds {
			if waited {
				fmt.Fprintln(streams.Output, "cohere-dev: the quiet window closed")
			}
			return 0
		}
		if !waited {
			fmt.Fprintf(streams.Errors, "cohere-dev: waiting: %s\n", line)
			// A person or a hand waits on this line, so it is delivered before the wait.
			streams.Show(streams.Errors)
			waited = true
		}
		time.Sleep(interval)
	}
}

// quietStatus is the quiet window's line in cohere-dev status.
func quietStatus(directory string, now time.Time) string {
	holds, line := quietHolds(directory, now)
	switch {
	case holds:
		lets := "no job starts"
		// Ignored: a marker that can't be read holds, and its line already says what is wrong with it.
		if window, _, _ := readQuiet(directory); window.through != "" {
			lets = "no job starts but " + quietThroughFlag + " " + window.through
		}
		return "quiet window: open, " + lets + ", no token is borrowed, the cache trim holds off; " + line
	case line != "":
		return "quiet window: none open; " + line
	}
	return "quiet window: none open"
}
