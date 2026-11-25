package strace_events

import (
	"strconv"
	"strings"
)

// A Call is one call a trace shows finished: the process that made it, its name, its arguments as
// strace printed them, and its result.
type Call struct {
	// Pid is the process that made the call, as Line.Pid.
	Pid string
	// Name is the call, `openat`.
	Name string
	// Arguments is everything between the call's parentheses, as strace printed it.
	Arguments string
	// Result is what the call returned.
	Result Result
	// Text is the call as one text, `openat(...) = 3</x>`, without its pid: the line it was printed on,
	// or the two halves of a split call joined.
	Text string
}

// ResultKind is whether strace knew what a call returned.
type ResultKind uint8

const (
	// NumberResult is a result strace printed as a number.
	NumberResult ResultKind = iota
	// UnknownResult is strace's `?`: a call strace never saw return, because its process exited, was
	// killed, or was replaced by another thread's execve during it. A number too large for an int64 is
	// one too, since no number this package could hand back would be the one strace printed.
	UnknownResult
)

// A Result is what a call returned, `= -1 ENOENT (No such file or directory)` or `= 3</x>`.
type Result struct {
	Kind ResultKind
	// Number is a NumberResult's value.
	Number int64
	// Raw is the result as strace printed it: `3`, `-1` or `?`.
	Raw string
	// Opened is the path strace decoded a returned descriptor to (--decode-fds=path), the `/x` of
	// `= 3</x>`, or empty.
	Opened Text
	// Errno is the error a failed call names, `ENOENT`, or "".
	Errno string
	// Detail is whatever strace printed after the result, its descriptor path and its errno, without
	// the whitespace around it: `(No such file or directory)`.
	Detail string
}

// parseCall reads `<name>(<arguments>) = <result>`. The arguments end at the last parenthesis outside
// every quoted string that a result follows, so neither a quoted `)` nor a quoted `) = 1` ends them:
// a shell's command line in an execve's argv is its own text, never the call's end.
func parseCall(text string) (Call, bool) {
	name := leading(text, isWord)
	if name == 0 || name == len(text) || text[name] != '(' {
		return Call{}, false
	}
	body := text[name+1:]
	call, found := Call{}, false
	quoted := false
	for index := 0; index < len(body); index++ {
		switch character := body[index]; {
		case character == '\\':
			// An escaped byte is the text's, a quote included, inside a quoted string or out of one:
			// strace writes a bare backslash nowhere else, and a decoded descriptor path spells a quote
			// `\"` outside every string, as `AT_FDCWD</d/q\"uote>`.
			index++
		case character == '"':
			quoted = !quoted
		case !quoted && character == ')':
			if result, isResult := parseResult(body[index+1:]); isResult {
				call, found = Call{Name: text[:name], Arguments: body[:index], Result: result, Text: text}, true
			}
		}
	}
	return call, found
}

// parseResult reads ` = <result>` at the start of text: `?` or a decimal number, then the descriptor
// path strace decoded right after it, then an errno and whatever follows. unread is what is left of
// text as each part is read off its front.
func parseResult(text string) (Result, bool) {
	spaces := leading(text, isSpace)
	if spaces == 0 || spaces == len(text) || text[spaces] != '=' {
		return Result{}, false
	}
	unread := text[spaces+1:]
	spaces = leading(unread, isSpace)
	if spaces == 0 {
		return Result{}, false
	}
	unread = unread[spaces:]
	result := Result{Kind: UnknownResult}
	if strings.HasPrefix(unread, "?") {
		result.Raw, unread = "?", unread[1:]
	} else {
		sign := 0
		if strings.HasPrefix(unread, "-") {
			sign = 1
		}
		digits := leading(unread[sign:], isDigit)
		if digits == 0 {
			return Result{}, false
		}
		result.Raw, unread = unread[:sign+digits], unread[sign+digits:]
		if number, parseError := strconv.ParseInt(result.Raw, 10, 64); parseError == nil {
			result.Kind, result.Number = NumberResult, number
		}
	}
	// strace escapes a `>` inside a decoded path (\76), so the first one closes it.
	if strings.HasPrefix(unread, "<") {
		if closing := strings.IndexByte(unread[1:], '>'); closing >= 0 {
			result.Opened, unread = newDescriptorText(unread[1:1+closing]), unread[2+closing:]
		}
	}
	if spaces = leading(unread, isSpace); spaces > 0 && spaces+1 < len(unread) && unread[spaces] == 'E' {
		if letters := leading(unread[spaces+1:], isErrnoCharacter); letters > 0 {
			result.Errno, unread = unread[spaces:spaces+1+letters], unread[spaces+1+letters:]
		}
	}
	result.Detail = strings.TrimSpace(unread)
	return result, true
}

// isErrnoCharacter is a byte of an errno's name after its E: a capital letter, a digit or an
// underscore, as ERESTART_RESTARTBLOCK's.
func isErrnoCharacter(character byte) bool {
	return isDigit(character) || 'A' <= character && character <= 'Z' || character == '_'
}
