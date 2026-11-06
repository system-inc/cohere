package core

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/ecmascript/reference"
	"github.com/system-inc/cohere/internal/lint/ecmascript/regex_syntax"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// regExpCallTraceMap asks the tracker for every call and construction of the global RegExp.
var regExpCallTraceMap = map[string]*reference.TraceMap{"RegExp": {Call: true, Construct: true}}

// NoUselessBackreference flags a backreference that can never match the group it names.
//
//	valid:   /(a)\1/
//	valid:   /(?<foo>a)\k<foo>/
//	valid:   /(?:a|(b))\1/
//	invalid: /\1(a)/
//	invalid: /(a|\1b)/
//	invalid: /(a\1)/
//	invalid: /\1(?!(a))/
//	invalid: /(?<=(a)\1)b/
//
// A backreference names a group and asks for the text that group captured. When the group cannot
// have captured anything at the moment the reference is evaluated, the reference matches the empty
// string instead of failing, so the pattern silently matches more than its author wrote it to
// match. That is the defect: not a syntax error the engine refuses, but a pattern that quietly
// means something else.
//
// # Five ways a group cannot have run, and they are five findings
//
// It reports five ids, ESLint's, since the kind is the diagnosis rather than decoration. A port collapsing
// them to one id passes a fixture that asserts only that something fired, while calling a
// different-alternative case a forward reference.
//
//	nested                  /(a\1)/       the group is still open, so it has captured nothing
//	disjunctive             /(a|\1b)/     only one branch runs, so the other never captured
//	forward                 /\1(a)/       matching is left to right and the group has not run
//	backward                /(?<=(a)\1)b/ a lookbehind matches right to left, so this is early
//	intoNegativeLookaround  /\1(?!(a))/   the group only runs on a path that fails
//
// oxc names the middle pair the other way round: its `Problem::Forward` is the lookbehind case. The
// ids here are ESLint's (#jjfa7qb), where forward is the ordinary left-to-right case.
//
// oxc's doc comment also lists `/(?<=\1(a))b/` as incorrect, and that exact pattern is a pass case in
// both corpora, correctly: a lookbehind matches right to left, so the group is reached before the
// reference and has captured.
//
// # Why this is not built on regex_pattern.Walk
//
// `Walk` reports characters with a quantifier and class depth, and its own doc says a caller
// wanting alternation or lookaround should extend it rather than find it half-built. This rule is
// entirely about alternation and lookaround: every one of the five findings above is a question
// about which construct encloses the reference and which encloses the group. `Walk` also reports
// `ClassDepth` without saying which class, and does not recurse into v-flag nested classes, so the
// one thing it could have answered here is answered wrong for `new RegExp('[[]\\1](a)', 'v')`.
//
// So this scans with `regex_syntax.SkipPatternEscape` and `regex_syntax.ClassEnd`, the same two
// primitives `regex_pattern.CountCapturingGroups` is built on, and keeps a path of enclosing nodes.
// `ClassEnd` handles v-flag nesting and `\q{...}`, which is what makes the two class cases in the
// corpus come out right without a second scanner.
//
// # Where a finding points, and which calls are read
//
// Each finding names the whole regex literal, or the whole `RegExp(...)` or `new RegExp(...)` call,
// as ESLint reports it (#jjfa7qb). The message names the backreference and the group, so pointing at
// the node loses nothing a reader needs, and it keeps one place per pattern however many references
// in it are useless.
//
// Every regex literal is checked under its own flags, including one passed to `RegExp`. The calls are
// found the way ESLint finds them, through the shelf's port of eslint-utils' ReferenceTracker: a call
// or construction of the global `RegExp`, directly or through anything the tracker follows, so a
// local named `RegExp` is not it and `const r = RegExp; new r(...)` is. The pattern and the flags are
// read as getStringIfConstant reads them, through reference.ConstantStringIn: a constant binding's
// value counts, and flags that are not constant read as none, as ESLint's `flags || ""` does. A
// pattern that is not constant is not checked.
var NoUselessBackreference = rule.Rule[rule.NoOptions]{
	Name: "no-useless-backreference",

	// The tracker tells the global `RegExp` from a local that shares its spelling by asking where its
	// symbol is declared, and a constant argument is followed to its binding the same way.
	NeedsTypeChecker: true,
	TypeReach:        rule.TypeReachShapes,

	Run: func(ruleContext rule.Context, options rule.Given[rule.NoOptions]) rule.Listeners {
		return rule.Listeners{
			ast.KindRegularExpressionLiteral: func(node *ast.Node) {
				pattern, flags := regex_syntax.PatternAndFlags(node.Text())
				if pattern == "" {
					return
				}
				reportUselessBackreferences(ruleContext, node, pattern, flags)
			},
			ast.KindSourceFile: func(node *ast.Node) {
				// Every trace starts at a reference to `RegExp`, so a file that never spells it has
				// nothing to follow, and the tracker's index is never built.
				if ruleContext.TypeChecker == nil || !strings.Contains(ruleContext.SourceFile.Text(), "RegExp") {
					return
				}
				tracker := reference.NewTracker(ruleContext.SourceFile, ruleContext.TypeChecker, nil)
				for _, tracked := range tracker.GlobalReferences(regExpCallTraceMap) {
					checkBackreferencesInRegExpCall(ruleContext, tracked.Node)
				}
			},
		}
	},
}

// checkBackreferencesInRegExpCall checks a call or construction of RegExp whose pattern is a constant.
func checkBackreferencesInRegExpCall(ruleContext rule.Context, call *ast.Node) {
	arguments := call.Arguments()
	if len(arguments) == 0 {
		return
	}
	pattern, isConstant := reference.ConstantStringIn(ruleContext, arguments[0])
	if !isConstant {
		return
	}
	flags := ""
	if len(arguments) > 1 {
		flags, _ = reference.ConstantStringIn(ruleContext, arguments[1])
	}
	reportUselessBackreferences(ruleContext, call, pattern, flags)
}

// regexNodeKind is what a node on a backreference's or group's enclosing path is.
type regexNodeKind int

const (
	// regexNodeAlternative is one branch of a `|` disjunction. Two paths diverging at an
	// alternative is exactly the different-branch case.
	regexNodeAlternative regexNodeKind = iota
	// regexNodeCapturingGroup is `(...)` or `(?<name>...)`.
	regexNodeCapturingGroup
	// regexNodeGroup is `(?:...)`, which nests but captures nothing.
	regexNodeGroup
	// regexNodeLookahead is `(?=...)` or `(?!...)`.
	regexNodeLookahead
	// regexNodeLookbehind is `(?<=...)` or `(?<!...)`, which matches right to left.
	regexNodeLookbehind
)

// regexNode is one enclosing construct in the pattern.
type regexNode struct {
	kind regexNodeKind
	// negative distinguishes `(?!` from `(?=` and `(?<!` from `(?<=`. A group inside a negative
	// lookaround only ever runs on a path that then fails, so it never keeps a capture.
	negative bool
}

// capturingGroup is a group the pattern declares, with where it sits and what encloses it.
type capturingGroup struct {
	name  string
	start int
	end   int
	// path is the chain of enclosing node indices, outermost first, ending at the group itself.
	path []int
}

// backreference is a `\1` or `\k<name>` in the pattern.
type backreference struct {
	// index is the group number for `\1`, or zero when the reference is by name.
	index int
	name  string
	start int
	end   int
	path  []int
}

// regexStructure is everything the rule needs about one pattern.
type regexStructure struct {
	nodes          []regexNode
	groups         []capturingGroup
	backreferences []backreference
}

var (
	problemInsideItsOwnGroup = backreferenceProblem{
		id:     "nested",
		reason: "encloses the reference and so has captured nothing yet when it is reached",
	}
	problemAnotherAlternative = backreferenceProblem{
		id:     "disjunctive",
		reason: "sits in a different branch of the same disjunction, and only one branch ever runs",
	}
	problemBeforeItsGroup = backreferenceProblem{
		id:     "forward",
		reason: "appears later in the pattern, and matching runs left to right",
	}
	problemAfterItsGroupInLookbehind = backreferenceProblem{
		id:     "backward",
		reason: "appears earlier in a lookbehind, which matches right to left",
	}
	problemIntoNegativeLookaround = backreferenceProblem{
		id:     "intoNegativeLookaround",
		reason: "sits in a negative lookaround, so it only runs on a path that then fails",
	}
)

// reportUselessBackreferences scans one pattern and reports every backreference that cannot match, at
// the node the pattern came from.
func reportUselessBackreferences(ruleContext rule.Context, node *ast.Node, pattern string, flagsText string) {
	flags := regex_syntax.ParseRegexFlags(flagsText)

	structure, ok := scanRegexStructure(pattern, flags)
	if !ok {
		return
	}
	if len(structure.backreferences) == 0 {
		return
	}

	// The group total decides whether `\1` is a backreference at all. `/\1/` has no groups, so the
	// escape is an octal, not a reference, and upstream is silent on it. This has to be the total
	// rather than a running count, because a pattern may reference a group declared later.
	groupCount := len(structure.groups)

	// A pattern the engine would refuse is not this rule's business. Upstream parses first and
	// reports nothing when the parse fails, so `new RegExp('\\1(a){', 'u')` is silent while
	// `RegExp('\\1(a){', flags)` reports: the brace is a syntax error under `u` and a literal
	// without it, and the second call's flags are unknown so no `u` is assumed. Four of upstream's
	// clean cases are this and nothing else, and a scanner permissive enough to walk the pattern
	// anyway reports a real backreference sitting next to the broken part.
	if !patternIsWellFormed(pattern, structure, groupCount, flags) {
		return
	}

	for _, reference := range structure.backreferences {
		groups := resolveBackreference(reference, structure.groups, groupCount)
		if len(groups) == 0 {
			continue
		}

		// Every group the reference names is judged, and one the reference can reach clears it. Of
		// the problems left, those in the reference's own disjunction are reported if there are any,
		// else all of them: the first is named and the rest are counted, as ESLint words it.
		type judged struct {
			problem backreferenceProblem
			group   capturingGroup
		}
		var all []judged
		var sameDisjunction []judged
		reachable := false
		for _, group := range groups {
			problem, ok := classifyBackreference(reference, group, structure.nodes)
			if !ok {
				reachable = true
				break
			}
			all = append(all, judged{problem, group})
			if problem.id != problemAnotherAlternative.id {
				sameDisjunction = append(sameDisjunction, judged{problem, group})
			}
		}
		if reachable {
			continue
		}
		toReport := all
		if len(sameDisjunction) > 0 {
			toReport = sameDisjunction
		}
		first := toReport[0]
		ruleContext.ReportNode(node, rule.Message{
			Id: first.problem.id,
			Description: first.problem.describe(pattern[reference.start:reference.end],
				pattern[first.group.start:first.group.end], len(toReport)-1),
		})
	}
}

// patternIsWellFormed reports whether the engine would accept the pattern.
//
// This is not a full validator and does not try to be. It answers the three ways a pattern in this
// corpus is refused, all of which only bite under `u` or `v`, where the grammar stops tolerating
// what annex B allows:
//
//	a numeric reference past the last group   `\1(a)\2` has one group, so `\2` is not an escape
//	a named reference naming no group         `\k<bar>` with no `bar` group
//	a lone `{` that opens no quantifier       `\1(a){`
//
// A named reference with no matching group is refused under every flag once the pattern declares
// any named group at all, which is why `new RegExp('\\k<foo>(?<foo>a)\\k<bar>')` is clean with no
// flags while `RegExp('\\1(a)\\k<foo>', 'u')` needs the `u` to be refused.
func patternIsWellFormed(pattern string, structure regexStructure, groupCount int, flags regex_syntax.RegexFlags) bool {
	hasNamedGroup := false
	for _, group := range structure.groups {
		if group.name != "" {
			hasNamedGroup = true
			break
		}
	}

	for _, reference := range structure.backreferences {
		if reference.name != "" {
			// A `\k<name>` naming nothing is a syntax error whenever the pattern has any named
			// group, and under `u` or `v` even when it has none.
			named := false
			for _, group := range structure.groups {
				if group.name == reference.name {
					named = true
					break
				}
			}
			if !named && (hasNamedGroup || flags.UnicodeMode()) {
				return false
			}
			continue
		}
		// Under `u` or `v` a numeric escape with no such group is not a legacy octal, it is an
		// error. Without the flag it is an octal and the rule is right to ignore it.
		if flags.UnicodeMode() && reference.index > groupCount {
			return false
		}
	}

	if flags.UnicodeMode() && hasUnmatchedQuantifierBrace(pattern, flags) {
		return false
	}
	return true
}

// hasUnmatchedQuantifierBrace reports whether the pattern holds a `{` that opens no valid
// quantifier, which annex B reads as a literal brace and `u` mode refuses.
func hasUnmatchedQuantifierBrace(pattern string, flags regex_syntax.RegexFlags) bool {
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '\\':
			step, ok := regex_syntax.SkipPatternEscape(pattern, index, flags)
			if !ok {
				return true
			}
			index += step
		case '[':
			end, ok := regex_syntax.ClassEnd(pattern, index, flags)
			if !ok {
				return true
			}
			index = end
		case '{':
			if !opensQuantifier(pattern, index) {
				return true
			}
			index++
		default:
			index++
		}
	}
	return false
}

// opensQuantifier reports whether the `{` at index begins `{n}`, `{n,}` or `{n,m}`.
func opensQuantifier(pattern string, index int) bool {
	cursor := index + 1
	digits := 0
	for cursor < len(pattern) && pattern[cursor] >= '0' && pattern[cursor] <= '9' {
		cursor++
		digits++
	}
	if digits == 0 {
		return false
	}
	if cursor < len(pattern) && pattern[cursor] == ',' {
		cursor++
		for cursor < len(pattern) && pattern[cursor] >= '0' && pattern[cursor] <= '9' {
			cursor++
		}
	}
	return cursor < len(pattern) && pattern[cursor] == '}'
}

// resolveBackreference finds the group a reference names.
//
// A numeric reference past the group count is not a reference at all but a legacy octal escape,
// which is why `/\2(a)/` and `/\11(a)/` are both silent. A named reference with no matching group
// is a syntax error the engine refuses, so it is skipped rather than reported.
//
// A name can be declared more than once under ES2025 duplicate named groups, where
// `(?<foo>a)|(?<foo>b)` is legal because the two live in different branches. Upstream reports such
// a reference only when **every** group carrying the name is unreachable from it, which is why
// `/((?<foo>bar)\k<foo>|(?<foo>baz))/` is clean: one of the two `foo` groups does run first. So a
// named reference resolves to every group carrying its name, in pattern order.
func resolveBackreference(reference backreference, groups []capturingGroup, groupCount int) []capturingGroup {
	if reference.name == "" {
		if reference.index < 1 || reference.index > groupCount {
			return nil
		}
		return groups[reference.index-1 : reference.index]
	}
	var named []capturingGroup
	for _, group := range groups {
		if group.name == reference.name {
			named = append(named, group)
		}
	}
	return named
}

// classifyBackreference decides which of the five problems a reference has, or that it has none.
//
// The order matters and matches upstream. Containment is checked first because a reference inside
// its own group is the one case where the two paths do not diverge at all. Then the paths are cut
// at their lowest common ancestor, and what the group's side of the cut begins with answers the
// disjunction question. Direction decides between the two ordering problems, and the negative
// lookaround check comes last because a group inside one can still be reachable in every other
// respect.
func classifyBackreference(
	reference backreference,
	group capturingGroup,
	nodes []regexNode,
) (backreferenceProblem, bool) {
	if group.start <= reference.start && reference.end <= group.end {
		return problemInsideItsOwnGroup, true
	}

	// Walk both paths together while they agree. Index 0 is the pattern root and is shared by
	// everything, so the comparison starts at 1 and the cut sits one past the last agreement.
	common := 1
	for common < len(reference.path) && common < len(group.path) &&
		reference.path[common] == group.path[common] {
		common++
	}

	groupCut := group.path[common:]

	// Direction is decided by the innermost lookaround the two share. A lookbehind *below* the cut
	// on one side only does not reverse the other side, which is what keeps `/(?<=(a))b\1/` clean:
	// the reference sits outside the lookbehind and matches forwards.
	matchesBackwards := false
	for index := common - 1; index >= 0; index-- {
		node := nodes[reference.path[index]]
		if node.kind == regexNodeLookahead || node.kind == regexNodeLookbehind {
			matchesBackwards = node.kind == regexNodeLookbehind
			break
		}
	}

	// Two paths that diverge at an alternative are in different branches of one disjunction, and
	// only one branch ever runs.
	if len(groupCut) > 0 && nodes[groupCut[0]].kind == regexNodeAlternative {
		return problemAnotherAlternative, true
	}

	if !matchesBackwards && reference.end <= group.start {
		return problemBeforeItsGroup, true
	}

	if matchesBackwards && group.end <= reference.start {
		return problemAfterItsGroupInLookbehind, true
	}

	// A group under a negative lookaround the reference is not itself under can never keep a
	// capture, because the assertion succeeds only when the group's path failed.
	for _, nodeIndex := range groupCut {
		node := nodes[nodeIndex]
		if node.negative && (node.kind == regexNodeLookahead || node.kind == regexNodeLookbehind) {
			return problemIntoNegativeLookaround, true
		}
	}

	return backreferenceProblem{}, false
}

// scanRegexStructure reads a pattern into the groups, backreferences and enclosing paths the rule
// needs. It returns false for a pattern it cannot make sense of, which is a syntax error the engine
// has already refused and where a second opinion would be noise.
func scanRegexStructure(pattern string, flags regex_syntax.RegexFlags) (regexStructure, bool) {
	scanner := &regexStructureScanner{pattern: pattern, flags: flags}
	if !scanner.run() {
		return regexStructure{}, false
	}
	return scanner.structure, true
}
