package gitignore

import (
	"strings"
	"testing"
)

// The glob cases gitignore(5) and fnmatch(3) describe, one per rule, so a regression names the rule it
// broke. path true is how an anchored pattern matches a path (fnmatch's FNM_PATHNAME), and false how a
// base-name pattern matches; a case whose answer differs between the two is written both ways.
func TestGlobFollowsTheDocumentedRules(test *testing.T) {
	test.Parallel()
	cases := []struct {
		pattern string
		text    string
		path    bool
		want    bool
	}{
		{"foo", "foo", true, true},
		{"foo", "fo", true, false},

		// `?` and `*` stay within one segment of a path, and cross a `/` in a base-name match.
		{"f?o", "fxo", true, true},
		{"f?o", "f/o", true, false},
		{"f?o", "f/o", false, true},
		{"?", "/", true, false},
		{"?", "/", false, true},
		{"*.go", "main.go", true, true},
		{"*.go", "a/main.go", true, false},
		{"*.go", "a/main.go", false, true},
		{"*", "a/b", true, false},
		{"*", "a/b", false, true},
		{"a*b", "a/x/b", true, false},
		{"a*b", "a/x/b", false, true},
		{"a/*/c", "a/b/c", true, true},
		{"a/*/c", "a/b/x/c", true, false},

		// A leading `**/` is any number of directories, none included, and only whole ones.
		{"**/foo", "foo", true, true},
		{"**/foo", "x/y/foo", true, true},
		{"**/foo", "xfoo", true, false},
		{"**/foo", "x/foo/y", true, false},
		{"**/foo/bar", "x/foo/bar", true, true},
		// Without a path, `**` is `*`, and the `/` after it is a literal that must be there.
		{"**/foo", "foo", false, false},
		{"**/foo", "x/y/foo", false, true},

		// A trailing `/**` is everything below, and not the directory itself.
		{"abc/**", "abc/x", true, true},
		{"abc/**", "abc/x/y", true, true},
		{"abc/**", "abc", true, false},
		{"x/**", "y/x/z", true, false},

		// `/**/` in the middle is one slash or any directories between, and two of them are no different.
		{"a/**/b", "a/b", true, true},
		{"a/**/b", "a/x/b", true, true},
		{"a/**/b", "a/x/y/b", true, true},
		{"a/**/b", "a/xb", true, false},
		{"a/**/b", "a/b", false, false},
		{"a/**/b", "a/x/y/b", false, true},
		{"a/**/**/b", "a/b", true, true},
		{"a/**/**/b", "a/x/b", true, true},
		{"a/**/**/b", "a/x/y/z/b", true, true},
		{"a/**/**/b", "a/xb", true, false},
		{"a/**/**/b", "a/b", false, false},

		// `**` that is not a whole segment is one `*`, which stays within a segment of a path.
		{"foo**/bar", "foo/bar", true, true},
		{"foo**/bar", "foox/bar", true, true},
		{"foo**/bar", "foo/x/bar", true, false},
		{"**foo", "x/foo", true, false},
		{"a**b", "axyb", true, true},
		{"a**b", "ax/yb", true, false},
		{"a**b", "ax/yb", false, true},
		{"a/**b", "a/xb", true, true},
		{"a/**b", "a/x/b", true, false},
		{"a/**b", "a/x/b", false, true},
		{"a/b**", "a/bx/y", true, false},
		{"a/b**", "a/bx/y", false, true},

		// Bracket expressions: members, `!` or `^` to negate, ranges.
		{"[abc]", "b", true, true},
		{"[!abc]", "b", true, false},
		{"[!abc]", "d", true, true},
		{"[^abc]", "d", true, true},
		{"[^abc]", "a", true, false},
		{"[a-c]x", "bx", true, true},
		{"[a-c]x", "dx", true, false},
		// `]` first is a member, and may start a range: `]` to `a` holds `_`.
		{"[]]", "]", true, true},
		{"[]-a]", "_", true, true},
		{"[]-a]", "b", true, false},
		// A `-` before the closing `]` is a member, so `[%-]` is a set of two and the last `]` a literal.
		{"[a-]", "-", true, true},
		{"[a-]", "b", true, false},
		{"[%-]]", "-]", true, true},
		{"[%-]]", "&]", true, false},
		// A range ends in `]` only escaped; `W` to `]` holds `[`.
		{`[W-\]]`, "[", true, true},
		{`[W-\]]`, "^", true, false},
		// A range may end in `-`: `+` to `-` holds `,`.
		{"[+--]", ",", true, true},
		{"[+--]", ".", true, false},
		{"[/]", "/", true, false},
		{"[/]", "/", false, true},

		// Every class fnmatch names, in the C locale, one member and one byte outside it.
		{"[[:alnum:]]", "a", true, true},
		{"[[:alnum:]]", "5", true, true},
		{"[[:alnum:]]", "_", true, false},
		{"[[:alpha:]]", "Z", true, true},
		{"[[:alpha:]]", "5", true, false},
		{"[[:blank:]]", " ", true, true},
		{"[[:blank:]]", "\t", true, true},
		{"[[:blank:]]", "\n", true, false},
		{"[[:cntrl:]]", "\x01", true, true},
		{"[[:cntrl:]]", "\x7F", true, true},
		{"[[:cntrl:]]", "a", true, false},
		{"[[:digit:]]", "7", true, true},
		{"[[:digit:]]", "x", true, false},
		{"[[:graph:]]", "!", true, true},
		{"[[:graph:]]", " ", true, false},
		{"[[:lower:]]", "q", true, true},
		{"[[:lower:]]", "Q", true, false},
		{"[[:print:]]", " ", true, true},
		{"[[:print:]]", "\x7F", true, false},
		{"[[:punct:]]", "_", true, true},
		{"[[:punct:]]", "~", true, true},
		{"[[:punct:]]", "a", true, false},
		{"[[:space:]]", " ", true, true},
		{"[[:space:]]", "\n", true, true},
		{"[[:space:]]", "\r", true, true},
		{"[[:space:]]", "x", true, false},
		// Git's space class is these four alone, without C's vertical tab and form feed; git 2.54 agrees.
		{"[[:space:]]", "\v", true, false},
		{"[[:space:]]", "\f", true, false},
		{"[[:upper:]]", "A", true, true},
		{"[[:upper:]]", "a", true, false},
		{"[[:xdigit:]]", "f", true, true},
		{"[[:xdigit:]]", "F", true, true},
		{"[[:xdigit:]]", "g", true, false},
		{"[[:nosuchclass:]]", "x", true, false},

		// A class is one member among others, and negates with them.
		{"[[:digit:]ab]", "a", true, true},
		{"[[:digit:]ab]", "5", true, true},
		{"[[:digit:]ab]", "c", true, false},
		{"[x[:upper:]]", "x", true, true},
		{"[x[:upper:]]", "Q", true, true},
		{"[x[:upper:]]", "y", true, false},
		{"[![:alpha:]]", "5", true, true},
		{"[![:alpha:]]", "x", true, false},
		// Outside a bracket expression `[:alpha:]` is no class, only a set of its own letters.
		{"[:alpha:]", "l", true, true},

		// A bracket that never closes makes the pattern match nothing, itself included.
		{"[abc", "[abc", true, false},
		{"[[:alpha:]", "a", true, false},
		{"a[", "a[", true, false},

		// A backslash makes the next byte literal, inside a bracket expression too.
		{`\*`, "*", true, true},
		{`\*`, "x", true, false},
		{`\?`, "?", true, true},
		{`\?`, "x", true, false},
		{`\[a]`, "[a]", true, true},
		{`\[a]`, "a", true, false},
		{`\\`, `\`, true, true},
		{`\a`, "a", true, true},
		{`[\]]`, "]", true, true},
		{`[\!a]`, "!", true, true},
		{`[\!a]`, "b", true, false},
		// An escaped `-` is a member, not a range.
		{`[a\-z]`, "-", true, true},
		{`[a\-z]`, "b", true, false},

		// A trailing backslash escapes nothing, so the pattern matches nothing.
		{`foo\`, "foo", true, false},
		{`*\`, `a\`, true, false},
		{`[a\`, `[a\`, true, false},

		{"*a*a*a*a*a*a*a*a*b", strings.Repeat("a", 200), true, false},
	}
	for _, testCase := range cases {
		compiled := compileGlob(testCase.pattern, testCase.path)
		if got := compiled.matches(testCase.text, testCase.path); got != testCase.want {
			test.Errorf(
				"%q against %q (path %v): got %v, want %v",
				testCase.pattern,
				testCase.text,
				testCase.path,
				got,
				testCase.want,
			)
		}
	}
}
