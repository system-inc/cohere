// Package regexp compiles a JavaScript regexp the way JavaScript reads it.
// Import it as esregexp, so the standard library's regexp keeps its own name.
//
// A rule ported from ESLint is handed patterns its users wrote as JavaScript
// regexps, in rule options and in `new RegExp` calls in the source under lint.
// Neither cohere-go answers about those the way JavaScript does:
//
//   - The standard library's regexp is RE2, which has no lookahead, no
//     lookbehind and no backreferences. A pattern using any of them does not
//     compile at all, and a rule that shrugs off the compile error silently
//     drops whatever option it came from.
//   - regexp2 backtracks and so takes the syntax, and its ECMAScript mode gets
//     most of the semantics right. What it still reads differently:
//     a `.` matches U+2028 and U+2029; `^` and `$` under `m` break on `\n`
//     alone where JavaScript breaks on all four line terminators; ignoring
//     case folds by Unicode's rules rather than JavaScript's; `\b` is drawn
//     over .NET's Unicode word set where ECMAScript keeps it to ASCII; an
//     escape .NET reads for itself (`\A`, `\a`, `\e`) is a plain character
//     to JavaScript, and one .NET does not know at all makes it refuse a
//     pattern JavaScript accepts; and `y` says where a match may start, which
//     regexp2 has no flag for.
//
// This package takes the second one and closes every one of those gaps by
// rewriting the pattern before handing it over. Unicode property escapes use
// cohere-generated Unicode 17.0.0 tables, including exact ECMAScript aliases. It also bounds how long a
// match may run: a backtracking matcher on a pattern a user wrote is a way to
// hang the linter. A match that overruns has no answer, so the caller says
// which answer stands in for it, and is told the pattern overran so the run
// can name the gap rather than read the stand-in as the pattern's.
//
// Three things are deliberately not covered:
//
//   - The `v` flag's set syntax, which Compile refuses outright.
//   - Comparing a backreference without regard to case. JavaScript uses the same
//     canonicalization as for literals; the fallback uses regexp2's case-insensitivity.
//   - Exhaustive syntax validation under `u`. The `u` flag makes JavaScript
//     reject constructs it otherwise tolerates (a bare `]` outside a class is
//     the usual one) and those still compile here. Every pattern JavaScript
//     accepts is read correctly; some it rejects are read rather than refused.
package regexp

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/dlclark/regexp2/v2"
)

// MatchTimeout bounds how long one subject may spend in one pattern. A pattern
// written by a user can backtrack exponentially (`(a|aa)+$` against a long
// run of `a` is the shape of it) and a linter that hangs on a config typo is
// worse than one that answers no.
//
// The bound is wall time because it is all regexp2 can count: it has no step or
// backtrack budget, so a pattern's own work cannot be told apart from the
// machine's load. So it sits far above what any real match takes. `Base`
// against `XBaseY` crossed the one second this used to be in a `-race` run on a
// busy box (#s4dy4e0), by regexp2's stale clock rather than by the match (see
// overranSpuriously). With that handled, the same match settled in under a
// millisecond under -race, 128 at once, and ten seconds is over ten thousand
// times that. A pattern still running after it is pathological, and the caller
// names it as a gap.
const MatchTimeout = 10 * time.Second

// Overrun is told of a pattern that ran past MatchTimeout, as JavaScript writes
// it, so the caller can say the answer that stood in was not the pattern's. A
// lint rule passes its context's PatternTimedOut, which names the gap in the
// run's footer and its --json. Nil drops the news, for a caller with nowhere to
// send it, which says why where it passes nil.
type Overrun func(pattern string)

// ErrUnsupportedFlag is returned for a flag this package does not implement.
var ErrUnsupportedFlag = errors.New("unsupported regexp flag")

// ErrUnsupportedSyntax is returned for syntax JavaScript itself rejects.
var ErrUnsupportedSyntax = errors.New("unsupported regexp syntax")

// RegExp is a compiled JavaScript regexp.
type RegExp struct {
	source string
	flags  string
	re     *regexp2.Regexp
	// exact is false when the pattern fell back to regexp2's own
	// case-insensitivity, which is close to JavaScript's but not identical.
	exact bool
}

type flagSet struct {
	ignoreCase bool
	multiline  bool
	dotAll     bool
	unicode    bool
	sticky     bool
}

// parseFlags reads a JavaScript flag string. `g` and `d` say how a match is
// driven rather than what matches, so they are accepted and ignored. `y` does
// change what matches and is kept.
func parseFlags(flags string) (flagSet, error) {
	var set flagSet
	seen := map[rune]bool{}
	for _, flag := range flags {
		if seen[flag] {
			return set, fmt.Errorf("%w: %q given twice", ErrUnsupportedFlag, flag)
		}
		seen[flag] = true
		switch flag {
		case 'i':
			set.ignoreCase = true
		case 'm':
			set.multiline = true
		case 's':
			set.dotAll = true
		case 'u':
			set.unicode = true
		case 'y':
			set.sticky = true
		case 'g', 'd':
		case 'v':
			return set, fmt.Errorf("%w: %q", ErrUnsupportedFlag, flag)
		default:
			return set, fmt.Errorf("%w: %q", ErrUnsupportedFlag, flag)
		}
	}
	return set, nil
}

// Compile compiles source under the given JavaScript flags, as
// `new RegExp(source, flags)` would. Pass "" for no flags.
func Compile(source string, flags string) (*RegExp, error) {
	set, parseFlagsError := parseFlags(flags)
	if parseFlagsError != nil {
		return nil, parseFlagsError
	}

	rewritten, exact, rewriteError := rewrite(source, rewriteOptions{
		multiline:  set.multiline,
		dotAll:     set.dotAll,
		ignoreCase: set.ignoreCase,
		unicode:    set.unicode,
	})
	if rewriteError != nil {
		return nil, rewriteError
	}
	if set.sticky {
		// A sticky regexp matches only at `lastIndex`, which is 0 for one just
		// compiled and never moves here, since nothing this package exposes
		// advances it. So it anchors at the start: around the whole pattern,
		// or a top-level `|` would anchor only its first branch.
		rewritten = inputStart + `(?:` + rewritten + `)`
	}

	// Multiline, Singleline and IgnoreCase are all handled by the rewrite, so
	// regexp2 is never told about them: being told twice would undo the work.
	options := regexp2.RegexOptions(regexp2.ECMAScript)
	if set.unicode {
		options |= regexp2.Unicode
	}
	if set.ignoreCase && !exact {
		options |= regexp2.IgnoreCase
	}

	re, compileError := regexp2.Compile(rewritten, options)
	if compileError != nil {
		// regexp2 quotes back the pattern it was handed, which is the
		// rewritten one. A message naming `[Oo][Kk]` where its author wrote
		// `ok` is no help, so the source goes back in as written.
		return nil, errors.New(strings.ReplaceAll(compileError.Error(), rewritten, source))
	}
	re.MatchTimeout = MatchTimeout

	return &RegExp{source: source, flags: flags, re: re, exact: exact}, nil
}

// MustCompile is Compile for a pattern written in this repository rather than
// read from a user, and panics rather than returning an error.
func MustCompile(source string, flags string) *RegExp {
	re, compileError := Compile(source, flags)
	if compileError != nil {
		// Invariant: every caller passes a pattern literal written in this repository, compiled when its
		// package initializes, so a failure stops the first test run and is never a user's input.
		panic(fmt.Sprintf("esregexp: cannot compile /%s/%s: %v", source, flags, compileError))
	}
	return re
}

// Test reports whether the regexp matches anywhere in `text`, as
// `RegExp.prototype.test` does, for a caller whose yes answer is what produces a
// report: a restricting pattern, or one that picks what a rule checks.
//
// A match that overruns MatchTimeout counts as no match, the answer that reports
// nothing, and overrun is told: a finding that may have been missed is the run's
// gap, never a clean answer.
func (exp *RegExp) Test(text string, overrun Overrun) bool {
	matched, testOrError := exp.TestOrError(text)
	if testOrError != nil {
		exp.overran(overrun)
		return false
	}
	return matched
}

// TestOrTimeout is Test for a caller whose no answer is what produces a
// report: an allow-pattern, an ignore-pattern, a pattern a name is required to
// match. Reading a bound match as no match would turn a skipped report into a
// false positive there, so a match that overruns MatchTimeout counts as a
// match, the report is skipped, and overrun is told.
func (exp *RegExp) TestOrTimeout(text string, overrun Overrun) bool {
	matched, testOrError := exp.TestOrError(text)
	if testOrError != nil {
		exp.overran(overrun)
		return true
	}
	return matched
}

// TestOrError is Test for a caller that has to tell "no match" apart from "the
// match ran into MatchTimeout", and that has somewhere other than a match to
// send the second answer.
func (exp *RegExp) TestOrError(text string) (bool, error) {
	if exp == nil || exp.re == nil {
		return false, nil
	}
	began := time.Now()
	for {
		matched, matchStringError := exp.re.MatchString(text)
		if !exp.overranSpuriously(matchStringError, began) {
			return matched, matchStringError
		}
	}
}

// ExecOrError is TestOrError for a caller that needs where the first match is
// and what it holds, as `RegExp.prototype.exec` returns it, rather than only
// whether there is one. Nil with no error is no match.
func (exp *RegExp) ExecOrError(text string) (*regexp2.Match, error) {
	if exp == nil || exp.re == nil {
		return nil, nil
	}
	began := time.Now()
	for {
		match, findStringMatchError := exp.re.FindStringMatch(text)
		if !exp.overranSpuriously(findStringMatchError, began) {
			return match, findStringMatchError
		}
	}
}

// overranSpuriously reports whether regexp2 said a match ran into its timeout
// when it has not run that long, after yielding so the next attempt meets a
// clock that has moved.
//
// regexp2 reads its deadline off a clock a background goroutine ticks every
// tenth of a second, and two ways that clock runs behind hand a match a
// deadline already past, which times out a trivial pattern on its first step.
// One is callers racing to restart the clock after it went idle, where one
// keeps the reading from before the idle spell (dlclark/regexp2#117, fixed
// upstream by v2.13.1, past the v2.5.2 this module takes). The other is the
// ticking goroutine starved by the scheduler under load. So an overrun is
// believed only once the bound has truly passed since the match began, measured
// on Go's monotonic clock. Before that the match is asked again, its deadline
// now drawn from the clock the first attempt woke. A pathological pattern still
// stops: by the bound when the clock is right, by at most twice it when a stale
// clock cut the first attempt short.
//
// regexp2's only error from a match is its timeout, so any error is read as one.
func (exp *RegExp) overranSpuriously(matchError error, began time.Time) bool {
	if matchError == nil || time.Since(began) >= exp.re.MatchTimeout {
		return false
	}
	runtime.Gosched()
	return true
}

// overran tells overrun, when there is one, that this pattern ran past its bound.
func (exp *RegExp) overran(overrun Overrun) {
	if overrun != nil {
		overrun(exp.String())
	}
}

// Source returns the pattern as it was written, the way `RegExp.prototype
// .source` does, not the rewritten form handed to regexp2.
func (exp *RegExp) Source() string { return exp.source }

// Flags returns the flags as they were written.
func (exp *RegExp) Flags() string { return exp.flags }

// String renders the regexp the way JavaScript writes a regexp literal.
func (exp *RegExp) String() string { return "/" + exp.source + "/" + exp.flags }

// Unwrap returns the compiled regexp2 pattern, for a caller that needs
// capture groups or replacement. The pattern it holds is the rewritten one, so
// its own String does not match Source.
func (exp *RegExp) Unwrap() *regexp2.Regexp {
	if exp == nil {
		return nil
	}
	return exp.re
}
