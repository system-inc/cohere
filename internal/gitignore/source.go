package gitignore

import "fmt"

// Source is the rule that decided a path: the ignore file, relative to the repository root, the line in
// it, and the pattern as git prints it, `!` and a trailing `/` included. The zero Source means no rule
// matched.
type Source struct {
	File    string
	Line    int
	Pattern string
}

// IsZero reports whether no rule decided.
func (source Source) IsZero() bool { return source.File == "" }

// String is git check-ignore's verbose form, `file:line:pattern`.
func (source Source) String() string {
	return fmt.Sprintf("%s:%d:%s", source.File, source.Line, source.Pattern)
}
