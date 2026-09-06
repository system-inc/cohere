package typescript

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/rule"
	"github.com/system-inc/cohere/internal/utilities/comments"
)

// messageBanTsCommentBanned is upstream's `comment`, the outright refusal of a directive.
//
// `rule.Message` is `{Id, Description}` with no interpolation layer, so upstream's format verb
// becomes concatenation at the report site. The directive name is worth carrying rather than
// dropping: a file holding a `@ts-nocheck` and a `@ts-check` produces two findings under the same
// id, and the name is the only thing in the text that separates them.
func messageBanTsCommentBanned(directive string) rule.Message {
	return rule.Message{
		Id: "banTsComment",
		Description: "This file silences the TypeScript compiler with `@ts-" + directive +
			"`. A directive comment switches off checking for a line or a whole file, so the error " +
			"it hides stays in the code and stops being reported to anyone. Fix the underlying " +
			"type error instead, or narrow the suppression to `@ts-expect-error` with a " +
			"description saying why it is necessary.",
	}
}

// messageBanTsCommentPreferExpectError is upstream's `ignore_instead_of_expect_error`.
//
// This is the only arm carrying a repair, and the only one whose text names a replacement rather
// than a removal, because the two directives differ in a way that matters: `@ts-expect-error`
// becomes an error itself once the line stops failing, and `@ts-ignore` silently rots.
func messageBanTsCommentPreferExpectError() rule.Message {
	return rule.Message{
		Id: "banTsCommentPreferExpectError",
		Description: "This suppression uses `@ts-ignore`, which keeps silencing the line even " +
			"after the underlying error is gone, so a stale suppression is invisible forever. " +
			"`@ts-expect-error` reports when the line it guards has stopped failing, which is what " +
			"makes the suppression removable.",
	}
}

// messageBanTsCommentRequiresDescription is upstream's `comment_requires_description`.
//
// The minimum length is rendered into the text because it is configurable, so a reader seeing the
// finding with no number would have to open the config to learn what would satisfy it.
func messageBanTsCommentRequiresDescription(directive string, minimumLength int) rule.Message {
	return rule.Message{
		Id: "banTsCommentRequiresDescription",
		Description: "This `@ts-" + directive + "` carries no explanation, so nobody reading it " +
			"later can tell whether the suppression is still needed or what it was hiding. Write " +
			"at least " + strconv.Itoa(minimumLength) + " characters after the directive saying which " +
			"error it suppresses and why the error cannot be fixed.",
	}
}

// messageBanTsCommentDescriptionFormat is upstream's `comment_description_not_match_pattern`.
//
// The pattern is rendered because it is the only actionable part: a reader told their description
// is wrong without being told the shape has nothing to act on.
func messageBanTsCommentDescriptionFormat(directive string, pattern string) rule.Message {
	return rule.Message{
		Id: "banTsCommentDescriptionFormat",
		Description: "The explanation after this `@ts-" + directive + "` does not match the shape " +
			"this project requires, `" + pattern + "`. The format exists so a suppression can be " +
			"traced back to the error it hides, which a free-form note cannot be.",
	}
}

// BanTsComment flags the TypeScript directive comments that switch off type checking.
//
//	valid:   // just a comment containing @ts-expect-error somewhere
//	valid:   // @ts-expect-error here is why the error is expected
//	valid:   /// @ts-nocheck                        three slashes is a pragma, not a directive
//	valid:   /* @ts-ignore\n * not the last line */
//	invalid: // @ts-ignore
//	invalid: // @ts-expect-error
//	invalid: /* @ts-nocheck */                      as a line comment; see the block note below
//
// Ported from `typescript/ban-ts-comment`, which oxc in turn ports from
// `@typescript-eslint/ban-ts-comment`.
//
// # A comment, not a node
//
// A directive is trivia, so there is no kind to anchor on. The rule runs once per file out of a
// `KindSourceFile` listener, matching upstream's `run_once`, and reads `comments.ForFile`, the
// shared per-file scan. Probed before building on it: the shelf returns the comment for a file whose
// entire content is `// @ts-ignore`, for a block comment alone in a file, and for a directive nested
// inside a block statement. All six shapes this rule needs were measured rather than assumed.
//
// # The span is the comment's INTERIOR, and that is not the comment's range
//
// Upstream reports `comm.content_span()`, which is the text between the delimiters. Measured
// against the release binary with the JSON reporter: `/* @ts-ignore */` at offset 0 reports offset
// 2 length 12, and a line comment at offset 13 reports offset 15 length 14. So a line comment's
// span drops two characters from the front and a block comment's drops two from each end. Our
// shelf hands back the comment's full range including delimiters, so the trimming happens here.
// `ReportRange` is used rather than `ReportNode` because there is no node, and a range helper does
// no trimming of its own, which is what this needs.
//
// # Only the LAST line of a block comment is scanned, and the prefix alphabet differs by kind
//
// Upstream's `find_ts_comment_directive` takes the text after the final newline for a block comment
// and the whole text for a line comment, then requires everything before `@ts-` on that line to be
// whitespace, plus slashes for a line comment, plus slashes OR stars for a block comment. That
// asymmetry is real and reachable, measured on the release binary:
//
//	// * @ts-ignore                    silent    a star may not precede in a LINE comment
//	/* * @ts-ignore */                 reports   a star may precede in a BLOCK comment
//	/* / @ts-ignore */                 reports   so may a slash
//	/////@ts-ignore                    reports   any number of slashes
//	//@ts-ignore                       reports   no space is required
//	// hello @ts-ignore                silent    a word may not precede
//	/* @ts-ignore\n * not last line */ silent    the last line holds no directive
//	/*\n @ts-ignore\n*/                silent    the last line is the closing delimiter
//
// The mid-line case is the one worth naming, because "a comment mentioning the directive" is
// exactly what upstream's first passing case is, and reading the code without probing leaves it
// ambiguous whether the guard is on position or on the surrounding text. It is on the text.
//
// # Three slashes exempt check and nocheck ONLY
//
// Upstream skips a `check` or `nocheck` directive when the content, delimiters already stripped,
// begins with a slash after trimming. Because `content_span` removes two characters, `/// x` leaves
// `/ x` and `// x` leaves ` x`, so the test is on the third character of the comment. It reads as a
// test for comment kind and is not one. Measured, and the asymmetry is the surprising half:
//
//	/// @ts-nocheck        silent     pragma exemption
//	/// @ts-check          silent     pragma exemption
//	/// @ts-ignore         reports    the guard names check and nocheck only
//	/// @ts-expect-error   reports    likewise
//
// A separate guard drops `check` and `nocheck` in a block comment entirely, which is why
// `/* @ts-nocheck */` is silent while `// @ts-nocheck` reports.
//
// # The description length is BYTES and the format match is UNTRIMMED
//
// Upstream compares `description.trim().len()`, which is a byte count in Rust, against the minimum,
// and then matches the regex against the description **without** trimming. Both halves are
// measured:
//
//	// @ts-expect-error éé      silent    two runes, four bytes, minimum three
//	// @ts-expect-error é       reports   one rune, two bytes
//
// The rune reading would report the first, so this is a place where a port doing the sensible thing
// diverges. The corpus's three emoji passing cases exist for this and are only passing because a
// family emoji is many bytes.
//
// The untrimmed regex is what makes upstream's `@ts-ignore    : TS1234 because xyz` cases report:
// the trimmed description is long enough to satisfy the length check, and the untrimmed one begins
// with spaces so `^:` fails. Measured on the release binary, and it means the two checks are not
// alternatives.
//
// # One comment can report TWICE
//
// The length check and the format check both run, in that order, with no early exit between them.
// Probed with `minimumDescriptionLength: 25` and a format, on `// @ts-ignore: TS1`, which reports
// both. No corpus case triggers both, which is the only reason the extractor's diagnostic count
// equals its input count, so the fixture that pins this is one written here rather than imported.
//
// # The fix replaces EVERY occurrence, not the directive
//
// Upstream's fixer is `raw.cow_replace("@ts-ignore", "@ts-expect-error")` over the whole content
// span, so a comment naming the directive twice has both rewritten. Measured by running the release
// binary with `--fix` on `// @ts-ignore see other @ts-ignore`, which produced two replacements. It
// is reproduced rather than narrowed, because the fix is applied unattended and a narrower rewrite
// would leave a span upstream rewrote.
var BanTsComment = rule.Rule{
	// No namespace prefix. The config writes `typescript/ban-ts-comment` and the parity guard
	// strips the namespace on a `/` boundary.
	Name: "@typescript-eslint/ban-ts-comment",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}

		// Upstream's `should_run` is `ctx.source_type().is_typescript()`, so a directive in a
		// JavaScript file is silent. Fidelity rather than an optimization: a `@ts-ignore` in a
		// `.js` file is how JavaScript files opt into checking, and reporting it would be a
		// false-positive class no imported fixture can see.
		if !isTypeScriptSourceFile(ctx.SourceFile.FileName()) {
			return nil
		}

		// The zero value is not the default and this is the normal path, not a defensive branch. A
		// rule configured as a bare `"error"` is handed nil options, because `DecodeOptionsInto`
		// errors on empty input and the config layer turns that into nil. A bare type assertion
		// would then yield four zero-valued directive settings, which match no arm, and the rule
		// would register on every file and report nothing while every fixture stayed green.
		parsed, decoded := options.(BanTsCommentOptions)
		if !decoded {
			parsed = DefaultBanTsCommentOptions()
		}

		return rule.Listeners{
			ast.KindSourceFile: func(node *ast.Node) {
				reportBanTsComments(ctx, parsed)
			},
		}
	},
}

// reportBanTsComments is upstream's `run_once` body.
func reportBanTsComments(ctx rule.Context, options BanTsCommentOptions) {
	for _, comment := range comments.ForFile(ctx) {
		content, contentRange, readable := commentContent(comment)
		if !readable {
			continue
		}

		directive, description, found := findTsCommentDirective(content, !comment.IsBlock)
		if !found {
			continue
		}

		// A block comment never carries a `check` or `nocheck` directive, and a content beginning
		// with a slash never does either. Both are upstream's, in upstream's order, and the second
		// is the three-slash pragma exemption described on the rule.
		if directive == banTsCommentCheck || directive == banTsCommentNoCheck {
			if comment.IsBlock {
				continue
			}
			if strings.HasPrefix(strings.TrimLeft(content, " \t\n\r\v\f"), "/") {
				continue
			}
		}

		setting := options.settingFor(directive)

		if setting.Kind == BanTsCommentBoolean {
			if !setting.Banned {
				continue
			}
			if directive == banTsCommentIgnore {
				ctx.ReportRangeWithFixes(contentRange, messageBanTsCommentPreferExpectError(),
					rule.Fix{
						Range: contentRange,
						Text:  strings.ReplaceAll(content, "@ts-ignore", "@ts-expect-error"),
					})
				continue
			}
			ctx.ReportRange(contentRange, messageBanTsCommentBanned(directive))
			continue
		}

		// Both checks below run. There is no early exit between them, so a description that is too
		// short AND wrongly shaped reports twice. See the doc comment.
		//
		// The length is a byte count of the TRIMMED description and the pattern is matched against
		// the UNTRIMMED one. That asymmetry is upstream's and it decides four of its own cases.
		if len(strings.TrimSpace(description)) < options.MinimumDescriptionLength {
			ctx.ReportRange(contentRange, messageBanTsCommentRequiresDescription(
				directive, options.MinimumDescriptionLength))
		}

		if setting.Kind == BanTsCommentDescriptionFormat && setting.DescriptionFormat != nil {
			if !setting.DescriptionFormat.MatchString(description) {
				ctx.ReportRange(contentRange, messageBanTsCommentDescriptionFormat(
					directive, setting.DescriptionFormat.String()))
			}
		}
	}
}

// commentContent is oxc's `content_span`: the text between a comment's delimiters, and where it is.
//
// A line comment loses its two leading slashes and nothing else, because it has no closing
// delimiter. A block comment loses two characters from each end. Both were measured against the
// release binary's JSON reporter rather than derived from the shape, and the span is what every
// finding in this rule points at.
//
// The boolean is false for a block comment too short to have an interior. That case is reachable
// and it is the only one: measured against the parser, error recovery hands back an unterminated
// `/*` as a block comment of length TWO and `/*/` and `/**` as length three, and subtracting two
// from each end of those produces a range whose start is past its end, which panics when it slices
// the source. A well-formed comment cannot reach it.
//
// Two further guards were written here and then deleted, with the measurement rather than the
// instinct recorded, because both read as obviously prudent and neither could decline anything.
// A `len(text) < 2` test is unreachable: the shortest comment the parser produces is two bytes,
// probed over twelve shapes including every unterminated one, and `text[2:]` on a two-byte string
// is a legal empty slice. A `contentStart > contentEnd` test after the block branch is subsumed by
// the guard above it, since it restates `length < 4` for a block comment and cannot hold at all for
// a line comment, whose end is not moved. Both were deleted and the whole suite stayed green.
func commentContent(comment comments.Comment) (string, core.TextRange, bool) {
	text := comment.Text
	start := comment.Range.Pos()
	end := comment.Range.End()

	contentStart := start + 2
	contentEnd := end
	content := text[2:]

	if comment.IsBlock {
		if len(text) < 4 {
			return "", core.TextRange{}, false
		}
		contentEnd = end - 2
		content = text[2 : len(text)-2]
	}

	return content, core.NewTextRange(contentStart, contentEnd), true
}

// The four directives, spelled once, in upstream's order.
//
// The order does NOT decide the answer, and the intuitive reading that it does is recorded here
// because it is the one a reader will reach for. `check` looks like it must be tried after
// `nocheck` or `@ts-nocheck` would read as a `check`. It cannot: the match below is anchored at a
// fixed offset just past `@ts-`, so a directive is found only when it is a PREFIX of the remaining
// text, and two directives can both match one position only when one is a prefix of the other.
// Enumerated over all twelve ordered pairs: none is a prefix of another, `check` being a suffix of
// `nocheck` rather than a prefix. So a mutant reversing this array survives the sweep, correctly,
// and it is equivalent rather than unmeasured. Upstream's order is kept because it is upstream's.
const (
	banTsCommentExpectError = "expect-error"
	banTsCommentIgnore      = "ignore"
	banTsCommentNoCheck     = "nocheck"
	banTsCommentCheck       = "check"
)

// banTsCommentDirectives is upstream's array, in upstream's order. See the note above on why the
// order decides the answer rather than only the speed.
var banTsCommentDirectives = []string{
	banTsCommentExpectError,
	banTsCommentIgnore,
	banTsCommentNoCheck,
	banTsCommentCheck,
}

// findTsCommentDirective is upstream's function of the same name, returning the directive and the
// text following it.
//
// The parameter is the comment's content, delimiters already removed, matching what upstream passes.
// `singleLine` selects the prefix alphabet and whether only the final line is considered, which are
// the two ways a block comment differs. Both are described with measurements on the rule.
func findTsCommentDirective(content string, singleLine bool) (string, string, bool) {
	const prefix = "@ts-"

	if !strings.Contains(content, prefix) {
		return "", "", false
	}

	// For a block comment only the text after the last newline is examined, so a directive written
	// on any earlier line is invisible. A line comment has no newline to find and uses the whole
	// text. Upstream is `raw.rfind('\n').map_or(0, |i| i + 1)`.
	lineStart := 0
	if !singleLine {
		if lastNewline := strings.LastIndex(content, "\n"); lastNewline >= 0 {
			lineStart = lastNewline + 1
		}
	}
	line := content[lineStart:]

	index := strings.Index(line, prefix)
	if index < 0 {
		return "", "", false
	}

	// Everything before the prefix on that line must be whitespace, or a slash, or, in a block
	// comment only, a star. A word before the directive declines the whole comment, which is what
	// makes upstream's `// just a comment containing @ts-expect-error somewhere` clean.
	for _, character := range line[:index] {
		if isBanTsCommentWhitespace(character) {
			continue
		}
		if character == '/' {
			continue
		}
		if !singleLine && character == '*' {
			continue
		}
		return "", "", false
	}

	start := index + len(prefix)
	for _, directive := range banTsCommentDirectives {
		if start+len(directive) > len(line) {
			continue
		}
		if line[start:start+len(directive)] != directive {
			continue
		}
		// The description is everything after the directive in the WHOLE content, not only on the
		// final line, because upstream slices `raw[end..]` with `end` computed from the full-text
		// offset. For a line comment the two are the same; for a block comment the description
		// runs to the closing delimiter.
		descriptionStart := lineStart + index + len(prefix) + len(directive)
		return directive, content[descriptionStart:], true
	}
	return "", "", false
}

// isBanTsCommentWhitespace is Rust's `char::is_whitespace`, which is Unicode-aware rather than the
// five bytes a Go author would reach for.
//
// The difference is reachable: a comment written with a non-breaking space before the directive is
// declined by an ASCII test and accepted by upstream. This lists the characters Rust treats as
// whitespace, which is the Unicode White_Space property.
func isBanTsCommentWhitespace(character rune) bool {
	switch character {
	case ' ', '\t', '\n', '\v', '\f', '\r',
		0x0085, 0x00A0, 0x1680,
		0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200A,
		0x2028, 0x2029, 0x202F, 0x205F, 0x3000:
		return true
	}
	return false
}

// BanTsCommentSettingKind names which of the three shapes a directive's configuration took.
//
// A Go struct rather than an interface because the three carry different payloads and the rule
// switches on all three, which is exactly what upstream's enum does.
type BanTsCommentSettingKind string

const (
	// BanTsCommentBoolean is `true` or `false`: ban the directive outright, or allow it.
	BanTsCommentBoolean BanTsCommentSettingKind = "Boolean"

	// BanTsCommentRequireDescription is `"allow-with-description"`: allow it when it carries an
	// explanation of at least the minimum length.
	BanTsCommentRequireDescription BanTsCommentSettingKind = "RequireDescription"

	// BanTsCommentDescriptionFormat is `{ "descriptionFormat": "<regex>" }`: allow it when the
	// explanation both reaches the minimum length and matches the pattern.
	BanTsCommentDescriptionFormat BanTsCommentSettingKind = "DescriptionFormat"
)

// BanTsCommentSetting is how one directive is configured.
//
// The length check runs for both non-boolean kinds, which is why `RequireDescription` is not simply
// `DescriptionFormat` with a nil pattern in the rule's reading, even though the two behave
// identically there. Keeping them distinct is what lets the decoder refuse a shape upstream refuses.
type BanTsCommentSetting struct {
	Kind BanTsCommentSettingKind

	// Banned is read only when Kind is BanTsCommentBoolean.
	Banned bool

	// DescriptionFormat is read only when Kind is BanTsCommentDescriptionFormat, and it may be nil
	// there: upstream's `DescriptionFormat(Option<Regex>)` holds `None` for
	// `{ "descriptionFormat": null }`, which then applies the length check alone.
	DescriptionFormat *regexp.Regexp
}

// BanTsCommentOptions configures which directives report and what an acceptable description is.
//
// The authoritative surface is oxc's `BanTsCommentConfig`, five keys under
// `serde(rename_all = "kebab-case", deny_unknown_fields)`. Our rule inventory records
// `"options": "no"` for this rule, which is wrong: all five keys are accepted and each changes the
// verdict. Every default below was pinned against the release binary rather than read off that
// column, because a default that binds and does nothing is the failure this rule is most exposed to.
type BanTsCommentOptions struct {
	// TsExpectError defaults to allow-with-description, the only directive not defaulting to a
	// boolean. This is why a bare `"error"` config reports an undescribed `@ts-expect-error` and
	// stays silent on a described one.
	TsExpectError BanTsCommentSetting

	// TsIgnore defaults to banned, and it is the only directive carrying a fix.
	TsIgnore BanTsCommentSetting

	// TsNoCheck defaults to banned.
	TsNoCheck BanTsCommentSetting

	// TsCheck defaults to ALLOWED. `@ts-check` turns checking on rather than off, so banning it by
	// default would be backwards, and this is the one default a reader is likely to guess wrong.
	TsCheck BanTsCommentSetting

	// MinimumDescriptionLength defaults to 3, counted in BYTES of the trimmed description.
	MinimumDescriptionLength int
}

// settingFor is upstream's `option`, mapping a directive name onto its configuration.
//
// Upstream's fallback arm is `unreachable!`, because the caller has already matched one of the four
// names. Here it returns the allowing setting instead: a panic in a linter takes the whole run down,
// and this is the one branch where a wrong answer is preferable to no answer.
//
// The fallback is genuinely unreachable and a mutant flipping it to a ban survives the sweep,
// correctly. The distinguishing input would have to be a directive string that this switch does not
// name, and `findTsCommentDirective` returns only values drawn from `banTsCommentDirectives`, whose
// four entries are exactly the four cases below. So no such input exists, and the statement is here
// because Go requires a return rather than because any caller can reach it. It is deleted-shaped
// dead code that cannot be deleted, which is why the verdict is recorded instead.
func (o BanTsCommentOptions) settingFor(directive string) BanTsCommentSetting {
	switch directive {
	case banTsCommentIgnore:
		return o.TsIgnore
	case banTsCommentCheck:
		return o.TsCheck
	case banTsCommentNoCheck:
		return o.TsNoCheck
	case banTsCommentExpectError:
		return o.TsExpectError
	}
	return BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: false}
}

// DefaultBanTsCommentOptions is upstream's configured-nothing behavior.
//
// Exported because a fixture asserting the default has to be able to name it, and because a caller
// starting from the Go zero value would get four empty-kind settings and a minimum of zero, which
// silences the rule on every input while looking configured.
func DefaultBanTsCommentOptions() BanTsCommentOptions {
	return BanTsCommentOptions{
		TsExpectError:            BanTsCommentSetting{Kind: BanTsCommentRequireDescription},
		TsIgnore:                 BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: true},
		TsNoCheck:                BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: true},
		TsCheck:                  BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: false},
		MinimumDescriptionLength: 3,
	}
}

// banTsCommentRawOptions is the wire shape.
//
// Each directive is `json.RawMessage` rather than a typed field because the same key accepts a
// boolean, a string, and an object, which is upstream's hand-written `Deserialize` and which no
// struct tag can express. `MinimumDescriptionLength` is a pointer so that an absent key and an
// explicit zero stay distinguishable long enough for the default to be applied.
type banTsCommentRawOptions struct {
	TsExpectError            json.RawMessage `json:"ts-expect-error"`
	TsIgnore                 json.RawMessage `json:"ts-ignore"`
	TsNoCheck                json.RawMessage `json:"ts-nocheck"`
	TsCheck                  json.RawMessage `json:"ts-check"`
	MinimumDescriptionLength *int            `json:"minimumDescriptionLength"`
}

// DecodeBanTsCommentOptions maps the wire keys onto the settings the rule reads, applying upstream's
// per-key defaults for anything absent.
//
// A hand-written decoder rather than `rule.DecodeOptionsInto` because four of the five keys are
// polymorphic and none of the five defaults is a Go zero value. An unrecognized shape falls back to
// the default rather than disabling the key: upstream refuses such a configuration outright, and
// there is no error channel reaching a user here, so keeping the documented behavior beats going
// quiet on a typo.
func DecodeBanTsCommentOptions(raw []byte) (any, error) {
	decoded, err := rule.DecodeOptionsInto[banTsCommentRawOptions]()(raw)
	if err != nil {
		return DefaultBanTsCommentOptions(), err
	}

	wire, _ := decoded.(banTsCommentRawOptions)
	options := DefaultBanTsCommentOptions()

	options.TsExpectError = settingOrDefaultBanTsComment(wire.TsExpectError, options.TsExpectError)
	options.TsIgnore = settingOrDefaultBanTsComment(wire.TsIgnore, options.TsIgnore)
	options.TsNoCheck = settingOrDefaultBanTsComment(wire.TsNoCheck, options.TsNoCheck)
	options.TsCheck = settingOrDefaultBanTsComment(wire.TsCheck, options.TsCheck)

	if wire.MinimumDescriptionLength != nil {
		options.MinimumDescriptionLength = *wire.MinimumDescriptionLength
	}

	return options, nil
}

// settingOrDefaultBanTsComment reads one directive's wire value, keeping the default when it is
// absent or a shape this key does not accept.
//
// This is upstream's hand-written `Deserialize for DirectiveConfig`, which accepts a boolean, the
// exact string `allow-with-description`, or an object carrying `descriptionFormat`. Anything else
// is an error there and the default here, for the reason on the decoder.
//
// An unparseable regex is also the default rather than a panic. Upstream refuses the whole config
// at load time, which it can because it has an error channel to a user; the honest equivalent here
// is to keep the documented behavior rather than to compile a pattern that would match nothing.
func settingOrDefaultBanTsComment(raw json.RawMessage, fallback BanTsCommentSetting) BanTsCommentSetting {
	if len(raw) == 0 {
		return fallback
	}

	var asBool bool
	if json.Unmarshal(raw, &asBool) == nil {
		return BanTsCommentSetting{Kind: BanTsCommentBoolean, Banned: asBool}
	}

	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		if asString == "allow-with-description" {
			return BanTsCommentSetting{Kind: BanTsCommentRequireDescription}
		}
		return fallback
	}

	var asObject struct {
		DescriptionFormat *string `json:"descriptionFormat"`
	}
	if json.Unmarshal(raw, &asObject) == nil {
		if asObject.DescriptionFormat == nil {
			return BanTsCommentSetting{Kind: BanTsCommentDescriptionFormat}
		}
		compiled, compileError := regexp.Compile(*asObject.DescriptionFormat)
		if compileError != nil {
			return fallback
		}
		return BanTsCommentSetting{
			Kind:              BanTsCommentDescriptionFormat,
			DescriptionFormat: compiled,
		}
	}

	return fallback
}
