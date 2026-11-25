package strace_events_test

// The ledgers: every line of the in-repo corpus that Loom or Adamic reads otherwise than this package,
// with both readings, the class that explains it, and what moving onto this package changes for that
// consumer. TestEveryDifferenceFromEachConsumerIsOnItsLedger holds them to the corpus both ways: a
// difference with no row fails, and so does a row whose line no longer differs. A reading is the call
// a line finishes, field by field, then its arguments walked as the consumer walks them, each path as
// the consumer keys it (differential_test.go).
//
// What the rows say, by class:
//   - unknownResult: strace's `= ?`, and a number past an int64. Loom reads only numbers it can hold,
//     so it drops the call; for an execve that is a binary the run reached that Loom never keys.
//   - quotedArguments: Loom ends an execve's arguments at a `) = 1` inside its argv, and both read past
//     a quote that never closes, which strace never prints and which neither then reads a path from.
//   - escapedPath: Adamic decodes a quoted path and a decoded descriptor path through strconv.Unquote,
//     which refuses strace's short octal (\76, \33[, \1), and then keys the escaped spelling, a name
//     no file has. Loom decodes both through ebpftrace's own unescape, which reads strace's escapes
//     as this package does, so it has no row here.
//   - malformedText: a path holding bytes strace never writes bare. This package marks it Malformed;
//     Loom refuses the trace there, and Adamic keys the bytes as read.
//   - resumedSpace: an older strace's space after `resumed>` stays in Loom's arguments, where only a
//     flag's presence is read, so no key moves.
//   - lineForm: Adamic reads only a bare pid at the start of a line, so a `[pid N]` line, a line with
//     no pid, a line with whitespace before it, and the resumption of a call whose first half was one
//     are all dropped, with whatever they read. Loom's one row is a call named in capitals, which
//     strace never prints.
//   - negativeDescriptor: Loom reads a descriptor printed negative, -100 or -1, as no descriptor, and
//     refuses the trace where a path follows it.

// A ledgerRow is one line a consumer reads otherwise than this package, with both readings, why
// (its class), and what it changes for that consumer (its direction).
type ledgerRow struct {
	line, oracle, module string
	class                differenceClass
	direction            direction
}

var loomLedger = []ledgerRow{
	{
		line:      "12 openat(AT_FDCWD, \"\xff\\\xff\", O_RDONLY) = 3<\xff>",
		oracle:    `openat pid "12" result 3 opened "undecodable" arguments "AT_FDCWD, \"\xff\\\xff\", O_RDONLY" [descriptor AT_FDCWD ""; path "undecodable"]`,
		module:    `openat pid "12" result 3 opened "malformed" arguments "AT_FDCWD, \"\xff\\\xff\", O_RDONLY" [descriptor AT_FDCWD ""; path "malformed"]`,
		class:     malformedText,
		direction: harmless,
	},
	{
		line:      `12 openat(-100, "/x", O_RDONLY) = 3`,
		oracle:    `openat pid "12" result 3 opened "" arguments "-100, \"/x\", O_RDONLY" []`,
		module:    `openat pid "12" result 3 opened "" arguments "-100, \"/x\", O_RDONLY" [descriptor -100 ""; path "/x"]`,
		class:     negativeDescriptor,
		direction: harmless,
	},
	{
		line:      `12 execve("/bin/sh", ["sh"], 0x7ffd /* 40 vars */) = ?`,
		oracle:    `no call`,
		module:    `execve pid "12" result ? arguments "\"/bin/sh\", [\"sh\"], 0x7ffd /* 40 vars */"`,
		class:     unknownResult,
		direction: missedRead,
	},
	{
		line:      `12 fchdir(-1</nowhere>) = -1 EBADF (Bad file descriptor)`,
		oracle:    `fchdir pid "12" result -1 opened "" arguments "-1</nowhere>" []`,
		module:    `fchdir pid "12" result -1 opened "" arguments "-1</nowhere>" [descriptor -1 "/nowhere"]`,
		class:     negativeDescriptor,
		direction: harmless,
	},
	{
		line:      `12 openat(AT_FDCWD, "/x\", O_RDONLY) = 3`,
		oracle:    `openat pid "12" result 3 opened "" arguments "AT_FDCWD, \"/x\\\", O_RDONLY" [descriptor AT_FDCWD ""]`,
		module:    `no call`,
		class:     quotedArguments,
		direction: harmless,
	},
	{
		line:      `24 <... openat resumed> ) = 3</w/old-space.txt>`,
		oracle:    `openat pid "24" result 3 opened "/w/old-space.txt" arguments "AT_FDCWD</w>, \"old-space.txt\", O_RDONLY " [descriptor AT_FDCWD "/w"; path "old-space.txt"]`,
		module:    `openat pid "24" result 3 opened "/w/old-space.txt" arguments "AT_FDCWD</w>, \"old-space.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "old-space.txt"]`,
		class:     resumedSpace,
		direction: harmless,
	},
	{
		line:      `31 execve("/bin/sh", ["sh", "-c", "a) = 1"], 0x7ffd /* 40 vars */) = ?`,
		oracle:    `execve pid "31" result 1 opened "" arguments "\"/bin/sh\", [\"sh\", \"-c\", \"a" [path "/bin/sh"]`,
		module:    `execve pid "31" result ? arguments "\"/bin/sh\", [\"sh\", \"-c\", \"a) = 1\"], 0x7ffd /* 40 vars */"`,
		class:     quotedArguments,
		direction: harmless,
	},
	{
		line:      `31 execve("/bin/sh", ["sh"], 0x7ffd /* 40 vars */) = ?`,
		oracle:    `no call`,
		module:    `execve pid "31" result ? arguments "\"/bin/sh\", [\"sh\"], 0x7ffd /* 40 vars */"`,
		class:     unknownResult,
		direction: missedRead,
	},
	{
		line:      `32 read(0,  <unfinished ...>) = ?`,
		oracle:    `no call`,
		module:    `read pid "32" result ? arguments "0,  <unfinished ...>"`,
		class:     unknownResult,
		direction: harmless,
	},
	{
		line:      `32 exit_group(0) = ?`,
		oracle:    `no call`,
		module:    `exit_group pid "32" result ? arguments "0"`,
		class:     unknownResult,
		direction: harmless,
	},
	{
		line:      `40 openat(-100, "/absolute.txt", O_RDONLY) = 3`,
		oracle:    `openat pid "40" result 3 opened "" arguments "-100, \"/absolute.txt\", O_RDONLY" []`,
		module:    `openat pid "40" result 3 opened "" arguments "-100, \"/absolute.txt\", O_RDONLY" [descriptor -100 ""; path "/absolute.txt"]`,
		class:     negativeDescriptor,
		direction: harmless,
	},
	{
		line:      `42 wait4(-1, 0x7ffd, 0, NULL) = ? ERESTARTSYS (To be restarted if SA_RESTART is set)`,
		oracle:    `no call`,
		module:    `wait4 pid "42" result ? arguments "-1, 0x7ffd, 0, NULL"`,
		class:     unknownResult,
		direction: harmless,
	},
	{
		line:      `43 openat(AT_FDCWD</w>, "huge", O_RDONLY) = 99999999999999999999`,
		oracle:    `no call`,
		module:    `openat pid "43" result 99999999999999999999 arguments "AT_FDCWD</w>, \"huge\", O_RDONLY"`,
		class:     unknownResult,
		direction: missedRead,
	},
	{
		line:      `44 openat(AT_FDCWD, "unterminated, O_RDONLY) = 3`,
		oracle:    `openat pid "44" result 3 opened "" arguments "AT_FDCWD, \"unterminated, O_RDONLY" [descriptor AT_FDCWD ""]`,
		module:    `no call`,
		class:     quotedArguments,
		direction: harmless,
	},
	{
		line:      `44 OPENAT(AT_FDCWD, "capital") = 3`,
		oracle:    `no call`,
		module:    `OPENAT pid "44" result 3 opened "" arguments "AT_FDCWD, \"capital\"" [descriptor AT_FDCWD ""; path "capital"]`,
		class:     lineForm,
		direction: harmless,
	},
	{
		line:      `9 openat(AT_FDCWD</w>, "slow", O_RDONLY) = ? ERESTARTSYS (To be restarted if SA_RESTART is set)`,
		oracle:    `no call`,
		module:    `openat pid "9" result ? arguments "AT_FDCWD</w>, \"slow\", O_RDONLY"`,
		class:     unknownResult,
		direction: harmless,
	},
	{
		line:      `9 fsmount(-1, 0x0, 0x0) = -1 EPERM (Operation not permitted)`,
		oracle:    `fsmount pid "9" result -1 opened "" arguments "-1, 0x0, 0x0" []`,
		module:    `fsmount pid "9" result -1 opened "" arguments "-1, 0x0, 0x0" [descriptor -1 ""]`,
		class:     negativeDescriptor,
		direction: harmless,
	},
	{
		line:      `9 newfstatat(-1, "x", 0x0, 0) = -1 EBADF (Bad file descriptor)`,
		oracle:    `newfstatat pid "9" result -1 opened "" arguments "-1, \"x\", 0x0, 0" []`,
		module:    `newfstatat pid "9" result -1 opened "" arguments "-1, \"x\", 0x0, 0" [descriptor -1 ""; path "x"]`,
		class:     negativeDescriptor,
		direction: harmless,
	},
}

var adamicLedger = []ledgerRow{
	{
		line:      `3315478 openat(AT_FDCWD</tmp/a>, "a>b\"c<d.txt", O_RDONLY) = 3</tmp/a/a\76b\"c\74d.txt>`,
		oracle:    `openat pid "3315478" result 3 opened "/tmp/a/a\\76b\\\"c\\74d.txt" errno "" arguments "AT_FDCWD</tmp/a>, \"a>b\\\"c<d.txt\", O_RDONLY" [descriptor AT_FDCWD "/tmp/a"; path "a>b\"c<d.txt"]`,
		module:    `openat pid "3315478" result 3 opened "/tmp/a/a>b\"c<d.txt" errno "" arguments "AT_FDCWD</tmp/a>, \"a>b\\\"c<d.txt\", O_RDONLY" [descriptor AT_FDCWD "/tmp/a"; path "a>b\"c<d.txt"]`,
		class:     escapedPath,
		direction: misKeyedPath,
	},
	{
		line:      `12 openat(AT_FDCWD, "/x\", O_RDONLY) = 3`,
		oracle:    `openat pid "12" result 3 opened "" errno "" arguments "AT_FDCWD, \"/x\\\", O_RDONLY" [descriptor AT_FDCWD ""]`,
		module:    `no call`,
		class:     quotedArguments,
		direction: harmless,
	},
	{
		line:      ` openat(AT_FDCWD, "/x", O_RDONLY) = 3`,
		oracle:    `no call`,
		module:    `openat pid "" result 3 opened "" errno "" arguments "AT_FDCWD, \"/x\", O_RDONLY" [descriptor AT_FDCWD ""; path "/x"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `openat(AT_FDCWD, "/x", O_RDONLY) = 3`,
		oracle:    `no call`,
		module:    `openat pid "" result 3 opened "" errno "" arguments "AT_FDCWD, \"/x\", O_RDONLY" [descriptor AT_FDCWD ""; path "/x"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `[pid 7] openat(AT_FDCWD</w>, "a.txt", O_RDONLY) = 3</w/a.txt>`,
		oracle:    `no call`,
		module:    `openat pid "7" result 3 opened "/w/a.txt" errno "" arguments "AT_FDCWD</w>, \"a.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "a.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `[pid    7] newfstatat(AT_FDCWD</w>, "b.txt", 0x7ffd, 0) = -1 ENOENT (No such file or directory)`,
		oracle:    `no call`,
		module:    `newfstatat pid "7" result -1 opened "" errno "ENOENT" arguments "AT_FDCWD</w>, \"b.txt\", 0x7ffd, 0" [descriptor AT_FDCWD "/w"; path "b.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `openat(AT_FDCWD</w>, "no-pid.txt", O_RDONLY) = 3</w/no-pid.txt>`,
		oracle:    `no call`,
		module:    `openat pid "" result 3 opened "/w/no-pid.txt" errno "" arguments "AT_FDCWD</w>, \"no-pid.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "no-pid.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `   12 openat(AT_FDCWD</w>, "indented.txt", O_RDONLY) = 4</w/indented.txt>`,
		oracle:    `no call`,
		module:    `openat pid "12" result 4 opened "/w/indented.txt" errno "" arguments "AT_FDCWD</w>, \"indented.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "indented.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `[pid 23] newfstatat(AT_FDCWD</w>, "c.txt", {st_mode=S_IFREG|0644, st_size=1, ...}, 0) = 0`,
		oracle:    `no call`,
		module:    `newfstatat pid "23" result 0 opened "" errno "" arguments "AT_FDCWD</w>, \"c.txt\", {st_mode=S_IFREG|0644, st_size=1, ...}, 0" [descriptor AT_FDCWD "/w"; path "c.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `[pid 22] <... read resumed>"abc", 10) = 3`,
		oracle:    `no call`,
		module:    `read pid "22" result 3 opened "" errno "" arguments "3, \"abc\", 10" [descriptor 3 ""; path "abc"]`,
		class:     lineForm,
		direction: harmless,
	},
	{
		line:      `23 <... openat resumed>) = 7</w/split-bracketed.txt>`,
		oracle:    `no call`,
		module:    `openat pid "23" result 7 opened "/w/split-bracketed.txt" errno "" arguments "AT_FDCWD</w>, \"split-bracketed.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "split-bracketed.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `30 openat(AT_FDCWD</w>, "angle<>.txt", O_RDONLY) = 3</w/angle\74\76.txt>`,
		oracle:    `openat pid "30" result 3 opened "/w/angle\\74\\76.txt" errno "" arguments "AT_FDCWD</w>, \"angle<>.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "angle<>.txt"]`,
		module:    `openat pid "30" result 3 opened "/w/angle<>.txt" errno "" arguments "AT_FDCWD</w>, \"angle<>.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "angle<>.txt"]`,
		class:     escapedPath,
		direction: misKeyedPath,
	},
	{
		line:      `30 openat(AT_FDCWD</w>, "escape\33[0m.txt", O_RDONLY) = -1 ENOENT (No such file or directory)`,
		oracle:    `openat pid "30" result -1 opened "" errno "ENOENT" arguments "AT_FDCWD</w>, \"escape\\33[0m.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "escape\\33[0m.txt"]`,
		module:    `openat pid "30" result -1 opened "" errno "ENOENT" arguments "AT_FDCWD</w>, \"escape\\33[0m.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "escape\x1b[0m.txt"]`,
		class:     escapedPath,
		direction: misKeyedPath,
	},
	{
		line:      `30 openat(AT_FDCWD</w>, "byte\1.txt", O_RDONLY) = -1 ENOENT (No such file or directory)`,
		oracle:    `openat pid "30" result -1 opened "" errno "ENOENT" arguments "AT_FDCWD</w>, \"byte\\1.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "byte\\1.txt"]`,
		module:    `openat pid "30" result -1 opened "" errno "ENOENT" arguments "AT_FDCWD</w>, \"byte\\1.txt\", O_RDONLY" [descriptor AT_FDCWD "/w"; path "byte\x01.txt"]`,
		class:     escapedPath,
		direction: misKeyedPath,
	},
	{
		line:      `44 openat(AT_FDCWD, "unterminated, O_RDONLY) = 3`,
		oracle:    `openat pid "44" result 3 opened "" errno "" arguments "AT_FDCWD, \"unterminated, O_RDONLY" [descriptor AT_FDCWD ""]`,
		module:    `no call`,
		class:     quotedArguments,
		direction: harmless,
	},
	{
		line:      `[pid   102] openat(AT_FDCWD, "relative.txt", O_RDONLY) = 4`,
		oracle:    `no call`,
		module:    `openat pid "102" result 4 opened "" errno "" arguments "AT_FDCWD, \"relative.txt\", O_RDONLY" [descriptor AT_FDCWD ""; path "relative.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      `[pid   209] faccessat2(AT_FDCWD</work>, "sub/bin/tool", X_OK, AT_EACCESS) = 0`,
		oracle:    `no call`,
		module:    `faccessat2 pid "209" result 0 opened "" errno "" arguments "AT_FDCWD</work>, \"sub/bin/tool\", X_OK, AT_EACCESS" [descriptor AT_FDCWD "/work"; path "sub/bin/tool"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      ` openat(AT_FDCWD</w>, "a.txt", O_RDONLY|O_CLOEXEC) = 3</w/a.txt>`,
		oracle:    `no call`,
		module:    `openat pid "" result 3 opened "/w/a.txt" errno "" arguments "AT_FDCWD</w>, \"a.txt\", O_RDONLY|O_CLOEXEC" [descriptor AT_FDCWD "/w"; path "a.txt"]`,
		class:     lineForm,
		direction: missedRead,
	},
	{
		line:      ` +++ exited with 0 +++`,
		oracle:    `no call`,
		module:    `exit "0"`,
		class:     lineForm,
		direction: harmless,
	},
	{
		line:      ` unshare(0) = 0`,
		oracle:    `no call`,
		module:    `unshare pid "" result 0 opened "" errno "" arguments "0" []`,
		class:     lineForm,
		direction: harmless,
	},
	{
		line:      "12 openat(AT_FDCWD, \"\xff\\\xff\", O_RDONLY) = 3<\xff>",
		oracle:    `openat pid "12" result 3 opened "\xff" errno "" arguments "AT_FDCWD, \"\xff\\\xff\", O_RDONLY" [descriptor AT_FDCWD ""; path "\xff\\\xff"]`,
		module:    `openat pid "12" result 3 opened "malformed" errno "" arguments "AT_FDCWD, \"\xff\\\xff\", O_RDONLY" [descriptor AT_FDCWD ""; path "malformed"]`,
		class:     malformedText,
		direction: misKeyedPath,
	},
}
