package strace_events_test

import (
	"testing"

	"github.com/system-inc/cohere/strace_events"
)

// A line's pid reads in each form strace prints it, and the call after it reads the same in all three.
func TestAPidReadsInEveryFormStracePrints(test *testing.T) {
	test.Parallel()
	for text, pid := range map[string]string{
		`123 openat(AT_FDCWD, "x", O_RDONLY) = 3`:          "123",
		`[pid 123] openat(AT_FDCWD, "x", O_RDONLY) = 3`:    "123",
		`[pid    123] openat(AT_FDCWD, "x", O_RDONLY) = 3`: "123",
		"123\topenat(AT_FDCWD, \"x\", O_RDONLY) = 3":       "123",
		`  123 openat(AT_FDCWD, "x", O_RDONLY) = 3  `:      "123",
		`openat(AT_FDCWD, "x", O_RDONLY) = 3`:              "",
	} {
		line := strace_events.ParseLine(text)
		if line.Kind != strace_events.CallLine ||
			line.Pid != pid ||
			line.Call.Pid != pid ||
			line.Call.Name != "openat" ||
			line.Call.Arguments != `AT_FDCWD, "x", O_RDONLY` ||
			line.Call.Result.Number != 3 {
			test.Errorf("%q reads as %+v", text, line)
		}
	}
	for _, text := range []string{
		`[pid 45 openat(AT_FDCWD, "x") = 3`,
		`[pid] openat(AT_FDCWD, "x") = 3`,
		`[pid 45]openat(AT_FDCWD, "x") = 3`,
	} {
		if line := strace_events.ParseLine(text); line.Kind != strace_events.OtherLine {
			test.Errorf("%q, a pid strace never prints, reads as %+v", text, line)
		}
	}
}

// A call's arguments end at the parenthesis a result follows outside every quoted string: a `)` or a
// whole `) = 1` inside a path or a shell's command line is the argument's own text, and a quote a
// decoded descriptor path escapes opens no string.
func TestArgumentsEndOutsideEveryQuotedString(test *testing.T) {
	test.Parallel()
	for _, want := range []struct{ text, arguments string }{
		{
			`12 execve("/bin/sh", ["sh", "-c", "a) = 1"], 0x7ffd /* 40 vars */) = ?`,
			`"/bin/sh", ["sh", "-c", "a) = 1"], 0x7ffd /* 40 vars */`,
		},
		{
			`12 execve("/bin/sh", ["sh", "-c", "test $(a) = 1"], 0x7ffd /* 40 vars */) = 0`,
			`"/bin/sh", ["sh", "-c", "test $(a) = 1"], 0x7ffd /* 40 vars */`,
		},
		{
			`12 openat(AT_FDCWD, "fake) = 7<x>", O_RDONLY) = -1 ENOENT (No such file or directory)`,
			`AT_FDCWD, "fake) = 7<x>", O_RDONLY`,
		},
		{`12 openat(AT_FDCWD, "quote\") = 1", O_RDONLY) = 3`, `AT_FDCWD, "quote\") = 1", O_RDONLY`},
		{`9 openat(AT_FDCWD</d/q\"uote>, "in", O_RDONLY) = 3`, `AT_FDCWD</d/q\"uote>, "in", O_RDONLY`},
		{
			`12 newfstatat(AT_FDCWD, "x", {st_mode=S_IFREG|0644, ...}, 0) = 0`,
			`AT_FDCWD, "x", {st_mode=S_IFREG|0644, ...}, 0`,
		},
	} {
		line := strace_events.ParseLine(want.text)
		if line.Kind != strace_events.CallLine || line.Call.Arguments != want.arguments {
			test.Errorf("%q reads arguments %q, not %q", want.text, line.Call.Arguments, want.arguments)
		}
	}
	// A quote that never closes leaves no parenthesis outside a string, so the line is no call.
	unclosed := strace_events.ParseLine(`44 openat(AT_FDCWD, "unterminated, O_RDONLY) = 3`)
	if unclosed.Kind != strace_events.OtherLine {
		test.Errorf("an unclosed quote reads as %+v", unclosed)
	}
}

// A result is a number, with the path a returned descriptor decodes to and an errno, or strace's
// unknown `?`, which is never a number. An errno is read whole, an underscore in its name included.
func TestAResultIsANumberOrUnknown(test *testing.T) {
	test.Parallel()
	for text, want := range map[string]strace_events.Result{
		`1 openat(AT_FDCWD, "x", O_RDONLY) = 3</w/x>`: {
			Kind:   strace_events.NumberResult,
			Number: 3,
			Raw:    "3",
			Opened: strace_events.Text{Raw: "/w/x", Unescaped: "/w/x"},
		},
		`1 openat(AT_FDCWD, "x", O_RDONLY) = 3</w/d\303\251/x (deleted)>`: {
			Kind:   strace_events.NumberResult,
			Number: 3,
			Raw:    "3",
			Opened: strace_events.Text{Raw: `/w/d\303\251/x (deleted)`, Unescaped: "/w/dé/x (deleted)"},
		},
		`1 openat(AT_FDCWD, "x", O_RDONLY) = -1 ENOENT (No such file or directory)`: {
			Kind:   strace_events.NumberResult,
			Number: -1,
			Raw:    "-1",
			Errno:  "ENOENT",
			Detail: "(No such file or directory)",
		},
		`1 execve("/bin/sh", ["sh"], 0x7ffd /* 40 vars */) = ?`: {Kind: strace_events.UnknownResult, Raw: "?"},
		`1 wait4(-1, 0x7ffd, 0, NULL) = ? ERESTARTSYS (To be restarted if SA_RESTART is set)`: {
			Kind:   strace_events.UnknownResult,
			Raw:    "?",
			Errno:  "ERESTARTSYS",
			Detail: "(To be restarted if SA_RESTART is set)",
		},
		`9 openat(AT_FDCWD</tree/p>, "data.txt", O_RDONLY|O_CLOEXEC) = ? ERESTART_RESTARTBLOCK (Interrupted by signal)`: {
			Kind:   strace_events.UnknownResult,
			Raw:    "?",
			Errno:  "ERESTART_RESTARTBLOCK",
			Detail: "(Interrupted by signal)",
		},
		`1 openat(AT_FDCWD, "x", O_RDONLY) = 99999999999999999999`: {
			Kind: strace_events.UnknownResult,
			Raw:  "99999999999999999999",
		},
	} {
		if line := strace_events.ParseLine(text); line.Kind != strace_events.CallLine || line.Call.Result != want {
			test.Errorf("%q's result reads as %+v, not %+v", text, line.Call.Result, want)
		}
	}
	for _, text := range []string{
		`1 openat(AT_FDCWD, "x") =`,
		`1 openat(AT_FDCWD, "x") = -`,
		`1 openat(AT_FDCWD, "x") =3`,
		`1 openat(AT_FDCWD, "x")= 3`,
	} {
		if line := strace_events.ParseLine(text); line.Kind != strace_events.OtherLine {
			test.Errorf("%q, with no result, reads as %+v", text, line)
		}
	}
}

// A call strace split around another process's joins back whole on its own pid, in any pid form, with
// the whitespace an older strace printed after `resumed>` left out of its arguments. A resumption with
// nothing unfinished on its pid, and a call never resumed, finish nothing.
func TestASplitCallJoinsOnItsOwnPid(test *testing.T) {
	test.Parallel()
	trace := []string{
		`20 openat(AT_FDCWD</w>, "a.txt", O_RDONLY <unfinished ...>`,
		`[pid 21] read(3,  <unfinished ...>`,
		`25 <... openat resumed>) = 3</w/never-started.txt>`,
		`21 <... read resumed>"abc", 10) = 3`,
		`20 <... openat resumed> ) = 6</w/a.txt>`,
		`29 openat(AT_FDCWD</w>, "abandoned.txt", O_RDONLY <unfinished ...>`,
		`29 +++ killed by SIGKILL +++`,
	}
	joiner := strace_events.Joiner{}
	calls := []strace_events.Call{}
	for _, text := range trace {
		if call, isCall := joiner.Join(strace_events.ParseLine(text)); isCall {
			calls = append(calls, call)
		}
	}
	if len(calls) != 2 {
		test.Fatalf("the trace finishes %d calls, not 2: %+v", len(calls), calls)
	}
	if read := calls[0]; read.Pid != "21" ||
		read.Name != "read" ||
		read.Arguments != `3, "abc", 10` ||
		read.Result.Number != 3 {
		test.Errorf("the split read joins as %+v", read)
	}
	if opened := calls[1]; opened.Pid != "20" ||
		opened.Arguments != `AT_FDCWD</w>, "a.txt", O_RDONLY` ||
		opened.Result.Opened.Unescaped != "/w/a.txt" ||
		opened.Text != `openat(AT_FDCWD</w>, "a.txt", O_RDONLY) = 6</w/a.txt>` {
		test.Errorf("the split openat joins as %+v", opened)
	}
	if line := strace_events.ParseLine(trace[0]); line.Kind != strace_events.UnfinishedLine ||
		line.Name != "openat" ||
		line.Started != `openat(AT_FDCWD</w>, "a.txt", O_RDONLY` {
		test.Errorf("an unfinished line reads as %+v", line)
	}
}

// A process's end and a signal read as lines of their own, and only in the forms strace prints.
func TestExitsAndSignalsReadAsTheirOwnLines(test *testing.T) {
	test.Parallel()
	for text, want := range map[string]strace_events.Exit{
		`12 +++ exited with 0 +++`:                         {Status: "0"},
		`12 +++ exited with 137 +++`:                       {Status: "137"},
		`12 +++ killed by SIGKILL +++`:                     {Signal: "SIGKILL"},
		`12 +++ killed by SIGSEGV (core dumped) +++`:       {Signal: "SIGSEGV", CoreDumped: true},
		`[pid 12] +++ killed by SIGSEGV (core dumped) +++`: {Signal: "SIGSEGV", CoreDumped: true},
	} {
		if line := strace_events.ParseLine(text); line.Kind != strace_events.ExitLine || line.Exit != want {
			test.Errorf("%q reads as %+v", text, line)
		}
	}
	for _, text := range []string{
		`12 +++ exited with  +++`,
		`12 +++ exited with 1 +++ +++`,
		`12 +++ killed by SIGSEGV (core dumped) (core dumped) +++`,
	} {
		if line := strace_events.ParseLine(text); line.Kind == strace_events.ExitLine {
			test.Errorf("%q, no end strace prints, reads as one: %+v", text, line)
		}
	}
	signal := strace_events.ParseLine(`33 --- SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED} ---`)
	if signal.Kind != strace_events.SignalLine || signal.Signal != "SIGCHLD {si_signo=SIGCHLD, si_code=CLD_EXITED}" {
		test.Errorf("a signal reads as %+v", signal)
	}
}

// Unescape decodes every escape strace writes, including the short octal Go's strconv.Unquote refuses,
// and reads all of it as well formed.
func TestUnescapeDecodesEveryEscapeStraceWrites(test *testing.T) {
	test.Parallel()
	for escaped, bytes := range map[string]string{
		`plain.txt`:        "plain.txt",
		`w\303\251.txt`:    "wé.txt",
		`a\76b\"c\74d.txt`: `a>b"c<d.txt`,
		`escape\33[0m`:     "escape\x1b[0m",
		`byte\1.txt`:       "byte\x01.txt",
		`byte\0017.txt`:    "byte\x017.txt",
		`\0`:               "\x00",
		`back\\slash`:      `back\slash`,
		`\t\n\v\f\r\a\b`:   "\t\n\v\f\r\a\b",
		`\x68\x65\x78`:     "hex",
		`angle<>`:          "angle<>",
	} {
		if unescaped, wellFormed := strace_events.Unescape(escaped); unescaped != bytes || !wellFormed {
			test.Errorf("Unescape(%q) = %q, %v, not %q, true", escaped, unescaped, wellFormed, bytes)
		}
	}
}

// Text strace never writes is malformed, each shape of it, and says so: Unescape reads it as not well
// formed, keeping what it couldn't read as spelled, and every helper that hands back a Text marks it,
// so a consumer refuses or keeps it as it chooses and never keys it unknowing.
func TestTextStraceNeverWritesIsMalformed(test *testing.T) {
	test.Parallel()
	for name, malformed := range map[string]struct{ escaped, kept string }{
		"an escape strace has none of":      {`\q.txt`, `\q.txt`},
		"a hexadecimal escape of one digit": {`\x6`, `\x6`},
		"a hexadecimal escape of no digit":  {`\xg`, `\xg`},
		"an octal escape past a byte":       {`\400.txt`, `\400.txt`},
		"a backslash ending the text":       {`trailing\`, `trailing\`},
		"a byte outside printable ASCII":    {"bare\xff", "bare\xff"},
		"a control byte":                    {"bare\x01", "bare\x01"},
		"a quote bare":                      {`bare"quote`, `bare"quote`},
		"a bare byte after an escape":       {"\\303bare\xff", "\xc3bare\xff"},
	} {
		unescaped, wellFormed := strace_events.Unescape(malformed.escaped)
		if wellFormed || unescaped != malformed.kept {
			test.Errorf(
				"%s: Unescape(%q) = %q, %v, not %q, false",
				name, malformed.escaped, unescaped, wellFormed, malformed.kept,
			)
		}
	}
	// A bare angle bracket is malformed only inside a decoded descriptor path, where strace escapes it.
	if _, wellFormed := strace_events.UnescapeDescriptor("/w/angle<"); wellFormed {
		test.Error("a bare < in a decoded path reads as well formed")
	}
	if text, _, found := strace_events.QuotedArgument(`"\q.txt", O_RDONLY`); !found || !text.Malformed {
		test.Errorf("a malformed quoted argument reads as %+v", text)
	}
	if text, _, found := strace_events.QuotedArgument(`"w\303\251.txt", O_RDONLY`); !found || text.Malformed {
		test.Errorf("a well formed quoted argument reads as %+v", text)
	}
	descriptor, _, found := strace_events.DescriptorArgument(`AT_FDCWD</w/\q>, "x"`)
	if !found || !descriptor.Path.Malformed {
		test.Errorf("a malformed decoded descriptor path reads as %+v", descriptor)
	}
	opened := strace_events.ParseLine(`1 openat(AT_FDCWD, "x", O_RDONLY) = 3</w/\400>`)
	if opened.Kind != strace_events.CallLine || !opened.Call.Result.Opened.Malformed {
		test.Errorf("a malformed opened path reads as %+v", opened.Call.Result.Opened)
	}
}

// The argument helpers read a descriptor, decoded or not and ending its arguments or not, a quoted
// string, cut short or not, and the next argument after a comma.
func TestTheArgumentHelpersReadEachArgument(test *testing.T) {
	test.Parallel()
	for arguments, want := range map[string]struct {
		name, path, rest string
	}{
		`AT_FDCWD</w/d\303\251>, "x"`: {"AT_FDCWD", "/w/dé", `"x"`},
		`AT_FDCWD, "x"`:               {"AT_FDCWD", "", `"x"`},
		`5</w/dir>,  0x55d0, 32768`:   {"5", "/w/dir", "0x55d0, 32768"},
		`-100, "/x"`:                  {"-100", "", `"/x"`},
		`5</w/dir>`:                   {"5", "/w/dir", ""},
		`5`:                           {"5", "", ""},
	} {
		descriptor, rest, found := strace_events.DescriptorArgument(arguments)
		if !found || descriptor.Name != want.name || descriptor.Path.Unescaped != want.path || rest != want.rest {
			test.Errorf("DescriptorArgument(%q) = %+v, %q, %v", arguments, descriptor, rest, found)
		}
	}
	for _, arguments := range []string{`"x"`, `4<unclosed, "c"`, `4</a>x, "c"`, `AT_FDCWDX, "c"`, ``} {
		if descriptor, _, found := strace_events.DescriptorArgument(arguments); found {
			test.Errorf("DescriptorArgument(%q) reads %+v", arguments, descriptor)
		}
	}
	text, rest, found := strace_events.QuotedArgument(`"a \"b\" \303\251"..., 32`)
	if !found || text.Raw != `a \"b\" \303\251` || text.Unescaped != `a "b" é` || rest != "..., 32" {
		test.Errorf("a quoted string cut short reads as %+v, %q, %v", text, rest, found)
	}
	for _, arguments := range []string{`x"`, `"unclosed`, `"ends in a backslash\"`} {
		if text, _, found := strace_events.QuotedArgument(arguments); found {
			test.Errorf("QuotedArgument(%q) reads %+v", arguments, text)
		}
	}
	if next := strace_events.NextArgument(` , AT_FDCWD</w>, "b" `); next != `AT_FDCWD</w>, "b"` {
		test.Errorf("NextArgument reads %q", next)
	}
}

// Every name Loom's tracer spells into a decoded descriptor path (ebpftrace.QuoteDescriptor, the names
// of Loom's TestEveryDecoratedNameReadsAsItself) reads back as itself, well formed, wherever strace
// decorates a descriptor: an argument's, which Loom joins with the quoted path after it, as `/d/q"uote`
// and "in" read /d/q"uote/in, a returned one's, and the one a call is made on. An escaped angle bracket
// closes no decoration, and an escaped quote opens no string.
func TestEveryDecoratedPathReadsAsItself(test *testing.T) {
	test.Parallel()
	for name, spelled := range map[string]string{
		"/d/a>":         `/d/a\76`,
		"/d/<b>":        `/d/\74b\76`,
		`/d/q"uote`:     `/d/q\"uote`,
		`/d/back\slash`: `/d/back\\slash`,
		"/d/\x018":      `/d/\0018`,
		"/d/\x01a":      `/d/\1a`,
		"/d/end\xff":    `/d/end\377`,
		"/d/\x7f>":      `/d/\177\76`,
		"/d/a,>, x":     `/d/a,\76, x`,
	} {
		decorated := strace_events.ParseLine(`9 openat(AT_FDCWD<` + spelled + `>, "in", O_RDONLY) = 3`)
		descriptor, rest, isDescriptor := strace_events.DescriptorArgument(decorated.Call.Arguments)
		relative, _, isQuoted := strace_events.QuotedArgument(rest)
		if decorated.Kind != strace_events.CallLine ||
			!isDescriptor ||
			descriptor.Path != (strace_events.Text{Raw: spelled, Unescaped: name}) ||
			!isQuoted ||
			relative.Unescaped != "in" {
			test.Errorf("%q decorating AT_FDCWD reads as %+v, %+v and %+v", name, decorated, descriptor, relative)
		}
		opened := strace_events.ParseLine(
			`9 openat(AT_FDCWD</>, "/proc/self/fd/7", O_RDONLY) = 8<` + spelled + `/via>`,
		)
		if opened.Kind != strace_events.CallLine ||
			opened.Call.Result.Opened != (strace_events.Text{Raw: spelled + "/via", Unescaped: name + "/via"}) {
			test.Errorf("%q decorating a returned descriptor reads as %+v", name, opened)
		}
		stat := strace_events.ParseLine(
			`9 newfstatat(8<` + spelled + `/via>, "", {st_mode=S_IFREG|0644, st_size=0, ...}, AT_EMPTY_PATH) = 0`,
		)
		statDescriptor, statRest, isStatDescriptor := strace_events.DescriptorArgument(stat.Call.Arguments)
		empty, _, isEmptyQuoted := strace_events.QuotedArgument(statRest)
		if stat.Kind != strace_events.CallLine ||
			!isStatDescriptor ||
			statDescriptor.Path != (strace_events.Text{Raw: spelled + "/via", Unescaped: name + "/via"}) ||
			!isEmptyQuoted ||
			empty.Unescaped != "" {
			test.Errorf("%q decorating a stat's descriptor reads as %+v, %+v and %+v", name, stat, statDescriptor, empty)
		}
	}
}
