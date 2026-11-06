package tailwind

import esregexp "github.com/system-inc/cohere/internal/lint/ecmascript/regexp"

// compiledMatcher is one matcher, its path compiled.
type compiledMatcher struct {
	matcherType MatcherType
	// path is nil when the matcher filters nothing, and also when its pattern does not compile, which
	// then matches no path rather than refusing the run, as an uncompilable name does (newNamePatterns).
	path    *esregexp.RegExp
	hasPath bool
	// inner is an anonymousFunctionReturn matcher's own matchers, run on what the function returns.
	inner []compiledMatcher
}

// matchesPath is upstream's check after a key or value is found: no path, or no path filter, is a
// match, and otherwise the filter must be found somewhere in the path.
func (matcher compiledMatcher) matchesPath(path string) bool {
	if path == "" || !matcher.hasPath {
		return true
	}
	if matcher.path == nil {
		return false
	}
	return matcher.path.Test(path)
}
