package core

import (
	"strings"
	"unicode/utf8"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/verify/internal/rule"
	"github.com/system-inc/verify/internal/utils/ecmascript/regexsyntax"
)

// The six findings are separate message ids rather than one, because they are six different
// mistakes that happen to share a shape. A reader who sees "combining class" knows to normalize the
// literal; one who sees "surrogate pair" without the flag knows to add the flag. Collapsing them
// into a single id would make the count assertable and the advice useless.
var messageMisleadingSurrogatePair = rule.Message{
	Id: "surrogatePairInCharacterClass",
	Description: "This character class holds a surrogate pair spelled partly as a code point escape, " +
		"so what looks like one astral character is two independent class members. The class matches " +
		"either half on its own and never the character the author wrote, which is a match that " +
		"silently succeeds on the wrong input rather than an error anybody sees.",
}

var messageMisleadingSurrogatePairWithoutUnicodeFlag = rule.Message{
	Id: "surrogatePairWithoutUnicodeFlagInCharacterClass",
	Description: "This character class holds an astral character while the pattern has neither the " +
		"u nor the v flag, so the engine reads it as its two UTF-16 halves and the class matches " +
		"either half alone. `/[👍]/` matches the two lone surrogates that spell it and never the " +
		"emoji; adding the flag is what makes the class mean the character it looks like.",
}

var messageMisleadingCombiningClass = rule.Message{
	Id: "combiningClassInCharacterClass",
	Description: "This character class holds a base character followed by a combining mark, which " +
		"renders as one glyph and is two code points. The class matches the base or the mark " +
		"separately, so `/[Á]/` accepts a bare `A` and accepts a stray accent, and rejects the " +
		"accented letter it appears to name.",
}

var messageMisleadingEmojiModifier = rule.Message{
	Id: "emojiModifierInCharacterClass",
	Description: "This character class holds an emoji followed by a skin tone modifier, which is one " +
		"glyph made of two code points. The class matches the unmodified emoji or the bare modifier, " +
		"so it accepts inputs nobody meant to accept and rejects the modified emoji itself.",
}

var messageMisleadingRegionalIndicator = rule.Message{
	Id: "regionalIndicatorInCharacterClass",
	Description: "This character class holds a pair of regional indicator symbols, which render as " +
		"one flag and are two code points. The class matches either letter alone, so a flag class " +
		"built this way accepts every other flag that shares a letter with it.",
}

var messageMisleadingZeroWidthJoiner = rule.Message{
	Id: "zeroWidthJoinerInCharacterClass",
	Description: "This character class holds characters spliced by a zero width joiner, which renders " +
		"as one glyph and is three or more code points. The class matches each piece on its own, so " +
		"a family emoji written this way matches the individual people in it and never the family.",
}

// NoMisleadingCharacterClass flags a character class holding something that looks like one character
// and is several.
//
//	valid:   /^[abc]$/
//	valid:   /^[👍]$/u          // the flag makes the astral character one class member
//	valid:   /[\uD83D]/         // a lone surrogate is one code unit and means what it says
//	valid:   /[́]/         // a lone combining mark, likewise
//	valid:   /^[\q{👶🏻}]$/v      // the v-flag string disjunction matches the sequence as a unit
//	invalid: /^[Á]$/u
//	invalid: /^[👍]$/
//	invalid: /^[👶🏻]$/u
//	invalid: /^[🇯🇵]$/u
//	invalid: /^[👨‍👩‍👦]$/u
//	invalid: new RegExp("[🎵]")
//
// A character class is a set of single characters, and the language builds that set out of code
// points rather than out of glyphs. Every finding here is the same gap seen five ways: text that a
// human reads as one character is stored as several, so the class quietly becomes a set of the
// pieces. The failure is not a thrown error and not a non-match; it is a match against the wrong
// input, which is why it survives review and testing and shows up as a security or correctness bug
// much later.
//
// # Why six ids and not one
//
// The five sequence shapes are detected independently and all of them run over every sequence, so
// one class can report several times and with several ids. `/[👨‍👩‍👦]/` reports five findings: three
// surrogate pairs from the three astral characters and two joins from the two zero width joiners.
// That is upstream's count on that input, and it is right rather than duplicated, because each
// finding names a different pair of adjacent members. A fixture asserting one finding per input
// would be wrong here, and the snapshot says so: 174 diagnostics from 130 failing inputs.
//
// The surrogate pair case splits in two because the repair differs. When neither half was written as
// a code point escape the pattern is simply missing its flag, and adding `u` fixes it; that is the
// suggestion below. When one half was written as `\u{...}` the author already knew about code
// points, so adding a flag would not be the repair and none is offered.
//
// # What the u and v flags change
//
// Under `u` or `v` an astral character is a single class member, so `/[👍]/u` is clean while `/[👍]/`
// is not. That is the whole of the without-flag finding. The other four detectors fire in both
// modes, because a combining mark or a joiner is two code points whatever the flag says.
//
// `v` adds a second thing: it gives a class internal structure. `\q{...}` matches a string as a
// unit, a nested `[...]` is its own class, and `--` and `&&` are set operators. Each of those breaks
// a run of characters in two, so `/[🇯[A]🇵]/v` is clean where `/[🇯🇵]/u` is not: the two indicators
// are no longer adjacent. That break is what `regexsyntax.RegexCharBreaker` marks, and honoring it
// is the difference between reproducing upstream and reporting four false positives its corpus
// names.
//
// # A range endpoint breaks the run too
//
// `[a-z👍]` holds a range and then a character, and the range's two endpoints are not adjacent to
// each other in the sense this rule cares about, because everything between them is also in the set.
// Upstream ends the current sequence at a range's minimum and starts a new one at its maximum, which
// is why `/[👍-￿]/` reports once rather than twice: the pair before the range is
// adjacent, the pair spanning into it is not.
//
// # The constructor half
//
// `new RegExp("[🎵]")` and `RegExp("[🎵]", "u")` are checked alongside literals, because the mistake
// is identical and the constructor form is the one that survives the parser. Only a string or a
// substitution-free template is read: an argument that is a variable could be anything at runtime,
// and reporting it would flag correct code.
//
// A pattern written as a string carries one more layer of escaping than a literal does, and that
// layer is the rule's sharpest edge upstream. `new RegExp("[\\uD83D\\uDC4D]")` passes the regex
// engine the six characters `\uD83D`, so the regex sees an escape; `new RegExp("[👍]")`
// passes it the astral character itself. Both report, and they report for different reasons. This
// port reads the string's cooked value, which is what the engine receives, so both land on the same
// path and the distinction upstream draws between them does not need to be redrawn here.
//
// # What this port does not do
//
// Upstream takes an `allowEscape` option that silences a sequence when any member was written with a
// backslash, and this port takes the default `false` and offers no surface, matching the choice
// `no-invalid-regexp` made for the same reason: nothing in this repository sets it, and a rule
// reading a value nobody writes is inert code wearing the shape of a feature. The default is
// asserted by fixture rather than assumed, because a port with no option surface lands on one branch
// or the other and the corpus alone cannot tell which.
//
// Upstream also declines to resolve a pattern held in a variable and declines a template with
// substitutions. Both gaps are reproduced rather than improved on: a substituted template's text is
// not known until it runs, and resolving a variable is a different rule's worth of machinery.
var NoMisleadingCharacterClass = rule.Rule{
	Name: "no-misleading-character-class",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		return rule.Listeners{
			ast.KindRegularExpressionLiteral: func(node *ast.Node) {
				// A literal that is the pattern argument of a RegExp call is checked by the call,
				// not here, because the call's flags argument replaces the literal's own. Checking
				// it twice would report `RegExp(/[👍]/u, '')` once for a clean literal and once for
				// the flagless call.
				if regexLiteralIsRegExpArgument(node) {
					return
				}
				text := node.Text()
				pattern, flags := regexsyntax.PatternAndFlags(text)
				if pattern == "" {
					return
				}
				literalStart := node.End() - len(text)
				checkRegexPattern(ctx, pattern, literalStart+1, flags, func() []rule.Suggestion {
					return unicodeFlagSuggestionForLiteral(node, text, flags)
				})
			},

			ast.KindCallExpression: func(node *ast.Node) {
				call := node.AsCallExpression()
				checkRegExpConstructorCall(ctx, call.Expression, call.Arguments)
			},
			ast.KindNewExpression: func(node *ast.Node) {
				expression := node.AsNewExpression()
				checkRegExpConstructorCall(ctx, expression.Expression, expression.Arguments)
			},
		}
	},
}

// checkRegExpConstructorCall checks a `RegExp(...)` or `new RegExp(...)` whose pattern is readable.
//
// The flags argument is consulted before the pattern because a flag decides what the pattern means:
// under `u` an astral character is one class member and under no flag it is two, which is the
// difference between a finding and a clean class. When the flags argument is present and is not a
// literal the call is skipped entirely rather than guessed at, matching upstream: the argument may
// well supply the `u` that makes the pattern correct.
func checkRegExpConstructorCall(ctx rule.Context, callee *ast.Node, arguments *ast.NodeList) {
	if !calleeIsRegExp(callee) {
		return
	}
	if arguments == nil || len(arguments.Nodes) == 0 {
		return
	}

	flags, flagsKnown := regexConstructorFlags(arguments)
	if !flagsKnown {
		return
	}

	patternNode := ast.SkipParentheses(arguments.Nodes[0])
	if patternNode == nil {
		return
	}

	// A regex literal handed to the constructor contributes its pattern text, and the call's flags
	// argument replaces the literal's own flags rather than merging with them. `RegExp(/[👍]/u, '')`
	// is therefore a flagless pattern and reports, which is upstream's behavior and the reason the
	// literal listener steps aside for this case.
	if patternNode.Kind == ast.KindRegularExpressionLiteral {
		text := patternNode.Text()
		pattern, ownFlags := regexsyntax.PatternAndFlags(text)
		if pattern == "" {
			return
		}
		if len(arguments.Nodes) < 2 {
			flags = ownFlags
		}
		literalStart := patternNode.End() - len(text)
		checkRegexPattern(ctx, pattern, literalStart+1, flags, nil)
		return
	}

	pattern, ok := regexConstructorPattern(patternNode)
	if !ok {
		return
	}
	checkRegexPatternInStringLiteral(ctx, pattern, patternNode, flags)
}

// checkRegexPatternInStringLiteral checks a pattern that reached the constructor as a string, where
// the text the engine sees and the text on disk are two different strings.
//
// The pattern checked is the literal's cooked value, because that is what the RegExp constructor
// receives: `"[\\uD83D\\uDC4D]"` hands the engine a backslash and the letters, and `"[👍]"` hands it
// the character. Both are correct inputs to the class scanner.
//
// But a finding has to point at the file, and cooked offsets are not file offsets. `"[á]"` is
// six bytes on disk and four cooked, and `"\\u200D"` is eight on disk and seven cooked, so an
// offset taken from the cooked string lands mid-character in the raw one and the reported span is
// garbage. That is not a hypothetical: it is the defect this function exists to fix, and it was
// invisible to every message-id fixture in the corpus because those assert what fired and never
// where.
//
// The mapping is built by decoding the raw text and recording, for each cooked byte, which raw byte
// produced it. When the two are the same length the mapping is the identity and is skipped, which is
// the common case.
func checkRegexPatternInStringLiteral(
	ctx rule.Context,
	pattern string,
	node *ast.Node,
	flags string,
) {
	textRange := rule.TokenRange(ctx.SourceFile, node)
	raw := ctx.SourceFile.Text()[textRange.Pos():textRange.End()]
	// The raw text carries its quotes or backticks; the cooked value does not.
	if len(raw) < 2 {
		return
	}
	rawBody := raw[1 : len(raw)-1]
	bodyStart := textRange.Pos() + 1

	// A pure shortcut, and a mutation sweep confirmed it as one: forcing every literal through the
	// mapper below leaves every fixture green, because when the two texts are the same length the
	// mapper computes exactly the identity mapping. It stays because the common case is a pattern
	// with no escapes at all, and walking it rune by rune to rediscover that offsets are offsets is
	// work this rule does on every string argument in the tree.
	if len(rawBody) == len(pattern) {
		checkRegexPattern(ctx, pattern, bodyStart, flags, nil)
		return
	}

	offsets := cookedToRawOffsets(rawBody, pattern)
	// Unreachable through the parser we have, and kept for the same reason as the flag guard above:
	// a sweep replacing this test with `false` survives, because the mapper only refuses when the
	// raw and cooked texts genuinely disagree, and a cooked value the parser produced from a raw
	// text it read cannot disagree with it. What the line protects against is this file's own
	// escape-width table falling behind the parser's, which is a change nobody would notice without
	// it: the alternative to declining is reporting at an offset computed from a desynced walk,
	// which points at real source and is wrong.
	if offsets == nil {
		// The raw text could not be reconciled with the cooked one, so no offset can be trusted.
		// Declining is the only honest move: a finding at a guessed span points somewhere real and
		// wrong, which is worse than no finding at all.
		return
	}
	checkRegexPatternMapped(ctx, pattern, bodyStart, flags, offsets)
}

// cookedToRawOffsets maps each byte offset in a cooked string onto its offset in the raw source.
//
// The returned slice has one more entry than the cooked string is long, so the end offset of the
// last character is addressable. A nil return means the two texts could not be walked together,
// which happens when the cooked value is not what this scanner would produce from the raw one.
//
// Only the escapes that can appear in the patterns this rule reads are decoded. Anything else falls
// through to the single-character case, and a mismatch there returns nil rather than a wrong answer.
func cookedToRawOffsets(raw string, cooked string) []int {
	offsets := make([]int, 0, len(cooked)+1)
	rawIndex, cookedIndex := 0, 0

	for rawIndex < len(raw) && cookedIndex < len(cooked) {
		var width int
		switch {
		case raw[rawIndex] == '\\' && rawIndex+1 < len(raw):
			width = escapeWidthInStringLiteral(raw, rawIndex)
		default:
			// A raw character carries its own UTF-8 width. Taking one byte here and comparing it
			// against a multi-byte cooked rune is what made every mapping fail: the two texts agree
			// and the comparison did not.
			_, runeWidth := utf8.DecodeRuneInString(raw[rawIndex:])
			if runeWidth == 0 {
				return nil
			}
			width = runeWidth
		}
		// Every cooked byte this raw token produced points back at the token's first byte, so a
		// span starting inside a decoded escape starts at the backslash.
		produced := cookedBytesProducedBy(raw[rawIndex:rawIndex+width], cooked[cookedIndex:])
		if produced == 0 {
			return nil
		}
		for count := 0; count < produced; count++ {
			offsets = append(offsets, rawIndex)
		}
		rawIndex += width
		cookedIndex += produced
	}
	if cookedIndex != len(cooked) {
		return nil
	}
	// Cooked finishing is necessary but not sufficient: the loop stops as soon as EITHER text runs
	// out, so a raw tail that produced no cooked characters at all is never compared against
	// anything and slips through. `'a  b\<newline>'` cooks to `a  b`, and checking only the cooked
	// side returns a complete, well formed mapping whose every offset past the run is wrong.
	//
	// Leftover raw is not wrong on its own, which is the trap and the reason this is not simply
	// `rawIndex != len(raw)`. A surrogate pair spends twelve raw bytes on one four byte rune, so
	// `[\uD83D\uDC4D]` legitimately exhausts cooked with six raw bytes still to go, and the strict
	// form rejects the case this rule exists for. What separates the two is whether the tail would
	// have produced anything: a low surrogate and a closing bracket would, a lone backslash or a
	// line continuation would not.
	//
	// Found by a mutation sweep in no-regex-spaces, which adopted this mapper and probed it against
	// its own corpus before building on it. The first form of this fix was the strict one and it
	// broke two of this rule's own fixtures, which is how the distinction got measured.
	if rawIndex < len(raw) && producesNoCookedBytes(raw[rawIndex:]) {
		return nil
	}
	offsets = append(offsets, rawIndex)
	return offsets
}

// producesNoCookedBytes reports whether a raw tail contributes nothing to the cooked string.
//
// Only two shapes do: a line continuation, which is defined to produce nothing, and a trailing lone
// backslash, which has nothing to escape. Anything else is either an ordinary character or an escape
// that decodes to one, so a tail containing any of it means the two texts genuinely disagree about
// length rather than the walk having stopped early.
func producesNoCookedBytes(tail string) bool {
	for index := 0; index < len(tail); {
		if tail[index] != '\\' {
			return false
		}
		if index+1 >= len(tail) {
			// A trailing backslash with nothing after it escapes nothing.
			return true
		}
		if tail[index+1] != '\n' && tail[index+1] != '\r' {
			return false
		}
		index += 2
		// A CRLF continuation spends one more byte.
		if tail[index-1] == '\r' && index < len(tail) && tail[index] == '\n' {
			index++
		}
	}
	return true
}

// escapeWidthInStringLiteral returns how many raw bytes a backslash escape occupies.
func escapeWidthInStringLiteral(raw string, index int) int {
	switch raw[index+1] {
	case 'u':
		if index+2 < len(raw) && raw[index+2] == '{' {
			closing := strings.IndexByte(raw[index+3:], '}')
			if closing >= 0 {
				return 3 + closing + 1
			}
			return 2
		}
		if index+5 < len(raw) && regexsyntax.AllHexDigits(raw[index+2:index+6]) {
			return 6
		}
		return 2
	case 'x':
		if index+3 < len(raw) && regexsyntax.IsHexDigit(raw[index+2]) &&
			regexsyntax.IsHexDigit(raw[index+3]) {
			return 4
		}
		return 2
	case '0', '1', '2', '3', '4', '5', '6', '7':
		// A legacy octal escape, which sloppy-mode source may still carry and which upstream's
		// corpus uses deliberately: `"[ \\ufe\60f]"` spells the `0` of a variation selector as
		// `\60` so that the escape only appears after the string is cooked. Up to three digits, and
		// a leading digit above three caps the run at two.
		width := 2
		limit := 3
		if raw[index+1] > '3' {
			limit = 2
		}
		for width < limit+1 && index+width < len(raw) && raw[index+width] >= '0' &&
			raw[index+width] <= '7' {
			width++
		}
		return width
	}
	// A backslash before anything else escapes that one character, and the character may be several
	// bytes. `\👍` is five raw bytes producing four cooked ones, and returning two here desynced the
	// walk on every case upstream wrote to exercise exactly this.
	_, runeWidth := utf8.DecodeRuneInString(raw[index+1:])
	if runeWidth == 0 {
		return 2
	}
	return 1 + runeWidth
}

// cookedBytesProducedBy returns how many bytes of the remaining cooked text one raw token produced.
//
// The comparison is by value rather than by rule: whatever the token decodes to must be a prefix of
// what is left, and if it is not, the walk has lost sync and the caller declines. Deciding it this
// way rather than by re-deriving each escape's meaning keeps this from becoming a second, subtly
// different string-literal decoder living next to the parser's.
func cookedBytesProducedBy(rawToken string, remainingCooked string) int {
	if rawToken == "" || remainingCooked == "" {
		return 0
	}
	if rawToken[0] != '\\' {
		_, width := utf8.DecodeRuneInString(remainingCooked)
		if width == 0 || width > len(rawToken) || rawToken[:width] != remainingCooked[:width] {
			return 0
		}
		return width
	}
	// A decoded escape produces one rune, whose encoded width is what it contributed.
	_, width := utf8.DecodeRuneInString(remainingCooked)
	if width == 0 {
		return 0
	}
	return width
}

// checkRegexPatternMapped is checkRegexPattern for a pattern whose offsets need translating back
// into the raw source before anything is reported.
func checkRegexPatternMapped(
	ctx rule.Context,
	pattern string,
	bodyStart int,
	flagsText string,
	offsets []int,
) {
	collector := &mappedFindingCollector{ctx: ctx, bodyStart: bodyStart, offsets: offsets}
	checkRegexPatternWithReporter(pattern, flagsText, collector.report)
}

// mappedFindingCollector reports a finding at the raw offsets its cooked span maps to.
type mappedFindingCollector struct {
	ctx       rule.Context
	bodyStart int
	offsets   []int
}

func (collector *mappedFindingCollector) report(finding pendingMisleadingFinding) {
	if finding.start >= len(collector.offsets) || finding.end >= len(collector.offsets) {
		return
	}
	collector.ctx.ReportRange(
		core.NewTextRange(
			collector.bodyStart+collector.offsets[finding.start],
			collector.bodyStart+collector.offsets[finding.end]),
		finding.message)
}

// calleeIsRegExp reports whether a call's callee names the global RegExp.
//
// The member forms are accepted because upstream accepts them and they appear in its corpus:
// `globalThis.RegExp`, `window.RegExp` and `global.RegExp` all reach the same constructor. What is
// deliberately not done is checking whether the name is shadowed. Upstream consults the scope and
// declines a locally bound `RegExp`; doing that here needs the binder, and the narrowing this rule
// would gain is a call to a user function that happens to be named RegExp and happens to be passed a
// pattern with a misleading class in it. The divergence is stated rather than hidden, and it can
// only produce a finding on code that is confusing for a second reason.
func calleeIsRegExp(callee *ast.Node) bool {
	callee = ast.SkipParentheses(callee)
	if callee == nil {
		return false
	}
	if callee.Kind == ast.KindIdentifier {
		return callee.AsIdentifier().Text == "RegExp"
	}
	if callee.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	access := callee.AsPropertyAccessExpression()
	if access.Name() == nil || access.Name().Kind != ast.KindIdentifier {
		return false
	}
	if access.Name().AsIdentifier().Text != "RegExp" {
		return false
	}
	object := ast.SkipParentheses(access.Expression)
	if object == nil || object.Kind != ast.KindIdentifier {
		return false
	}
	switch object.AsIdentifier().Text {
	case "globalThis", "window", "global":
		return true
	}
	return false
}

// regexConstructorFlags returns the flags a constructor call passes, and whether they are knowable.
//
// A one-argument call passes no flags, which is the empty string and is knowable.
//
// The two unknowable shapes are treated differently, and the difference is upstream's rather than a
// choice made here. A template with substitutions declines the whole call: its text is assembled at
// run time and could contain a `u`, so nothing can be said. Any other non-literal argument, an
// identifier most often, is read as no flags and the pattern is checked without one. Upstream's
// corpus pins both sides: `new RegExp('[🇯🇵]', `${foo}`)` is a pass case and
// `const flags = ""; var r = new RegExp("[👍]", flags)` is a fail case, and the only thing separating
// them is which of the two shapes the argument has.
//
// That asymmetry is hard to defend on its own terms, since an identifier can hold `"u"` exactly as
// easily as a template can produce it, and reading it as the empty string is a guess that reports on
// code which may well be correct. It is reproduced rather than corrected because the corpus asserts
// it in both directions, and a port that quietly picked the consistent reading would go silent on a
// case upstream reports.
func regexConstructorFlags(arguments *ast.NodeList) (string, bool) {
	if len(arguments.Nodes) < 2 {
		return "", true
	}
	flagsNode := ast.SkipParentheses(arguments.Nodes[1])
	if flagsNode == nil {
		return "", false
	}
	switch flagsNode.Kind {
	case ast.KindStringLiteral:
		return flagsNode.AsStringLiteral().Text, true
	case ast.KindNoSubstitutionTemplateLiteral:
		return flagsNode.Text(), true
	case ast.KindTemplateExpression:
		return "", false
	}
	return "", true
}

// regexConstructorPattern returns the pattern text a constructor argument carries.
//
// The text returned is the cooked value, which is what the RegExp constructor actually receives.
// That means `"\\u200D"` yields the seven characters the regex engine parses as an escape, and a
// raw joiner yields the joiner itself. Both are correct inputs to the class scanner and it tells
// them apart on its own. Where that text sits in the file is a separate question, answered by
// checkRegexPatternInStringLiteral, because the two are not the same string.
func regexConstructorPattern(node *ast.Node) (string, bool) {
	switch node.Kind {
	case ast.KindStringLiteral:
		return node.AsStringLiteral().Text, true
	case ast.KindNoSubstitutionTemplateLiteral:
		return node.Text(), true
	}
	return "", false
}

// regexLiteralIsRegExpArgument reports whether a regex literal is the pattern argument of a RegExp
// construction, in which case the construction checks it and this literal must not.
func regexLiteralIsRegExpArgument(node *ast.Node) bool {
	child := node
	parent := node.Parent
	for parent != nil {
		switch parent.Kind {
		case ast.KindParenthesizedExpression, ast.KindAsExpression,
			ast.KindSatisfiesExpression, ast.KindNonNullExpression, ast.KindTypeAssertionExpression:
			child = parent
			parent = parent.Parent
			continue
		case ast.KindCallExpression:
			call := parent.AsCallExpression()
			return calleeIsRegExp(call.Expression) && firstArgumentIs(call.Arguments, child)
		case ast.KindNewExpression:
			expression := parent.AsNewExpression()
			return calleeIsRegExp(expression.Expression) && firstArgumentIs(expression.Arguments, child)
		}
		return false
	}
	return false
}

// firstArgumentIs reports whether a node is the first argument of an argument list.
func firstArgumentIs(arguments *ast.NodeList, node *ast.Node) bool {
	return arguments != nil && len(arguments.Nodes) > 0 && arguments.Nodes[0] == node
}

// checkRegexPattern reports every misleading sequence in every character class of a pattern.
//
// suggestions is called only for the without-flag surrogate finding, which is the one finding whose
// repair is known, and it is a function rather than a value so a literal's rewrite is not computed
// for the patterns that never report.
func checkRegexPattern(
	ctx rule.Context,
	pattern string,
	patternStart int,
	flagsText string,
	suggestions func() []rule.Suggestion,
) {
	checkRegexPatternWithReporter(pattern, flagsText, func(finding pendingMisleadingFinding) {
		textRange := core.NewTextRange(patternStart+finding.start, patternStart+finding.end)
		if finding.suggest && suggestions != nil {
			ctx.ReportRangeWithSuggestions(textRange, finding.message, suggestions()...)
			return
		}
		ctx.ReportRange(textRange, finding.message)
	})
}

// checkRegexPatternWithReporter finds every misleading sequence in a pattern and hands each to a
// reporter, which decides where in the file it lands.
//
// The two callers differ only in that translation: a regex literal's pattern offsets are file
// offsets plus a constant, and a string literal's are not, because the text the engine sees was
// decoded from the text on disk. Keeping the search in one place is what stops the two from drifting
// into two slightly different rules.
func checkRegexPatternWithReporter(
	pattern string,
	flagsText string,
	report func(pendingMisleadingFinding),
) {
	// The cheap gate: no bracket, no class. Most patterns in a real tree have none, and the class
	// walk below is the expensive part.
	if !strings.ContainsRune(pattern, '[') {
		return
	}

	flags := regexsyntax.ParseRegexFlags(flagsText)

	// Under `u` or `v` a brace that opens no quantifier is a SyntaxError, so `RegExp('{ [Á]', 'u')`
	// never runs and the class inside it cannot mislead anybody. Upstream declines it because its
	// parser refuses the pattern outright, and two of its clean cases are this exact shape.
	//
	// Checked here rather than by handing the pattern to the repository's regex compiler, which was
	// measured against these inputs and is the wrong gate: it accepts this brace, rejects the `v`
	// flag entirely, and rejects `/[👍-￿]/` which upstream reports on. Trading two false
	// positives for four false negatives is not a trade, so the one construct that matters is
	// tested directly.
	if flags.UV() && patternHasLoneQuantifierBrace(pattern, flags) {
		return
	}

	// Findings are collected before any is reported, because a pattern that turns out to be
	// unparseable must produce none at all. `new RegExp('[Á] [ ')` opens a well-formed class and
	// then an unterminated one; the walk reports the first and only then discovers the second, so
	// reporting as it goes would emit a finding about a pattern that throws a SyntaxError before it
	// can match anything. Upstream declines the whole call because its parser refuses the pattern
	// outright, and five of its clean cases are exactly this shape.
	var found []pendingMisleadingFinding
	classesParsed := true
	walked := regexsyntax.IterateRegexCharacterClasses(pattern, flags, func(start, end int) {
		elements, _, ok := regexsyntax.ParseRegexCharacterClassWithEnd(pattern, start, end, flags)
		if !ok {
			classesParsed = false
			return
		}
		for _, sequence := range characterSequences(elements, flags) {
			found = append(found, misleadingSequenceFindings(sequence)...)
		}
	})
	if !walked || !classesParsed {
		return
	}

	for _, finding := range found {
		report(finding)
	}
}

// pendingMisleadingFinding is one finding held back until the whole pattern is known to parse.
type pendingMisleadingFinding struct {
	start   int
	end     int
	message rule.Message
	// suggest marks the one finding whose repair is known: an astral character in a pattern that is
	// simply missing its flag.
	suggest bool
}

// misleadingCharacter is one class member, reduced to what the detectors need: the code point it
// denotes, where its source sits, and whether it was written as a code point escape.
type misleadingCharacter struct {
	value uint32
	start int
	end   int
	// isCodePointEscape records `\u{...}` spelling, which decides only which of the two surrogate
	// pair messages fires. An author who wrote `\u{d83d}` chose the code point deliberately, so
	// telling them to add a flag would be the wrong advice.
	isCodePointEscape bool
}

// characterSequences splits a class's elements into the runs of adjacent characters the detectors
// scan.
//
// The splitting is the part that is easy to get wrong and that upstream's clean cases are mostly
// about. Three things end a run:
//
//   - a breaker, which is `\d`, `\p{...}`, a v-flag `\q{...}`, a nested class, or a set operator.
//     Characters on opposite sides of one are not adjacent, which is why `/[🇯\q{abc}🇵]/v` is clean.
//   - a range's minimum, which ends the run, with the range's maximum starting a new one. The
//     characters a range covers sit between its endpoints, so the endpoints are not neighbours.
//   - the class itself ending. Two classes side by side are two runs, which is why
//     `/[‌][‍][a]/` is clean while `/[‌‍]/` is not.
//
// Under no unicode flag an astral character occupies two UTF-16 code units and the engine treats
// each as its own class member, so it is expanded here into its surrogate halves. That expansion is
// the entire without-flag surrogate finding, and it is done in the rule rather than in the shared
// scanner because it is a question about what this rule means by adjacency rather than about how the
// pattern parses.
func characterSequences(
	elements []regexsyntax.RegexCharElement,
	flags regexsyntax.RegexFlags,
) [][]misleadingCharacter {
	var sequences [][]misleadingCharacter
	var current []misleadingCharacter

	flush := func() {
		if len(current) > 0 {
			sequences = append(sequences, current)
			current = nil
		}
	}

	appendCharacter := func(value uint32, start int, end int, isCodePointEscape bool) {
		// Under no unicode flag the engine sees UTF-16 code units, so an astral character is two
		// members rather than one. Their spans both cover the whole character, because there is no
		// narrower source text to point at: the two halves were written as one glyph.
		if !flags.UV() && value > 0xFFFF && !isCodePointEscape {
			high := 0xD800 + ((value - 0x10000) >> 10)
			low := 0xDC00 + ((value - 0x10000) & 0x3FF)
			current = append(current,
				misleadingCharacter{value: high, start: start, end: end},
				misleadingCharacter{value: low, start: start, end: end})
			return
		}
		current = append(current, misleadingCharacter{
			value: value, start: start, end: end, isCodePointEscape: isCodePointEscape,
		})
	}

	for _, element := range elements {
		switch element.Kind {
		case regexsyntax.RegexCharBreaker:
			flush()
		case regexsyntax.RegexCharRange:
			// The minimum joins the run that was building and closes it; the maximum opens the next
			// one. Reproducing that shape rather than dropping ranges is what makes
			// `/[👍-￿]/` report exactly once.
			appendCharacter(element.Value, element.Start, element.End, element.IsUBrace)
			flush()
			appendCharacter(element.Max, element.Start, element.End, element.MaxIsUBrace)
		default:
			appendCharacter(element.Value, element.Start, element.End, element.IsUBrace)
		}
	}
	flush()
	return sequences
}

// misleadingSequenceFindings runs every detector over one run of adjacent characters.
//
// All five run, and each reports independently, so one run can produce findings of several kinds.
// The order matches upstream's so that a reader comparing this port against it, or comparing two
// diagnostic streams, sees the same sequence rather than the same set.
func misleadingSequenceFindings(sequence []misleadingCharacter) []pendingMisleadingFinding {
	var found []pendingMisleadingFinding
	report := func(from misleadingCharacter, to misleadingCharacter, message rule.Message) {
		found = append(found, pendingMisleadingFinding{
			start: from.start, end: to.end, message: message})
	}

	for index := 1; index < len(sequence); index++ {
		previous, current := sequence[index-1], sequence[index]

		if isCombiningCharacter(current.value) && !isCombiningCharacter(previous.value) {
			report(previous, current, messageMisleadingCombiningClass)
		}
		if isRegionalIndicatorSymbol(previous.value) && isRegionalIndicatorSymbol(current.value) {
			report(previous, current, messageMisleadingRegionalIndicator)
		}
		if isEmojiModifier(current.value) && !isEmojiModifier(previous.value) {
			report(previous, current, messageMisleadingEmojiModifier)
		}
	}

	// The joiner detector reads three characters rather than two, because a joiner is only
	// misleading when it actually joins: it needs something on each side, and neither side may be
	// another joiner. `/[‍]/` is a class holding one joiner and means what it says.
	for index := 1; index+1 < len(sequence); index++ {
		if sequence[index].value != zeroWidthJoiner {
			continue
		}
		if sequence[index-1].value == zeroWidthJoiner || sequence[index+1].value == zeroWidthJoiner {
			continue
		}
		report(sequence[index-1], sequence[index+1], messageMisleadingZeroWidthJoiner)
	}

	for index := 1; index < len(sequence); index++ {
		previous, current := sequence[index-1], sequence[index]
		if !isSurrogatePair(previous.value, current.value) {
			continue
		}
		// Which of the two messages fires is decided by spelling rather than by value. When either
		// half was written `\u{...}` the author picked the code point on purpose and adding a flag
		// is not the repair, so the finding carries no suggestion. When neither was, the pattern is
		// missing its flag and that is exactly what to propose.
		if previous.isCodePointEscape || current.isCodePointEscape {
			report(previous, current, messageMisleadingSurrogatePair)
			continue
		}
		found = append(found, pendingMisleadingFinding{
			start:   previous.start,
			end:     current.end,
			message: messageMisleadingSurrogatePairWithoutUnicodeFlag,
			suggest: true,
		})
	}
	return found
}

// unicodeFlagSuggestionForLiteral proposes appending `u` to a regex literal's flags.
//
// This is a suggestion and never a fix, and the reason is the whole point of the rewrite: adding `u`
// changes what the rest of the pattern means. Under `u` an identity escape like `\a` is a syntax
// error, a lone `{` is a syntax error, and `\p{L}` stops being the letters `p{L}` and becomes a
// property escape. So the edit can turn a working pattern into one that throws at parse time, and a
// rewrite that can break the file is a thing a human chooses rather than a thing the engine applies
// while nobody is looking.
//
// Upstream reaches the same conclusion and declares the rule `suggestion` rather than `fix`. It also
// re-parses the pattern under the new flag and withholds the rewrite when the result would not
// parse; this port does not, and instead withholds it whenever the pattern holds anything whose
// meaning `u` is known to change. That is a coarser test in the same direction: it declines some
// rewrites upstream would offer, and never offers one upstream would decline.
func unicodeFlagSuggestionForLiteral(node *ast.Node, text string, flags string) []rule.Suggestion {
	// No fixture can kill this line, and it stays anyway. A mutation sweep dropped each half of the
	// test and both mutants survived, which sent me looking for the input that would tell the
	// versions apart. There is none, and the reason is one step upstream: reaching here at all takes
	// a `suggest` finding, which takes two adjacent surrogate halves, and under `u` or `v` the class
	// parser has already folded `👍` into a single astral element before this rule sees
	// it. The only way two halves survive that fold is for one to be written `\u{...}`, which routes
	// to the no-suggestion branch before this function is ever called.
	//
	// So the guard is unreachable through the parser we have today, not through the language. If the
	// shelf ever stops folding, this is the line that keeps the rule from proposing a flag a pattern
	// already carries, and its absence would be silent.
	if strings.ContainsRune(flags, 'u') || strings.ContainsRune(flags, 'v') {
		return nil
	}
	pattern, _ := regexsyntax.PatternAndFlags(text)
	if patternMeaningChangesUnderUnicodeFlag(pattern) {
		return nil
	}
	return []rule.Suggestion{{
		Message: rule.Message{
			Id: "addUnicodeFlag",
			Description: "Add the u flag so the class matches the character rather than its parts. " +
				"The flag changes how the whole pattern parses, so this is offered rather than applied.",
		},
		Fixes: []rule.Fix{{
			Range: core.NewTextRange(node.End(), node.End()),
			Text:  "u",
		}},
	}}
}

// patternMeaningChangesUnderUnicodeFlag reports whether adding `u` could change how a pattern parses.
//
// Three constructs are legal without the flag and are errors or mean something else with it: an
// identity escape of a character that needs no escaping, an unquantified brace, and a lone `]`.
// Rather than enumerate the escapes the flag forbids, this refuses any escape outside the set the
// flag leaves alone, which keeps a new escape from silently becoming safe when it is not.
func patternMeaningChangesUnderUnicodeFlag(pattern string) bool {
	for index := 0; index < len(pattern); index++ {
		character := pattern[index]
		if character == '{' || character == '}' {
			// A brace that does not open a quantifier is a literal brace without the flag and a
			// syntax error with it. Deciding which it is means parsing the quantifier grammar, so
			// the coarse answer is taken: braces are only safe when they close a `\u{...}`, which
			// the escape branch below has already consumed.
			return true
		}
		if character != '\\' || index+1 >= len(pattern) {
			continue
		}
		next := pattern[index+1]
		if next == 'u' && index+2 < len(pattern) && pattern[index+2] == '{' {
			closing := strings.IndexByte(pattern[index+3:], '}')
			if closing < 0 {
				return true
			}
			index += 3 + closing
			continue
		}
		if !strings.ContainsRune(unicodeFlagSafeEscapes, rune(next)) {
			return true
		}
		index++
	}
	return false
}

// patternHasLoneQuantifierBrace reports whether a pattern holds a `{` outside a character class
// that opens no counted quantifier.
//
// Without a unicode flag such a brace is an ordinary literal character and the pattern is fine. With
// one it is a SyntaxError, which is the whole reason this is asked: a pattern that cannot compile
// cannot mislead a reader about what it matches, so the rule has nothing to say about it.
//
// Only `{` is examined and a stray `}` is ignored, because the two are not symmetric in the grammar:
// a lone closing brace is legal under the unicode flag while a lone opening brace is not.
func patternHasLoneQuantifierBrace(pattern string, flags regexsyntax.RegexFlags) bool {
	for index := 0; index < len(pattern); {
		switch pattern[index] {
		case '\\':
			step, ok := regexsyntax.SkipPatternEscape(pattern, index, flags)
			if !ok {
				return false
			}
			index += step
		case '[':
			// A brace inside a class is a literal brace in every mode, so classes are stepped over
			// whole rather than scanned.
			end, ok := regexsyntax.ClassEnd(pattern, index, flags)
			if !ok {
				return false
			}
			index = end
		case '{':
			if !quantifierBraceEnds(pattern, index) {
				return true
			}
			index++
		default:
			index++
		}
	}
	return false
}

// quantifierBraceEnds reports whether the `{` at index opens a counted quantifier.
//
// The accepted shapes are the language's three: `{n}`, `{n,}` and `{n,m}`. Anything else is a
// literal brace, which is the case the caller is looking for.
func quantifierBraceEnds(pattern string, index int) bool {
	scan := index + 1
	digitsStart := scan
	for scan < len(pattern) && pattern[scan] >= '0' && pattern[scan] <= '9' {
		scan++
	}
	if scan == digitsStart {
		return false
	}
	if scan < len(pattern) && pattern[scan] == ',' {
		scan++
		for scan < len(pattern) && pattern[scan] >= '0' && pattern[scan] <= '9' {
			scan++
		}
	}
	return scan < len(pattern) && pattern[scan] == '}'
}

// unicodeFlagSafeEscapes are the escape characters whose meaning the `u` flag leaves alone.
//
// Everything here is either a syntax character that must be escaped in both modes, a named escape,
// or a class shorthand. An identity escape of anything else is legal without the flag and rejected
// with it, which is the case this list exists to exclude.
const unicodeFlagSafeEscapes = `^$\.*+?()[]{}|/dDsSwWbBnrtvf0123456789xuck<>=!:,-`

// zeroWidthJoiner is U+200D, the code point that splices emoji into one glyph.
const zeroWidthJoiner = 0x200D

// isRegionalIndicatorSymbol reports whether a code point is one of the 26 letters that pair into a
// flag. Each stands for one Latin letter, and two of them make an ISO 3166 country code.
func isRegionalIndicatorSymbol(value uint32) bool {
	return value >= 0x1F1E6 && value <= 0x1F1FF
}

// isEmojiModifier reports whether a code point is one of the five skin tone modifiers.
func isEmojiModifier(value uint32) bool {
	return value >= 0x1F3FB && value <= 0x1F3FF
}

// isSurrogatePair reports whether two code units are a lead and a trail that together denote one
// astral character.
func isSurrogatePair(high uint32, low uint32) bool {
	return high >= 0xD800 && high <= 0xDBFF && low >= 0xDC00 && low <= 0xDFFF
}

// isCombiningCharacter reports whether a code point is a mark that attaches to the character before
// it, covering the Mn, Mc and Me general categories and the variation selectors.
//
// The ranges are the blocks dedicated to combining marks rather than the full Unicode property,
// which is what upstream matches and is a deliberate narrowing on both sides: script-specific marks
// in the Devanagari and Hiragana blocks share the same categories and are not listed, so a Devanagari
// sign following its consonant is not reported. Reproduced rather than extended, because widening it
// here would make this port report on inputs upstream's corpus asserts are clean, and the fix for a
// stated gap is a Unicode table rather than a longer list of guesses.
func isCombiningCharacter(value uint32) bool {
	switch {
	case value >= 0x0300 && value <= 0x036F: // Combining Diacritical Marks
		return true
	case value >= 0x1AB0 && value <= 0x1AFF: // Combining Diacritical Marks Extended
		return true
	case value >= 0x1DC0 && value <= 0x1DFF: // Combining Diacritical Marks Supplement
		return true
	case value >= 0x20D0 && value <= 0x20FF: // Combining Diacritical Marks for Symbols
		return true
	case value >= 0xFE00 && value <= 0xFE0F: // Variation Selectors
		return true
	case value >= 0xFE20 && value <= 0xFE2F: // Combining Half Marks
		return true
	case value >= 0xE0100 && value <= 0xE01EF: // Variation Selectors Supplement
		return true
	}
	return false
}
