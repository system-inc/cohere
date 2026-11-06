package core

import esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"

// ignoredParameterNames is the compiled form of the two allowance options.
type ignoredParameterNames struct {
	literal  map[string]bool
	patterns []*esregexp.RegExp
}

// matches reports whether a parameter's property writes are excused.
func (ignored ignoredParameterNames) matches(name string) bool {
	if ignored.literal[name] {
		return true
	}
	for _, pattern := range ignored.patterns {
		// A match that overruns the time bound excuses too: no answer here is a report.
		if pattern.TestOrTimeout(name) {
			return true
		}
	}
	return false
}
