package strace_events_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Loom's strace parser, copied from loom's origin/release-0.8.1184 at 9352c88758ab (fetched Nov 9,
// 2026), read with `git show` and never checked out. From planner/trace.go (last changed in
// 384011f33531, Nov 7): its expressions (lines 33 to 39, 514 and 517), quoted (633 to 650),
// nextArgument (445 to 447) and traceEvents' loop body (459 to 495). From ebpftrace/format.go (last
// changed in 0ff93bc2b799, Oct 25): Unquote (28 to 33), UnquoteDescriptor (37 to 39), letterEscapes
// (42) and unescape (50 to 90), through which trace.go decodes every quoted path and every decoded
// descriptor path, the AT_FDCWD it reads a working directory from and the opened path included.
//
// It is the oracle the differential holds this package against, so it is kept as Loom wrote it,
// regular expressions and all. Its errors' names are spelled out, as cohere's lint asks, and its loop
// body runs one line at a time, so a corpus of any size streams through it beside this package. It
// leaves out where a fork is placed: Loom puts a split fork's call where it started, which is Loom's
// order over the calls, kept in Loom over the Joiner, and never which calls a trace holds.
var (
	loomTraceCall  = regexp.MustCompile(`^(?:\[pid\s+(\d+)\]\s+|(\d+)\s+)?([a-z0-9_]+)\((.*)\)\s+=\s+(-?\d+)`)
	loomUnfinished = regexp.MustCompile(`^(?:\[pid\s+(\d+)\]\s+|(\d+)\s+)?(.*)\s<unfinished \.\.\.>$`)
	loomResumed    = regexp.MustCompile(`^(?:\[pid\s+(\d+)\]\s+|(\d+)\s+)?<\.\.\. (\w+) resumed>(.*)$`)
	loomOpenedPath = regexp.MustCompile(`\)\s+=\s+\d+<(.+)>$`)
	loomDescriptor = regexp.MustCompile(`^(?:AT_FDCWD|\d+)(?:<(.*?)>)?,\s*`)
)

// A loomEvent is Loom's traceEvent: one finished call of one process.
type loomEvent struct {
	pid, call, arguments, line string
	result                     int
}

// loomParser is traceEvents' state: the call each pid left unfinished.
type loomParser struct {
	pending map[string]string
}

// line is traceEvents' loop body over one line, the event it finishes and whether it finishes one.
func (parser *loomParser) line(text string) (loomEvent, bool) {
	if parser.pending == nil {
		parser.pending = map[string]string{}
	}
	line := strings.TrimSpace(text)
	if match := loomUnfinished.FindStringSubmatch(line); match != nil {
		parser.pending[match[1]+match[2]] = match[3]
		return loomEvent{}, false
	}
	if match := loomResumed.FindStringSubmatch(line); match != nil {
		start, found := parser.pending[match[1]+match[2]]
		if !found {
			return loomEvent{}, false
		}
		delete(parser.pending, match[1]+match[2])
		line = strings.TrimSpace(match[1] + match[2] + " " + start + match[4])
	}
	match := loomTraceCall.FindStringSubmatch(line)
	if match == nil {
		return loomEvent{}, false
	}
	result, atoiError := strconv.Atoi(match[5])
	if atoiError != nil {
		return loomEvent{}, false
	}
	return loomEvent{pid: match[1] + match[2], call: match[3], arguments: match[4], line: line, result: result}, true
}

// loomQuoted is Loom's quoted: strace's leading C string argument, and what follows it.
func loomQuoted(arguments string) (string, string, error) {
	if !strings.HasPrefix(arguments, `"`) {
		return "", "", fmt.Errorf("no path argument")
	}
	for index := 1; index < len(arguments); index++ {
		switch arguments[index] {
		case '\\':
			index++
		case '"':
			name, unquoteError := loomUnquote(arguments[:index+1])
			if unquoteError != nil {
				return "", "", fmt.Errorf("path %s: %w", arguments[:index+1], unquoteError)
			}
			return name, arguments[index+1:], nil
		}
	}
	return "", "", fmt.Errorf("unterminated path")
}

// loomNextArgument is Loom's nextArgument.
func loomNextArgument(rest string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(rest), ","))
}

// loomUnquote is ebpftrace.Unquote: a quoted string's bytes.
func loomUnquote(quoted string) (string, error) {
	if len(quoted) < 2 || quoted[0] != '"' || quoted[len(quoted)-1] != '"' {
		return "", fmt.Errorf("%s isn't a quoted string", quoted)
	}
	return loomUnescape(quoted[1:len(quoted)-1], `"`)
}

// loomUnquoteDescriptor is ebpftrace.UnquoteDescriptor: a decoded descriptor path's bytes.
func loomUnquoteDescriptor(text string) (string, error) {
	return loomUnescape(text, `"<>`)
}

// loomLetterEscapes is ebpftrace's letterEscapes.
var loomLetterEscapes = map[byte]byte{
	'"': '"', '\\': '\\', 'n': '\n', 't': '\t', 'r': '\r', 'f': '\f', 'v': '\v', 'a': '\a', 'b': '\b',
}

// loomUnescape is ebpftrace's unescape, which refuses a byte strace never writes bare, a backslash
// ending the text, and an escape strace never writes.
func loomUnescape(text, extra string) (string, error) {
	decoded := make([]byte, 0, len(text))
	for index := 0; index < len(text); index++ {
		character := text[index]
		if character != '\\' {
			if character < ' ' || character > '~' || strings.IndexByte(extra, character) >= 0 {
				return "", fmt.Errorf("%q holds the byte %#o bare, which is always escaped", text, character)
			}
			decoded = append(decoded, character)
			continue
		}
		index++
		if index == len(text) {
			return "", fmt.Errorf("%q ends in a backslash", text)
		}
		if letter, found := loomLetterEscapes[text[index]]; found {
			decoded = append(decoded, letter)
			continue
		}
		if text[index] == 'x' {
			value, parseError := strconv.ParseUint(text[index+1:min(index+3, len(text))], 16, 8)
			if parseError != nil || index+3 > len(text) {
				return "", fmt.Errorf("%q holds a hex escape that isn't two digits at %d", text, index-1)
			}
			decoded = append(decoded, byte(value))
			index += 2
			continue
		}
		value, digits := 0, 0
		for digits < 3 && index+digits < len(text) && text[index+digits] >= '0' && text[index+digits] <= '7' {
			value = value*8 + int(text[index+digits]-'0')
			digits++
		}
		if digits == 0 || value > 0o377 {
			return "", fmt.Errorf("%q holds an escape that's neither a letter nor a byte in octal at %d", text, index-1)
		}
		decoded = append(decoded, byte(value))
		index += digits - 1
	}
	return string(decoded), nil
}
