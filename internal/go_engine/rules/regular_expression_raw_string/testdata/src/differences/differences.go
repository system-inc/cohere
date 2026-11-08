// Package differences holds the shapes where the port and the original could most plausibly part, and records none,
// since they part nowhere.
package differences

import "regexp"

type shadow struct{}

func (shadow) MustCompile(pattern string) string { return pattern }

type holder struct {
	pattern *regexp.Regexp
}

// patterns is calls inside a composite literal of a package's variable.
var patterns = []*regexp.Regexp{
	regexp.MustCompile("\\d"),        // found
	regexp.MustCompile("\\\\\\\\"),   // found, four escaped backslashes
	regexp.MustCompile("\\\\\\\\\n"), // an escape after four escaped backslashes
}

func generic[T any](value T) *regexp.Regexp {
	_ = value
	return regexp.MustCompile("\\w") // found inside a generic function
}

func shadowed() {
	// A local named regexp hides the package, so its MustCompile is a method.
	regexp := shadow{}
	regexp.MustCompile("\\.")
}

func shapes() {
	_ = holder{pattern: regexp.MustCompile("\\s")} // found in a field's value
	_, _ = regexp.Compile("\\\"")                  // an escaped quote after an escaped backslash
	_, _ = regexp.Compile("\"\\")                  // an escaped quote before an escaped backslash
	_ = generic(1)
	_ = patterns
}
