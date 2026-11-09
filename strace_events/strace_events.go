// Package strace_events reads strace's text output, line by line, into the calls a build cache keys
// on, written once for the two repositories that trace a build to learn what it read: Loom's planner
// and Adamic's buildcache. Each turned strace's lines into calls on its own, Loom with regular
// expressions and Adamic by hand, and where the two read a line differently one of them keyed a path
// that isn't the file on disk, or missed a read.
//
// It reads strace as strace writes it, never as either consumer happened to: a line's pid in any of
// the three forms strace prints, a call's arguments up to the parenthesis that closes them outside
// every quoted string, a result that is a number or strace's unknown `?`, a call strace split around
// another process's joined back whole, and every quoted string and decoded descriptor path both as
// strace spells it and as the bytes it names. Where a consumer read a line otherwise, the difference
// is on a ledger in this module's tests, line by line, with what it changes for that consumer.
//
// It holds none of either consumer's policy: which calls count as reads, which flags make an open a
// write, how a relative path resolves against a working directory, what a path ending in
// " (deleted)" means. Each consumer brings its own, over the calls this package reads. It is a module
// of its own, beside static_single_assignment and git_tree, so Loom and Adamic can require it without
// requiring cohere's root module.
package strace_events

import "strings"

// LineKind is what one line of a trace is.
type LineKind uint8

const (
	// OtherLine is a line this package doesn't read: blank, garbage, or a form strace prints that
	// names no call, process end or signal.
	OtherLine LineKind = iota
	// CallLine is a call strace printed whole: its name, arguments and result.
	CallLine
	// UnfinishedLine is the first half of a call strace split because another process's call came
	// between: `openat(AT_FDCWD, "x", O_RDONLY <unfinished ...>`.
	UnfinishedLine
	// ResumedLine is the second half of a split call: `<... openat resumed>) = 3`.
	ResumedLine
	// ExitLine is a process's end: `+++ exited with 0 +++` or `+++ killed by SIGKILL +++`.
	ExitLine
	// SignalLine is a signal delivered to a process: `--- SIGCHLD {si_signo=SIGCHLD, ...} ---`.
	SignalLine
)

// A Line is one line of a trace, read.
type Line struct {
	Kind LineKind
	// Pid is the process the line is from, its digits as strace printed them, from `123 ` or
	// `[pid 123] `. It is "" for a line with none, which strace prints for the traced process while it
	// is the only one and its output isn't a file.
	Pid string
	// Text is the line after its pid, without the whitespace around it.
	Text string
	// Name is the call a CallLine, UnfinishedLine or ResumedLine names, or "" when an unfinished
	// line's text starts with no call.
	Name string
	// Call is a CallLine's call.
	Call Call
	// Started is an UnfinishedLine's text before its marker, which a ResumedLine's Resumed completes.
	Started string
	// Resumed is what follows a ResumedLine's marker, its leading whitespace dropped.
	Resumed string
	// Exit is an ExitLine's end.
	Exit Exit
	// Signal is a SignalLine's text between its `---` markers.
	Signal string
}

// An Exit is how a process ended: Status is the digits of `exited with <status>`, or Signal the
// signal of `killed by <signal>`, with CoreDumped when strace added `(core dumped)`.
type Exit struct {
	Status     string
	Signal     string
	CoreDumped bool
}

// The markers strace splits a call with.
const (
	unfinishedMarker = "<unfinished ...>"
	resumedOpening   = "<... "
	resumedClosing   = " resumed>"
)

// ParseLine reads one line of strace's output. A line no form matches is an OtherLine, never an
// error: a trace holds lines no consumer reads, and a consumer that wants to know about one asks for
// its Kind.
func ParseLine(text string) Line {
	// Whitespace around a line is no part of it: strace prints none, and a trace copied through a
	// terminal or a log can gain some.
	pid, rest := splitPid(strings.TrimSpace(text))
	line := Line{Pid: pid, Text: rest}
	if exit, isExit := parseExit(rest); isExit {
		line.Kind, line.Exit = ExitLine, exit
		return line
	}
	if signal, isSignal := parseSignal(rest); isSignal {
		line.Kind, line.Signal = SignalLine, signal
		return line
	}
	if started, isUnfinished := strings.CutSuffix(rest, unfinishedMarker); isUnfinished {
		// strace puts one space between the call's text and the marker. Any more is the call's own: a
		// call split right after an argument's comma is `read(3,  <unfinished ...>`, and its resumption
		// `<... read resumed>"text", 4) = 4` reads back as `read(3, "text", 4) = 4`.
		if started != "" && isSpace(started[len(started)-1]) {
			started = started[:len(started)-1]
		}
		line.Kind, line.Started, line.Name = UnfinishedLine, started, callName(started)
		return line
	}
	if name, resumed, isResumed := parseResumed(rest); isResumed {
		line.Kind, line.Name, line.Resumed = ResumedLine, name, resumed
		return line
	}
	if call, isCall := parseCall(rest); isCall {
		call.Pid = pid
		line.Kind, line.Name, line.Call = CallLine, call.Name, call
	}
	return line
}

// splitPid is a line's pid and the rest of it. strace prints `123 ` before every line of a trace it
// writes to a file with -f, `[pid 123] ` before a line to its terminal once a second process is
// traced, and nothing before the first process's lines until then.
func splitPid(text string) (string, string) {
	if bracketed, isBracketed := strings.CutPrefix(text, "[pid"); isBracketed {
		spaces := leading(bracketed, isSpace)
		digits := leading(bracketed[spaces:], isDigit)
		after := bracketed[spaces+digits:]
		if spaces == 0 || digits == 0 || !strings.HasPrefix(after, "]") {
			return "", text
		}
		gap := leading(after[1:], isSpace)
		if gap == 0 {
			return "", text
		}
		return bracketed[spaces : spaces+digits], after[1+gap:]
	}
	digits := leading(text, isDigit)
	if digits == 0 {
		return "", text
	}
	gap := leading(text[digits:], isSpace)
	if gap == 0 {
		return "", text
	}
	return text[:digits], text[digits+gap:]
}

// parseExit reads `+++ exited with <digits> +++` and `+++ killed by <signal> +++`, the signal
// followed by ` (core dumped)` when it dumped one.
func parseExit(text string) (Exit, bool) {
	middle, opened := strings.CutPrefix(text, "+++ ")
	if !opened {
		return Exit{}, false
	}
	middle, closed := strings.CutSuffix(middle, " +++")
	if !closed {
		return Exit{}, false
	}
	if status, exited := strings.CutPrefix(middle, "exited with "); exited {
		if status == "" || leading(status, isDigit) != len(status) {
			return Exit{}, false
		}
		return Exit{Status: status}, true
	}
	if signal, killed := strings.CutPrefix(middle, "killed by "); killed {
		signal, dumped := strings.CutSuffix(signal, " (core dumped)")
		if signal == "" || leading(signal, isWord) != len(signal) {
			return Exit{}, false
		}
		return Exit{Signal: signal, CoreDumped: dumped}, true
	}
	return Exit{}, false
}

// parseSignal reads `--- <signal> ... ---`, returning what lies between the markers.
func parseSignal(text string) (string, bool) {
	middle, opened := strings.CutPrefix(text, "--- ")
	if !opened {
		return "", false
	}
	middle, closed := strings.CutSuffix(middle, " ---")
	if !closed || middle == "" {
		return "", false
	}
	return middle, true
}

// parseResumed reads `<... <name> resumed>` and what follows it, whose leading whitespace is no part
// of the call: an older strace printed a space after the marker, and joining it would put a space
// inside the call's arguments.
func parseResumed(text string) (string, string, bool) {
	tail, opened := strings.CutPrefix(text, resumedOpening)
	if !opened {
		return "", "", false
	}
	name := leading(tail, isWord)
	if name == 0 {
		return "", "", false
	}
	resumed, closed := strings.CutPrefix(tail[name:], resumedClosing)
	if !closed {
		return "", "", false
	}
	return tail[:name], resumed[leading(resumed, isSpace):], true
}

// callName is the call a text starts with, `openat` of `openat(AT_FDCWD, ...`, or "".
func callName(text string) string {
	name := leading(text, isWord)
	if name == 0 || name == len(text) || text[name] != '(' {
		return ""
	}
	return text[:name]
}

func isDigit(character byte) bool {
	return '0' <= character && character <= '9'
}

// isWord is a byte of a call's or a signal's name: an ASCII letter, a digit or an underscore.
func isWord(character byte) bool {
	return isDigit(character) || 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' ||
		character == '_'
}

// isSpace is the whitespace strace separates a line's parts with: a space, tab, newline, form feed or
// carriage return.
func isSpace(character byte) bool {
	return character == ' ' || character == '\t' || character == '\n' || character == '\f' || character == '\r'
}

// leading is how many of text's first bytes are in class.
func leading(text string, class func(byte) bool) int {
	count := 0
	for count < len(text) && class(text[count]) {
		count++
	}
	return count
}
