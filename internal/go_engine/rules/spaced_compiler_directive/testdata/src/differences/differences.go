// Package differences holds the comments where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same comments of the same files. A line directive moves the columns
// after it, which the check reads unmoved; a directive may sit in the middle of a comment group; and the message quotes
// the comment, quotes and backslashes included.
package differences

// Found by both: the second line of a comment group.
// go:noinline
func secondLine() {}

// Found by both: quotes and a backslash, which the message escapes.

// go:generate echo "a\b"
func quoted() {}

//line moved.go:20:9
// Found by both: the line directive above moves this file's columns, and the check reads them unmoved.

// go:noinline
func afterLineDirective() {}
