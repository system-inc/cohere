package tailwind

import (
	"strings"

	esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"
)

// namePatterns is a selector's name patterns, compiled. Its answers are remembered by the selector
// group that asks, once for all of the group's selectors.
type namePatterns struct {
	compiled  []*esregexp.RegExp
	lowercase bool
}

// matchesUnremembered reports whether any pattern matches the whole name, upstream's matchesName,
// asking the patterns every time.
func (patterns *namePatterns) matchesUnremembered(name string) bool {
	if name == "" || len(patterns.compiled) == 0 {
		return false
	}
	compared := name
	if patterns.lowercase {
		compared = strings.ToLower(name)
	}
	for _, pattern := range patterns.compiled {
		// The first match, and only if it is the name: `exec(name)[0] === name`. A pattern whose
		// first match is shorter does not match, though a longer match exists, exactly as upstream.
		match, findStringMatchError := pattern.Unwrap().FindStringMatch(compared)
		if findStringMatchError == nil && match != nil && match.RuneIndex == 0 && match.String() == compared {
			return true
		}
	}
	return false
}
