package strace_events_test

// The ledger: every line of the in-repo corpus that Adamic reads otherwise than this package, with
// both readings, the class that explains it, and what moving onto this package changes for Adamic.
// TestEveryDifferenceFromEachConsumerIsOnItsLedger holds it to the corpus both ways: a difference
// with no row fails, and so does a row whose line no longer differs. A reading is the call a line
// finishes, field by field, then its arguments walked as the consumer walks them, each path as the
// consumer keys it (differential_test.go).
//
// What the rows say, by class:
//   - quotedArguments: Adamic reads past a quote that never closes, which strace never prints and
//     which it then reads no path from.
//   - escapedPath: Adamic decodes a quoted path and a decoded descriptor path through strconv.Unquote,
//     which refuses strace's short octal (\76, \33[, \1), and then keys the escaped spelling, a name
//     no file has.
//   - malformedText: a path holding bytes strace never writes bare. This package marks it Malformed;
//     Adamic keys the bytes as read.
//   - lineForm: Adamic reads only a bare pid at the start of a line, so a `[pid N]` line, a line with
//     no pid, a line with whitespace before it, and the resumption of a call whose first half was one
//     are all dropped, with whatever they read.

// A ledgerRow is one line a consumer reads otherwise than this package, with both readings, why
// (its class), and what it changes for that consumer (its direction).
type ledgerRow struct {
	line, oracle, module string
	class                differenceClass
	direction            direction
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
		line:      `+++ exited with 1 +++`,
		oracle:    `no call`,
		module:    `exit "1"`,
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
