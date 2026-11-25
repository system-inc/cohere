package core

import esregexp "github.com/system-inc/cohere/internal/typescript_language/ecmascript/regexp"

// ignoredParameterNames is the compiled form of the two allowance options.
type ignoredParameterNames struct {
	literal  map[string]bool
	patterns []*esregexp.RegExp
}

// matches reports whether a parameter's property writes are excused, telling overrun of a pattern that ran
// past the time bound.
func (ignored ignoredParameterNames) matches(name string, overrun esregexp.Overrun) bool {
	if ignored.literal[name] {
		return true
	}
	for _, pattern := range ignored.patterns {
		// A match that overruns the time bound excuses too: no answer here is a report.
		if pattern.TestOrTimeout(name, overrun) {
			return true
		}
	}
	return false
}
