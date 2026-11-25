package strace_events_test

import (
	"regexp"
	"strconv"
	"strings"
)

// Adamic's strace grammar, copied from adamic's internal/buildcache at a23ba096388c (Nov 9, 2026):
// the expressions in strace_test.go (lines 12 to 18), which its hand parsers in strace.go take exactly
// what they take (TestTheStraceParsersMatchTheirExpressions and FuzzTheStraceParsers hold them so),
// with settle.go's traceState.line (853 to 885) and unescape (1450 to 1458) around them. It is the
// second oracle the differential holds this package against, kept as Adamic reads a line, its error's
// name spelled out as cohere's lint asks.
var (
	adamicTraceLine  = regexp.MustCompile(`^(\d+)\s+(.*)$`)
	adamicResumed    = regexp.MustCompile(`^<\.\.\. (\w+) resumed>\s?(.*)$`)
	adamicCall       = regexp.MustCompile(`^(\w+)\((.*)\)\s+=\s+(-?\d+|\?)(?:<([^>]*)>)?(?:\s+(E[A-Z0-9]+))?`)
	adamicExit       = regexp.MustCompile(`^\+\+\+ (?:exited with \d+|killed by \w+(?: \(core dumped\))?) \+\+\+$`)
	adamicDirectory  = regexp.MustCompile(`^(AT_FDCWD|-?\d+)(?:<([^>]*)>)?,\s*`)
	adamicQuoted     = regexp.MustCompile(`^"((?:[^"\\]|\\.)*)"`)
	adamicDescriptor = regexp.MustCompile(`^-?\d+<([^>]*)>`)
)

// adamicCallParts is Adamic's traceCall, the parts parseCall reads.
type adamicCallParts struct {
	pid                                   string
	call, arguments, value, opened, errno string
}

// adamicParser is traceState's unfinished calls, by pid.
type adamicParser struct {
	unfinished map[string]string
}

// line is traceState.line: the call a line finishes, or the status a process's end reports, with
// whether it was an end at all.
func (parser *adamicParser) line(text string) (adamicCallParts, bool, string, bool) {
	if parser.unfinished == nil {
		parser.unfinished = map[string]string{}
	}
	match := adamicTraceLine.FindStringSubmatch(text)
	if match == nil {
		return adamicCallParts{}, false, "", false
	}
	pid, rest := match[1], match[2]
	if adamicExit.MatchString(rest) {
		status, _ := strings.CutPrefix(rest, "+++ exited with ")
		if status == rest {
			return adamicCallParts{}, false, "", true
		}
		return adamicCallParts{}, false, strings.TrimSuffix(status, " +++"), true
	}
	if strings.HasSuffix(rest, "<unfinished ...>") {
		parser.unfinished[pid] = strings.TrimSpace(strings.TrimSuffix(rest, "<unfinished ...>"))
		return adamicCallParts{}, false, "", false
	}
	if resumed := adamicResumed.FindStringSubmatch(rest); resumed != nil {
		start, found := parser.unfinished[pid]
		if !found {
			return adamicCallParts{}, false, "", false
		}
		delete(parser.unfinished, pid)
		if tail := resumed[2]; strings.HasPrefix(tail, ")") || strings.HasPrefix(tail, ",") {
			rest = start + tail
		} else {
			rest = start + " " + tail
		}
	}
	call := adamicCall.FindStringSubmatch(rest)
	if call == nil {
		return adamicCallParts{}, false, "", false
	}
	parts := adamicCallParts{
		pid:       pid,
		call:      call[1],
		arguments: call[2],
		value:     call[3],
		opened:    call[4],
		errno:     call[5],
	}
	return parts, true, "", false
}

// adamicUnescape is Adamic's unescape: a path as strace spells it back in its bytes, or as spelled
// when Go's unquoting refuses it.
func adamicUnescape(text string) string {
	if !strings.Contains(text, `\`) {
		return text
	}
	if value, unquoteError := strconv.Unquote(`"` + text + `"`); unquoteError == nil {
		return value
	}
	return text
}
