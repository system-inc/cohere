// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the pattern
// among several values, and a regexp function held in a variable, whose callee is known only once the variable is
// lifted, or not at all when two functions meet. The port and the original find the same here, so the fixture records
// no difference.
package differences

import "regexp"

func patternAndInput() (string, string) {
	return "(", ""
}

func patternAndBytes() (string, []byte) {
	return "(", nil
}

func statements() {
	defer regexp.MustCompile(`(`)
	go regexp.MustCompile(`[`)
	defer func() {
		regexp.MustCompile(`)`)
	}()
}

func multipleValues() {
	regexp.MatchString(patternAndInput())
	regexp.Match(patternAndBytes())
}

func lifted(condition bool) {
	compile := regexp.Compile
	if condition {
		compile = regexp.Compile
	}
	compile(`(`)
	choose := regexp.MustCompile
	if condition {
		choose = regexp.MustCompilePOSIX
	}
	choose(`(`)
}
