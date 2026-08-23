// Package config decides which rules apply to which files.
//
// Without this, every rule is all-or-nothing across the whole tree, and the pressure that creates
// runs exactly backwards: the only way to accommodate one directory is to disable a rule
// everywhere, and a rule disabled everywhere is a judgment we have stopped enforcing. A narrow,
// path-scoped escape hatch is what keeps a rule enabled in the other 3,400 files. That is the same
// argument suppression comments make one line at a time, made one directory at a time.
//
// The live case is 336 findings: `consistency-require-type-suffix` firing across
// `libraries/structure/source/api/graphql/generated/`, which the gate verify replaces correctly
// stays silent on. Generated GraphQL output is not ours to name, so a rule about how we name types
// has nothing to say about it.
package config

import "strings"

// Match reports whether a slash-separated path matches a glob pattern.
//
// The supported syntax is the subset the config files actually use, implemented exactly rather than
// approximately:
//
//   - any run of characters within one path segment, never crossing a slash
//     **        any number of whole segments, including none
//     ?         exactly one character within a segment
//     {a,b,c}   alternation, which is how `*.{ts,tsx}` is written
//
// An approximation here is the dangerous kind of wrong. Treating `**` as `*` makes
// `**/generated/**/*.ts` miss every nested file and the 336 findings come back; treating `*` as
// crossing slashes makes `modules/*` swallow the whole subtree and silently scope rules off files
// nobody excluded. The first failure is loud, the second is invisible, which is why the segment
// boundary is enforced rather than assumed.
func Match(pattern string, path string) bool {
	for _, expanded := range expandBraces(pattern) {
		if matchSegments(splitPath(expanded), splitPath(path)) {
			return true
		}
	}
	return false
}

// MatchAny reports whether any pattern matches, which is how a `files` list and an `ignorePatterns`
// list are both evaluated.
func MatchAny(patterns []string, path string) bool {
	for _, pattern := range patterns {
		if Match(pattern, path) {
			return true
		}
	}
	return false
}

func splitPath(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// matchSegments walks pattern segments against path segments, recursing only at `**`.
//
// The recursion is bounded by the path length rather than unbounded, because `**` consumes zero or
// more whole segments and each recursive call consumes at least one when it advances.
func matchSegments(pattern []string, path []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			// A trailing `**` matches everything left, including nothing. This is what makes
			// `node_modules/**` match the directory itself as well as its contents.
			if len(pattern) == 1 {
				return true
			}
			// Try consuming zero segments, then one, then two, and so on.
			for index := 0; index <= len(path); index++ {
				if matchSegments(pattern[1:], path[index:]) {
					return true
				}
			}
			return false
		}

		if len(path) == 0 {
			return false
		}
		if !matchSegment(pattern[0], path[0]) {
			return false
		}

		pattern = pattern[1:]
		path = path[1:]
	}

	return len(path) == 0
}

// matchSegment matches one path segment against one pattern segment, where `*` and `?` apply but
// never cross a slash because there is no slash left to cross.
func matchSegment(pattern string, segment string) bool {
	// patternIndex and segmentIndex walk forward; the star pair remembers where to backtrack to when
	// a `*` guessed too short. This is the standard linear-backtracking match, which avoids the
	// exponential blowup a naive recursion has on a pattern like `*a*a*a*`.
	patternIndex, segmentIndex := 0, 0
	starPattern, starSegment := -1, 0

	for segmentIndex < len(segment) {
		switch {
		case patternIndex < len(pattern) && (pattern[patternIndex] == '?' || pattern[patternIndex] == segment[segmentIndex]):
			patternIndex++
			segmentIndex++
		case patternIndex < len(pattern) && pattern[patternIndex] == '*':
			starPattern = patternIndex
			starSegment = segmentIndex
			patternIndex++
		case starPattern >= 0:
			starSegment++
			segmentIndex = starSegment
			patternIndex = starPattern + 1
		default:
			return false
		}
	}

	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

// expandBraces turns `*.{ts,tsx}` into `*.ts` and `*.tsx`.
//
// Expanding up front rather than matching braces inline keeps matchSegment a plain glob matcher.
// The config's live pattern `**/generated/**/*.{ts,tsx}` is exactly this shape, and a matcher that
// ignored braces would match neither extension while looking like it worked.
//
// Nesting is handled by recursing on the expansion, so `{a,{b,c}}` resolves. Unbalanced braces are
// returned as a literal rather than an error: a pattern that does not parse should fail to match
// something, not crash a lint run.
func expandBraces(pattern string) []string {
	open := strings.IndexByte(pattern, '{')
	if open < 0 {
		return []string{pattern}
	}

	depth := 0
	closing := -1
	for index := open; index < len(pattern); index++ {
		switch pattern[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closing = index
			}
		}
		if closing >= 0 {
			break
		}
	}
	if closing < 0 {
		return []string{pattern}
	}

	prefix := pattern[:open]
	suffix := pattern[closing+1:]
	expanded := []string{}
	for _, alternative := range splitAlternatives(pattern[open+1 : closing]) {
		expanded = append(expanded, expandBraces(prefix+alternative+suffix)...)
	}
	return expanded
}

// splitAlternatives splits on commas at brace depth zero, so `{a,{b,c}}` yields `a` and `{b,c}`.
func splitAlternatives(body string) []string {
	alternatives := []string{}
	depth := 0
	start := 0
	for index := 0; index < len(body); index++ {
		switch body[index] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				alternatives = append(alternatives, body[start:index])
				start = index + 1
			}
		}
	}
	return append(alternatives, body[start:])
}
