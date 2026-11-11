// Package shouting is what counts as shouting in a comment, in one place, for every engine that judges comments.
//
// The TypeScript rule nexus/consistency-no-shouting and the Go engine's cohere-go/consistency-no-shouting both read
// it, so an acronym added to the safe list, or a mask taught a new shape of code, quiets both at once. It lives in
// its own package, importing nothing heavier than the standard library, because the nexus rules package links the
// TypeScript checker and the Go engine must not. The Go engine's cohere-go/consistency-no-warning-comment reads its
// indented code block mask too, so the two Go rules agree on which lines of a doc comment are code shown verbatim.
package shouting

import (
	"regexp"
	"strings"

	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// What counts as shouting.
//
// All-caps reads as aggression, and it spreads: a reader mirrors the register of what they are
// answering, so one shouted line ratchets a whole file's comments into block capitals. In a codebase
// written largely by minds that read their own prose back as context, the volume compounds.
//
// The discriminator is shape, and it was bought with a corpus survey rather than guessed. A naive
// "is it uppercase" test cannot separate shouted English from an acronym, and real prose is full of
// both: the measured corpus put `NOT`, `THE`, `IS`, `AND`, `ONE` right beside SDK, API, DB, and RPC. An
// acronym is usually a consonant cluster, or carries digits or underscores; a shouted word almost
// always has a vowel and is a real word. Vowel-plus-allowlist was right across the whole corpus.
//
// Backticks are the escape hatch, and they are what keeps the allowlist small: anything that is code
// wears backticks, and inside them nothing is read as shouting.

// The words that are never shouting, and the two-letter words that are, are policy/Shouting.json's, read by
// vocabulary.go into allowedUppercaseTokens and shoutedTwoLetterWords, so every engine reads one list.

var (
	digitOrUnderscore = regexp.MustCompile(`[0-9_]`)
	vowel             = regexp.MustCompile(`[AEIOUY]`)
	uppercaseToken    = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*\b`)
)

// isShoutedToken reports whether an uppercase token is shouting rather than an acronym.
func isShoutedToken(token string) bool {
	if allowedUppercaseTokens[token] {
		return false
	}
	// Digits or underscores mean an identifier, never a shout: C1, H3, DATABASE_URL.
	if digitOrUnderscore.MatchString(token) {
		return false
	}
	if len(token) < 2 {
		return false
	}
	if len(token) == 2 {
		return shoutedTwoLetterWords[token]
	}
	// A vowel is the tell of a pronounceable word; a consonant cluster is an acronym.
	return vowel.MatchString(token)
}

var (
	// A fence spans lines, so its newlines are preserved rather than collapsed.
	fencedBlock = regexp.MustCompile("(?s)```.*?```")

	// Inline backticks, including a pair that wraps across a line. A block comment reflows, so a
	// long snippet often ends up split with the continuation behind an asterisk gutter. Matching a
	// single line would leave that pair unmasked and read the code inside it as shouting, which is
	// the opposite of what backticks are for.
	inlineBackticks = regexp.MustCompile("`[^`]*`")

	// Quoted strings, for the same reason backticks are masked: a quote marks the text inside it as
	// a literal rather than as the writer's voice. Prose about strings needs to show them, and a
	// sentence discussing "ORDER STATUS" is about a value, not shouting it.
	//
	// One line at a time here; a pair that wraps onto the next line is maskWrappedDoubleQuotes's.
	doubleQuoted = regexp.MustCompile(`"[^"\n]*"`)

	// Single-quoted tokens, deliberately narrower than the double-quote rule, because an apostrophe
	// is the same character as an opening single quote and prose is full of them. A naive pair-match
	// would start at a possessive and swallow the rest of the sentence, blinding the rule to
	// whatever followed. So only a pair that looks like a token is masked: no spaces inside.
	singleQuotedToken = regexp.MustCompile(`'[A-Za-z0-9_.\-/]+'`)

	// A single-quoted phrase in capitals, such as 'RENAME COLUMN' or 'ON CONFLICT DO NOTHING'. The
	// token pattern above stops at the first space, so before this a quoted keyword phrase was read
	// as shouting even though the writer had quoted it, which is exactly the repair the rule asks for.
	//
	// No lowercase letter is allowed inside, and that is what keeps it from repeating the apostrophe
	// failure: a possessive or a contraction is followed by lowercase prose before any closing quote,
	// so it can never form a match. The boundary half lives in maskSingleQuotedCapitalPhrases.
	singleQuotedCapitalPhrase = regexp.MustCompile(`'[A-Z0-9_][A-Z0-9_ .,:=\-/]*[A-Z0-9_]'`)

	jsDocGutter = regexp.MustCompile(`^\s*\*?\s?`)
	// A block comment's gutter alone, leading whitespace, the asterisk and one space or tab, which
	// unlike jsDocGutter leaves a line with no asterisk untouched so its indentation can be measured.
	blockCommentGutter = regexp.MustCompile(`^[ \t]*\*(?: |\t|$)`)
	exampleTag         = regexp.MustCompile(`^@example\b`)
	anyJsDocTag        = regexp.MustCompile(`^@\w+`)
	// A line opening with a shell prompt or `s c` is a command whatever follows it.
	promptLine = regexp.MustCompile(`^\s*(\$|s\s+c\b)`)
	// A line opening with one of these words is a command unless it reads as prose, which
	// commandWordReadsAsProse decides, since prose wraps onto a line that starts with them too.
	commandWordLine = regexp.MustCompile(`^\s*(ahra|git|pnpm|npm|sqlite3|curl)\s`)
	// A word an argument cannot be: a lowercase word, possibly hyphenated, as a subcommand is.
	lowercaseWord = regexp.MustCompile(`^[a-z][a-z-]*$`)
	// A dot between letters, as in a file name like PhiSocialUpload.ts.
	dotBetweenLetters = regexp.MustCompile(`[A-Za-z]\.[A-Za-z]`)
)

// MaskCodeAndCommands blanks out anything that is code, so nothing inside it is ever read as
// shouting: fenced blocks, inline backticks, quoted strings, JSDoc examples, and command lines.
//
// Replacing with spaces rather than deleting preserves every offset, so a caller reporting positions
// keeps true line and column numbers. Newlines are preserved for the same reason: collapsing a
// fence's newlines to spaces keeps character offsets right but destroys the line count, and every
// line after the fence then reports against the wrong source line.
func MaskCodeAndCommands(text string) string {
	masked := blankKeepingNewlines(fencedBlock, text)
	masked = MaskIndentedCodeBlocks(masked)
	masked = blankKeepingNewlines(inlineBackticks, masked)
	masked = blankAll(doubleQuoted, masked)
	masked = maskWrappedDoubleQuotes(masked)
	masked = blankAll(singleQuotedToken, masked)
	masked = maskSingleQuotedCapitalPhrases(masked)
	masked = maskJsDocExamples(masked)
	masked = maskCommandLines(masked)
	return masked
}

// blankKeepingNewlines replaces every match with spaces, leaving its newlines in place.
//
// One space per byte, not per rune, as every other mask here blanks, so the masked text keeps the
// input's byte length. Blanking `www.點看.com` rune by rune once shrank it by four bytes, and the Go
// engine, which slices one masked comment group by its comments' byte offsets, read past the end and
// panicked on 12 of cohere's packages. A newline is one byte, and never part of a multi-byte rune.
func blankKeepingNewlines(pattern *regexp.Regexp, text string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		blanked := []byte(match)
		for index, character := range blanked {
			if character != '\n' {
				blanked[index] = ' '
			}
		}
		return string(blanked)
	})
}

// blankAll replaces every match with spaces of the same length. Used only for single-line patterns,
// where there is no newline to preserve.
func blankAll(pattern *regexp.Regexp, text string) string {
	return pattern.ReplaceAllStringFunc(text, func(match string) string {
		return strings.Repeat(" ", len(match))
	})
}

// maskWrappedDoubleQuotes blanks a double-quoted phrase that a block comment wrapped onto the next
// line, which the one-line pattern cannot see.
//
// A comment reflows like prose, so `"the caller's job and you must NOT"` ends up as its opening half
// on one line and its closing half behind the next line's gutter (ahra's Shouting.ts did exactly
// this), and the capitals inside a quote the writer closed read as shouting. The backtick mask
// already spans a wrap for the same reason.
//
// It runs after the one-line pass, which pairs every two quotes sharing a line, so each line has at
// most one quote left. Only those leftovers pair, and only across one wrap: the leftover on a line
// with the leftover on the very next line. Pairing quotes freely across lines would let a stray
// inch mark (`a 5" board`) open a span that eats a later quote's opening, leaving the phrase that
// quote protected unmasked; confined to leftovers on adjacent lines, the pass can only blank text,
// never unblank what the one-line pass already masked.
func maskWrappedDoubleQuotes(text string) string {
	if !strings.Contains(text, "\n") || strings.Count(text, `"`) < 2 {
		return text
	}

	lines := strings.Split(text, "\n")
	for index := 0; index+1 < len(lines); index++ {
		opening := strings.IndexByte(lines[index], '"')
		if opening < 0 {
			continue
		}
		closing := strings.IndexByte(lines[index+1], '"')
		if closing < 0 {
			continue
		}
		lines[index] = lines[index][:opening] + strings.Repeat(" ", len(lines[index])-opening)
		// The closing quote is blanked with the rest, so it cannot also open a pair with the line
		// after it.
		lines[index+1] = strings.Repeat(" ", closing+1) + lines[index+1][closing+1:]
	}
	return strings.Join(lines, "\n")
}

// maskSingleQuotedCapitalPhrases blanks a capital phrase the writer set in single quotes.
//
// A quote is only a quote at a word boundary: the opening one must not follow a letter or a digit,
// and the closing one must not be followed by one. Without that, the plural possessive in "the
// users' 'NOT NULL'" could open a pair, and a quote glued to a suffix (`'RENAME COLUMN'd`) would mask
// a phrase the writer never closed. RE2 has no lookaround, so the boundary is checked on the match
// indices rather than in the pattern.
func maskSingleQuotedCapitalPhrases(text string) string {
	// Two quotes and a space between them are the least a phrase can be, and almost no comment holds
	// a quote at all, so the common case pays one byte scan.
	if strings.Count(text, "'") < 2 {
		return text
	}

	matches := singleQuotedCapitalPhrase.FindAllStringIndex(text, -1)
	if matches == nil {
		return text
	}

	masked := []byte(text)
	for _, match := range matches {
		start, end := match[0], match[1]
		if start > 0 && isAsciiLetterOrDigit(text[start-1]) {
			continue
		}
		if end < len(text) && isAsciiLetterOrDigit(text[end]) {
			continue
		}
		for index := start; index < end; index++ {
			masked[index] = ' '
		}
	}
	return string(masked)
}

// isAsciiLetterOrDigit reports whether a byte is a word character a quote could be glued to.
func isAsciiLetterOrDigit(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
		(character >= '0' && character <= '9')
}

// maskJsDocExamples blanks the lines under an @example tag, which are code whatever they look like.
//
// The lines under it are a call and its result, written the way a reader would type them, so
// hour24To12(0) with a comment showing 'AM' is a snippet rather than someone shouting. Backticking
// inside an example is the wrong repair too: the whole block is already code by context. The block
// runs from the tag to the next tag or the end of the comment, which is how JSDoc delimits it.
func maskJsDocExamples(text string) string {
	// An example block opens with an `@example` tag, so a text with no `@` at all cannot hold one.
	// Measured at 12.2ms of the 35.7ms masking cost before this check, from splitting and running
	// two regexes per line on every comment in the tree.
	if !strings.ContainsRune(text, '@') {
		return text
	}

	lines := strings.Split(text, "\n")
	insideExample := false
	for index, line := range lines {
		withoutGutter := jsDocGutter.ReplaceAllString(line, "")

		if exampleTag.MatchString(withoutGutter) {
			insideExample = true
			lines[index] = strings.Repeat(" ", len(line))
			continue
		}
		// Any other JSDoc tag closes the example.
		if insideExample && anyJsDocTag.MatchString(withoutGutter) {
			insideExample = false
		}
		if insideExample {
			lines[index] = strings.Repeat(" ", len(line))
		}
	}
	return strings.Join(lines, "\n")
}

// MaskIndentedCodeBlocks blanks an indented code block, the markdown form of a fence: lines indented
// four spaces or a tab, opened after a blank line (or at the start of the text) and running until a
// line that is not indented. Without it, a sandbox command posted as an indented block had its
// $HOME and TMPDIR read as shouting, and the quieted draft lowercased them into a command that no
// longer ran, while the same command in a fence survived (#vy055vj).
//
// An indented line straight after prose is a paragraph continuation in CommonMark, not code, so it
// is still read; that is what the blank-line condition is for. A block comment's gutter is set aside
// before the indentation is measured, so a comment's own indentation is never mistaken for a code
// block, while an indented block written inside a JSDoc comment still is one. Mirrors nexus's
// Shouting.ts, so the quieter and this rule agree on what is code.
//
// Exported for the Go engine's comment rules, which mask a comment group's GoCommentBody texts joined
// by newlines with it, so the shouting rule and the warning-comment rule agree on which lines of a doc
// comment are a code block shown verbatim.
func MaskIndentedCodeBlocks(masked string) string {
	// Nothing can be indented four deep without four spaces or a tab somewhere.
	if !strings.Contains(masked, "    ") && !strings.ContainsRune(masked, '\t') {
		return masked
	}

	lines := strings.Split(masked, "\n")
	previousIsBlank := true
	insideBlock := false
	for index, line := range lines {
		content := blockCommentGutter.ReplaceAllString(line, "")
		if text.TrimWhitespace(content) == "" {
			previousIsBlank = true
			continue
		}
		isIndented := strings.HasPrefix(content, "    ") || strings.HasPrefix(content, "\t")
		insideBlock = isIndented && (insideBlock || previousIsBlank)
		previousIsBlank = false
		if insideBlock {
			lines[index] = strings.Repeat(" ", len(line))
		}
	}
	return strings.Join(lines, "\n")
}

// GoCommentBody is the text of a Go comment that MaskIndentedCodeBlocks measures, given the comment's
// text as go/ast holds it. A // comment loses its slashes and the one space after them, as go/ast's
// CommentGroup.Text strips them, so a tab or the further spaces after them are the indentation of a
// go/doc code block. A block comment is kept whole, delimiters included, since the mask already knows
// its gutter. The body is always a suffix of text, so its offsets map back by the length cut.
func GoCommentBody(text string) string {
	body, isLineComment := strings.CutPrefix(text, "//")
	if !isLineComment {
		return text
	}
	return strings.TrimPrefix(body, " ")
}

// maskCommandLines blanks a line that is plainly a command the reader is meant to run.
func maskCommandLines(text string) string {
	// Nothing to mask unless one of the command starters appears somewhere in the text. One scan of
	// the whole string is far cheaper than two regexes per line, and the overwhelming majority of
	// comments hold no command at all.
	//
	// Measured on 300 real files: this stage was 15.2ms of the 35.7ms masking cost, the single most
	// expensive step, because it ran a gutter-stripping ReplaceAllString and a match on every line
	// of every file. The abbreviation gate taught the same lesson one layer down: the cheapest
	// question first, and a per-line regex is where a small constant gets multiplied.
	if !containsAnyCommandStarter(text) {
		return text
	}
	return blankCommandLines(text)
}

// blankCommandLines is maskCommandLines without its gate: every line that is a command, blanked.
//
// A line is read in the state the lines before it were left in, so a line already blanked as code,
// by an earlier mask or as a command here, is blank to the line after it. That is what keeps a run
// of commands masked: each one follows a blank line, so none of them continues a sentence.
func blankCommandLines(text string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		// The gutter is stripped before matching, which the TypeScript original does not do. Its
		// pattern anchors at the start of the line, so a command inside a block comment sits behind
		// a " * " gutter and never matches, and the rule reads the command as prose. The
		// example-block masker in this same file already strips the gutter for exactly this reason,
		// so this follows that convention rather than inventing one.
		content := jsDocGutter.ReplaceAllString(line, "")
		isCommand := promptLine.MatchString(content)
		if starter := commandWordLine.FindString(content); starter != "" {
			// The first line has nothing before it, which reads as blank: a comment opening on a
			// command word continues no sentence.
			previousLine := ""
			if index > 0 {
				previousLine = lines[index-1]
			}
			isCommand = !commandWordReadsAsProse(previousLine, line, content[len(starter):])
		}
		if isCommand {
			lines[index] = strings.Repeat(" ", len(line))
		}
	}
	return strings.Join(lines, "\n")
}

// commandWordReadsAsProse reports whether a line opening with a command word is a sentence that
// wrapped onto it, given the line before it as masked so far, the line, and what follows the word.
//
// The words are English too, so a comment rewrapped by a formatter puts "git and go are stand-ins"
// or "ahra run, which waits" at the start of a line, and blanking it whole hid every shout on it.
// A list of prose words would never be finished, so the test is structure, and it needs both halves:
//
//   - The line continues a sentence: the line before it holds text that does not end a sentence or
//     open a block, and the line sits no deeper than it. A command shown after `Usage:`, after a
//     blank line, or under another command follows a colon or a blank, and a table of commands set
//     in under a sentence sits deeper than it, so each stays masked.
//   - What follows the word is not shaped like a command's arguments: a flag, a path, a placeholder,
//     an assignment, a file name, a word in capitals, or the line's end, reached through at most two
//     lowercase words such as `git merge-base` or `ahra os skills` take as subcommands. Prose
//     reaches a capitalized word, a comma, or a third plain word first.
//
// Only a line passing both is read, so a command that wraps out of a sentence still stays masked.
func commandWordReadsAsProse(previousLine string, line string, afterWord string) bool {
	previous := text.TrimWhitespace(jsDocGutter.ReplaceAllString(previousLine, ""))
	// A block comment's opening line is its delimiter, with no sentence to continue.
	previous = text.TrimWhitespace(strings.TrimLeft(strings.TrimPrefix(previous, "/*"), "*"))
	if previous == "" || strings.ContainsAny(previous[len(previous)-1:], ".:!?;") {
		return false
	}
	// Measured past blockCommentGutter, which keeps the indentation jsDocGutter strips from a line
	// with no asterisk. A sentence wraps back to where it started, never further in.
	if indentation(line) > indentation(previousLine) {
		return false
	}
	words := strings.Fields(afterWord)
	for index := range 3 {
		if index == len(words) || isCommandShaped(words[index]) {
			return false
		}
		if !lowercaseWord.MatchString(words[index]) {
			return true
		}
	}
	return true
}

// indentation is how many spaces or tabs a line's text sits in from its block comment gutter.
func indentation(line string) int {
	body := blockCommentGutter.ReplaceAllString(line, "")
	return len(body) - len(strings.TrimLeft(body, " \t"))
}

// isCommandShaped reports whether a word is shaped like a command's argument rather than prose.
//
// A word wholly in capitals counts, as an SQL keyword after `sqlite3 db` or an environment variable
// does. A sentence could shout that early too, and that shout goes unread, which is the side to err
// on: a word in capitals this close to the command word is likelier its argument, and reading an
// argument as a shout is a false finding.
func isCommandShaped(word string) bool {
	return strings.ContainsAny(word[:1], "-<[\"'`+/~.") ||
		strings.ContainsAny(word, "/=") ||
		dotBetweenLetters.MatchString(word) ||
		strings.HasSuffix(word, ":") ||
		uppercaseToken.FindString(word) == word
}

// ShoutedToken is one shouted word in a masked text, and the byte offset it starts at.
type ShoutedToken struct {
	Text   string
	Offset int
}

// ShoutedTokensInMasked returns every shouted token in text MaskCodeAndCommands has already masked, each occurrence
// where it sits, in order. It is for a caller that reports each word at the word: masking keeps every byte's place,
// so an offset into the masked text is the same byte offset into the text it masked, multi-byte runes before the
// word, or inside code masked before it, included.
func ShoutedTokensInMasked(masked string) []ShoutedToken {
	var tokens []ShoutedToken
	for _, location := range uppercaseToken.FindAllStringIndex(masked, -1) {
		token := masked[location[0]:location[1]]
		if isShoutedToken(token) {
			tokens = append(tokens, ShoutedToken{Text: token, Offset: location[0]})
		}
	}
	return tokens
}

// commandStarters are the literal prefixes promptLine and commandWordLine can match after their
// optional leading space.
//
// Kept beside the patterns deliberately: if one gains an alternative, this must gain it too,
// or maskCommandLines will stop masking that command and the rule will read it as prose. The gate is
// a superset check, so an extra entry here costs one wasted line scan and a missing one costs a
// false finding.
// The trailing space is dropped from each entry on purpose: the pattern separates the command from
// its argument with `\s`, which matches a tab as well as a space, so requiring a literal space here
// would miss `git\tstatus`. A bare word over-admits slightly, which is the safe direction.
var commandStarters = []string{"$", "ahra", "git", "pnpm", "npm", "sqlite3", "curl"}

// structureCommandStarter is the `s\s+c` arm, which no literal can express.
var structureCommandStarter = regexp.MustCompile(`s\s+c`)

// containsAnyCommandStarter reports whether any command prefix appears anywhere in the text.
//
// A superset of what `promptLine` and `commandWordLine` can match, never a subset, for the same
// reason the abbreviation gate is: a gate that admits too much costs one wasted line scan, and a
// gate that admits too little silently stops masking a command, so the rule reads it as prose and
// shouts at it.
//
// The `s\s+c` arm is the one that cannot be reduced to a literal, since the pattern accepts any run
// of whitespace between the two letters. A fixture caught that: a hand-written `"s c"` entry misses
// `s   c`, and 800 real files did not happen to contain the multi-space form. **Absence from a
// corpus is not absence in general**, which is why this arm keeps its regex rather than a literal.
func containsAnyCommandStarter(text string) bool {
	for _, starter := range commandStarters {
		if strings.Contains(text, starter) {
			return true
		}
	}
	return structureCommandStarter.MatchString(text)
}

// HasAdjacentUppercaseLetters reports whether two uppercase letters appear in a row.
//
// That is the shortest thing `uppercaseToken` can match that `isShoutedToken` will not immediately
// decline, so it is a superset of what the rule can report. Over-approximating costs one masking
// pass on a comment that turns out to hold only acronyms; under-approximating would silence the rule
// for a real shout, which is the failure that cannot be allowed. Masking only ever replaces
// characters with spaces, never adds an uppercase letter, so a text that fails this test cannot pass
// it after masking either, and a caller checks it before masking to skip both stages.
//
// Measured over 8,814 real comments: 5.7% hold two adjacent uppercase letters. The byte scan is
// 579µs against 5,413µs for the regex over the same corpus, and masking is another 6,019µs that
// this avoids entirely.
func HasAdjacentUppercaseLetters(text string) bool {
	previousWasUppercase := false
	for index := 0; index < len(text); index++ {
		character := text[index]
		isUppercase := character >= 'A' && character <= 'Z'
		if isUppercase && previousWasUppercase {
			return true
		}
		previousWasUppercase = isUppercase
	}
	return false
}

// QuotedTokenList renders the first few shouted tokens for a reader, so the finding names what to
// look at rather than making them rescan the comment. It lives beside the tokens so every engine's
// finding names them the same way.
//
// Capped because a banner line can hold a dozen, and a message that lists all of them is harder to
// act on than one that names the first few.
func QuotedTokenList(tokens []string) string {
	const maximumListed = 4

	listed := tokens
	if len(listed) > maximumListed {
		listed = listed[:maximumListed]
	}

	quoted := make([]string, 0, len(listed))
	for _, token := range listed {
		quoted = append(quoted, `"`+token+`"`)
	}
	return strings.Join(quoted, ", ")
}

// generatedMarker is the line go.dev/s/generatedcode says marks a file as generated, spelled exactly so tools can find
// it. Its capitals are the convention's, which no writer can lower, so it is notation in any language.
var generatedMarker = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// IsGeneratedMarker reports whether a line comment's text, its slashes included, is the generated-code marker. Where in
// a file the line counts is the engine's to say, since the convention places it before the first token.
func IsGeneratedMarker(commentText string) bool {
	return generatedMarker.MatchString(commentText)
}
