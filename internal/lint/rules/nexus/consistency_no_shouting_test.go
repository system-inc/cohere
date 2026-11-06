package nexus

import (
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/lint/testing"
)

const shoutingFile = "/repository/source/Thing.ts"

func TestConsistencyNoShoutingFires(test *testing.T) {
	test.Parallel()

	cases := []struct {
		name       string
		sourceText string
		words      []string
	}{
		{"shouted word in a line comment", "// NEVER cache this\nexport const Value = 1;\n", []string{"NEVER"}},
		{
			"shouted word in a block comment",
			"/* this is REALLY important */\nexport const Value = 1;\n",
			[]string{"REALLY"},
		},
		{"banner line", "// DO NOT EDIT\nexport const Value = 1;\n", []string{"DO", "NOT", "EDIT"}},
		{"shouted two-letter word", "// this IS the one\nexport const Value = 1;\n", []string{"IS"}},
		{"shouted word in jsdoc", "/** ALWAYS returns a copy */\nexport const Value = 1;\n", []string{"ALWAYS"}},
		// An unterminated backtick must not mask the rest of the comment. If it did, opening a
		// backtick would be a way to turn the rule off for everything after it.
		{
			"unclosed backtick does not mask the rest",
			"// `code and then NEVER do this\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

func TestConsistencyNoShoutingStaysSilent(test *testing.T) {
	test.Parallel()

	// These are the discriminating cases. A rule that only detects capitals would fire on every
	// one of them, and a rule that fires on ordinary technical prose is a rule people disable.
	cases := []struct {
		name       string
		sourceText string
	}{
		{"ordinary prose", "// parse the body before validating it\nexport const Value = 1;\n"},
		{"allowlisted acronyms", "// the JSON body arrives over HTTP from the API\nexport const Value = 1;\n"},
		{"consonant cluster acronym", "// send it over RPC to the DB\nexport const Value = 1;\n"},
		{"backticked identifier", "// the `NOT_FOUND` branch returns early\nexport const Value = 1;\n"},
		{"backticked shouted word", "// the `NEVER` constant is a sentinel\nexport const Value = 1;\n"},
		{"identifier with underscore", "// read DATABASE_URL from the environment\nexport const Value = 1;\n"},
		{"identifier with a digit", "// the H3 heading and the C1 column\nexport const Value = 1;\n"},
		{"currency code", "// amounts in KRW have no minor unit\nexport const Value = 1;\n"},
		{"double quoted literal", "// \"ORDER STATUS\" is the column label\nexport const Value = 1;\n"},
		{"single quoted token", "// pass 'US' as the region\nexport const Value = 1;\n"},
		{"fenced block", "/*\n * ```\n * NEVER DO THIS\n * ```\n */\nexport const Value = 1;\n"},
		{"command line", "/*\n * git commit --amend NEVER\n */\nexport const Value = 1;\n"},
		{"two-letter abbreviation", "// the IP address and the OS version\nexport const Value = 1;\n"},
		{"notation shorthand", "// TODO: handle the empty case\nexport const Value = 1;\n"},
		{"no comments at all", "export const Value = 1;\n"},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}
}

// The 2026-10-01 rule review hand-checked every finding on ahra. Five were acronyms the vowel test
// misread as raised voice, and those lines are here verbatim as must-stay-silent cases. The
// true positives from the same review sit beside them, verbatim, so the safe list growing can never
// quietly swallow a real shout or a code constant that still owes its backticks.
func TestConsistencyNoShoutingKnowsTheAcronymsTheReviewFound(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{"TUI in a jsdoc block (CodexConversationSource.ts)",
			"/**\n * A freshly started Codex TUI writes session metadata before the human has typed\n */\nexport const Value = 1;\n"},
		{"TUI in a line comment (CreateMindRuntimeTables.ts)",
			"// adapter projects its native TUI into the same two columns.\nexport const Value = 1;\n"},
		{"ARP (UnifiProtectChimeCommandLineInterface.ts)",
			"/*\n * kept dialing .176 on port 7442 for six months (seen on the wire 2026-10-01 as ARP who-has every\n */\nexport const Value = 1;\n"},
		{"CDATA (PensieveDailies.ts)",
			"// XML character references are literal inside CDATA. Unwrap the one admitted\nexport const Value = 1;\n"},
		{"CAF (SpeechRenderer.ts)",
			"// Writes `text` spoken by `voice` (an identifier or a voice name) to a CAF file at `outputPath`\nexport const Value = 1;\n"},
		// Not from the ahra review: the health-records interoperability standard, flagged in
		// www-phi-health's ClinicalRecords.tsx.
		{"FHIR (ClinicalRecords.tsx)",
			"// FHIR interpretation code: \"N\" = Normal, \"A\"/\"AA\"/\"H\"/\"HH\"/\"L\"/\"LL\" = abnormal.\nexport const Value = 1;\n"},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		// An errno name is a literal the reader matches against what the system emits, so it is code
		// and owes backticks. A dictionary test would read it as an acronym and let it through.
		{"errno name (AhraOsPositionFifo.ts)",
			"/*\n * wake and a delivery to a reader-less pipe already fails ENXIO and shrugs. Idempotent,\n */\nexport const Value = 1;\n",
			[]string{"ENXIO"}},
		{"emphasis (AhraOsPositionFifo.ts)",
			"/*\n * safe while watchers are armed BECAUSE the pipe is only the fast-path poke: the durable\n */\nexport const Value = 1;\n",
			[]string{"BECAUSE"}},
		{"emphasis (NextEventLoopTurn.ts)",
			"/**\n * This YIELDS. It makes no claim about what finished, because it cannot know\n */\nexport const Value = 1;\n",
			[]string{"YIELDS"}},
		{"banner and a sqlite keyword (FinancePositionCommandLineInterface.ts)",
			"/*\n * OUTPUT MUST BE VALID INPUT. The name is printed in FULL, never fit() to the column width: a\n */\nexport const Value = 1;\n",
			[]string{"OUTPUT", "MUST", "BE", "VALID", "INPUT", "FULL"}},
		{"emphasis (AhraOsTriggers.ts)",
			"/*\n * DO NOT rename this symbol to `positionsMissingPortrait` with the rest of the\n */\nexport const Value = 1;\n",
			[]string{"DO", "NOT"}},
		{"emphasis (FinanceHoldingsCommandLineInterface.ts)",
			"// straight back in. A miss is a LOUD failure with near misses named by ID, never a silent\nexport const Value = 1;\n",
			[]string{"LOUD"}},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

// Kirk's ruling on the review: all caps belongs in backticks, single quotes or double quotes unless
// it is on the safe list. Backticks, double quotes and a single-quoted token were already masked; a
// single-quoted phrase was not, because an apostrophe is the same character as an opening quote and
// a naive pair-match swallows the rest of a sentence. These pin the narrow version: the opening quote
// follows no letter or digit, the closing one is followed by none, and nothing inside is lowercase.
func TestConsistencyNoShoutingMasksASingleQuotedCapitalPhrase(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{"quoted sql phrase", "// a 'RENAME COLUMN' statement is not idempotent\nexport const Value = 1;\n"},
		{
			"quoted phrase at the start of the comment",
			"// 'ON CONFLICT DO NOTHING' keeps the first row\nexport const Value = 1;\n",
		},
		{
			"quoted phrase before punctuation",
			"// the pragma reads 'SYNCHRONOUS FULL', then commits\nexport const Value = 1;\n",
		},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		// A plural possessive is an apostrophe after a letter. It must not open a quote that the
		// next apostrophe closes, or the shout between them disappears.
		{
			"possessive apostrophe opens nothing",
			"// the users' data is NEVER cached by the dogs' walker\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
		{"contraction opens nothing", "// don't EVER cache what isn't yours\nexport const Value = 1;\n", []string{"EVER"}},
		// The apostrophe after `BOSS` follows a letter, so it opens nothing, and the closing apostrophe
		// after `ORDERS` must not pair with it to hide `ORDERS`.
		{
			"opening quote glued to a letter",
			"// the BOSS'S ORDERS' wording\nexport const Value = 1;\n",
			[]string{"BOSS", "ORDERS"},
		},
		{
			"closing quote glued to a letter",
			"// a 'RENAME COLUMN'd table is NEVER safe\nexport const Value = 1;\n",
			[]string{"RENAME", "COLUMN", "NEVER"},
		},
		// The apostrophe hazard itself: a leading apostrophe after a space and a plural possessive
		// later in the sentence look like a pair at both boundaries. Lowercase inside is what refuses it.
		{
			"apostrophes that bracket prose",
			"// the '90s were LOUD and the users' data was quiet\nexport const Value = 1;\n",
			[]string{"LOUD"},
		},
		{
			"lowercase inside the quotes",
			"// the 'tis season, NEVER the 'quiet' one\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
		{"shout outside a quoted phrase", "// 'NOT NULL' is ALWAYS enforced\nexport const Value = 1;\n", []string{"ALWAYS"}},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

// An indented code block is code, like a fence (#vy055vj): a sandbox command's `TMPDIR` and `$HOME` are
// not a raised voice. The blank line before it is what makes it a block; an indented line straight
// after prose continues the paragraph and is still read. A block comment's own indentation is set
// aside first, so only a block indented past the gutter counts. Mirrors nexus's Shouting.test.ts.
func TestConsistencyNoShoutingMasksAnIndentedCodeBlock(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{
			"indented block in a block comment",
			"/**\n * Run it sandboxed:\n *\n *     TMPDIR=$HOME/tmp sandbox-exec -f profile.sb node run.mjs\n */\nexport const Value = 1;\n",
		},
		{"tab-indented block", "/*\nRun it:\n\n\tHOME=/x TMPDIR=/y node run.mjs\n*/\nexport const Value = 1;\n"},
		{
			"fenced block, the control",
			"/**\n * Run it:\n * ```\n * TMPDIR=$HOME/tmp node run.mjs\n * ```\n */\nexport const Value = 1;\n",
		},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		{
			"indented line straight after prose",
			"/*\nThis is prose\n    and you must NEVER do it\n*/\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
		{
			"prose after an indented block",
			"/**\n * Run:\n *\n *     HOME=/x run\n *\n * Then ALWAYS check.\n */\nexport const Value = 1;\n",
			[]string{"ALWAYS"},
		},
		{
			"a deeply indented comment line is prose",
			"export function run() {\n    /**\n     * You must NEVER do this.\n     */\n    return 1;\n}\n",
			[]string{"NEVER"},
		},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

// Kirk's ruling: a quoted all-caps phrase is a literal, and a block comment that reflows a quote
// onto the next line has not unquoted it. The first silent row is ahra's Shouting.ts verbatim, where
// the quote opens on one line and closes behind the next line's gutter. The reporting rows pin the
// edges: capitals after the closing quote, a stray inch mark with nothing on the next line to close
// it, and a quote that only closes two lines later (one wrap is the limit).
func TestConsistencyNoShoutingMasksADoubleQuoteThatWraps(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{"Shouting.ts, a quote wrapped behind a gutter",
			"/*\n * Kept apart from the token pattern above so prose stays safe: the run must start and end on a\n * capital, digit or underscore and hold only capitals and token punctuation, so \"the caller's job\n * and you must NOT\" still cannot pair its apostrophe with a later one. Mirrors cohere's shouting.go.\n */\nexport const Value = 1;\n"},
		{"a wrapped quote after a one-line quote on the same line",
			"/*\n * \"ok\" then \"a phrase that\n * says NEVER\" here\n */\nexport const Value = 1;\n"},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		{"capitals after the closing quote on the next line",
			"/*\n * the \"quiet\n * phrase\" is NEVER loud\n */\nexport const Value = 1;\n", []string{"NEVER"}},
		{"a stray inch mark opens nothing",
			"/*\n * a 5\" board\n * is NEVER warped\n */\nexport const Value = 1;\n", []string{"NEVER"}},
		{"a quote that closes two lines later",
			"/*\n * the \"first\n * line is NEVER\n * closed\" here\n */\nexport const Value = 1;\n", []string{"NEVER"}},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

func TestConsistencyNoShoutingRespectsTheAllowOption(test *testing.T) {
	test.Parallel()

	sourceText := "// the WIDGET subsystem owns this\nexport const Value = 1;\n"

	withoutOption := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, sourceText)
	expectShoutedWords(test, withoutOption, sourceText, "WIDGET")

	withOption := rule_testing.RunWithOptions(test, ConsistencyNoShouting, shoutingFile, sourceText,
		ConsistencyNoShoutingOptions{Allow: []string{"WIDGET"}})
	rule_testing.ExpectClean(test, withOption)
}

// The message has to name what it saw, and the finding has to sit on it. A finding that says only
// "this comment shouts", over the whole comment, makes the reader rescan a paragraph to find the
// word that tripped it.
func TestConsistencyNoShoutingReportsEachWordAtTheWord(test *testing.T) {
	test.Parallel()

	sourceText := "// NEVER cache this, it is REALLY bad, NEVER\nexport const Value = 1;\n"
	result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, sourceText)
	expectShoutedWords(test, result, sourceText, "NEVER", "REALLY", "NEVER")
	// The second `NEVER` is the comment's last word, so a finding still covering the comment would
	// start where the first one does.
	if result.Diagnostics[0].Range.Pos() == result.Diagnostics[2].Range.Pos() {
		test.Fatalf("both findings on `NEVER` start at %d", result.Diagnostics[0].Range.Pos())
	}
}

// A run of // lines is one text, as a block comment is, so a backtick pair or a fence wrapped across
// its lines masks what it holds (the corpus's sh04, sh05 and sh14). What ends a run is structure: a
// blank line, code between the comments, or a block comment, and each pins one of those edges with
// the words its two halves then report alone.
func TestConsistencyNoShoutingMasksARunOfLineCommentsAsOneText(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{"a backtick pair across two lines", "// The flag `KEEP\n// GOING` stays set.\nexport const Value = 1;\n"},
		{"a fence across the lines", "// Run this:\n// ```\n// MAKE BUILD\n// ```\nexport const Value = 1;\n"},
		// Pairing backticks alone would join the fence's third with the stray one and read what
		// follows it, so this holds the fence mask itself.
		{
			"a fence holding a stray backtick",
			"// Run this:\n// ```\n// say ` then STOP\n// ```\nexport const Value = 1;\n",
		},
		{"a double quote across two lines", "// the \"quiet\n// PHRASE\" stays\nexport const Value = 1;\n"},
		{
			"an indented run",
			"export function run() {\n    // The flag `KEEP\n    // GOING` stays set.\n    return 1;\n}\n",
		},
		{"a run after a trailing comment", "export const Value = 1; // The flag `KEEP\n// GOING` stays set.\n"},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		{
			"a blank line ends the run",
			"// The flag `KEEP\n\n// GOING` stays set.\nexport const Value = 1;\n",
			[]string{"KEEP", "GOING"},
		},
		{
			"code between the comments ends the run",
			"export const First = 1; // The flag `KEEP\nexport const Second = 2; // GOING` stays set.\n",
			[]string{"KEEP", "GOING"},
		},
		{
			"a block comment is not part of a run",
			"/* The flag `KEEP */\n// GOING` stays set.\nexport const Value = 1;\n",
			[]string{"KEEP", "GOING"},
		},
		{
			"each line reports its own words where they are",
			"// The first line is quiet,\n// and this one is NEVER quiet.\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

// The generated-code marker's capitals are fixed by go.dev/s/generatedcode, which tools match
// exactly, so the line is not read (the corpus's sh10). The convention counts it only before the
// file's first token and only as a line comment spelled exactly, so the same words anywhere else
// still report.
func TestConsistencyNoShoutingSetsAsideTheGeneratedMarker(test *testing.T) {
	test.Parallel()

	silent := []struct {
		name       string
		sourceText string
	}{
		{"the marker", "// Code generated by probe. DO NOT EDIT.\nexport const Value = 1;\n"},
		{
			"the marker after a header comment",
			"// Copyright the authors.\n// Code generated by probe. DO NOT EDIT.\nexport const Value = 1;\n",
		},
		{"the marker after a shebang", "#!/usr/bin/env node\n// Code generated by probe. DO NOT EDIT.\nconsole.log(1);\n"},
		{"the marker in a file with no code", "// Code generated by probe. DO NOT EDIT.\n"},
	}
	for _, testCase := range silent {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			rule_testing.ExpectClean(test, result)
		})
	}

	fires := []struct {
		name       string
		sourceText string
		words      []string
	}{
		{
			"the marker's words after the first token",
			"export const Value = 1;\n// Code generated by probe. DO NOT EDIT.\n",
			[]string{"DO", "NOT", "EDIT"},
		},
		{
			"the marker's words in a block comment",
			"/* Code generated by probe. DO NOT EDIT. */\nexport const Value = 1;\n",
			[]string{"DO", "NOT", "EDIT"},
		},
		{
			"the marker without its period",
			"// Code generated by probe. DO NOT EDIT\nexport const Value = 1;\n",
			[]string{"DO", "NOT", "EDIT"},
		},
		{
			"the line after the marker is read",
			"// Code generated by probe. DO NOT EDIT.\n// and NEVER by hand\nexport const Value = 1;\n",
			[]string{"NEVER"},
		},
	}
	for _, testCase := range fires {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			result := rule_testing.Run(test, ConsistencyNoShouting, shoutingFile, testCase.sourceText)
			expectShoutedWords(test, result, testCase.sourceText, testCase.words...)
		})
	}
}

// expectShoutedWords asserts one finding per word, in order, each covering exactly that word in the
// source and naming it in its message.
func expectShoutedWords(test *testing.T, result rule_testing.Result, sourceText string, words ...string) {
	test.Helper()

	wantIds := make([]string, 0, len(words))
	for range words {
		wantIds = append(wantIds, "shoutingInComment")
	}
	rule_testing.ExpectFindings(test, result, wantIds...)
	for index, word := range words {
		diagnostic := result.Diagnostics[index]
		if covered := sourceText[diagnostic.Range.Pos():diagnostic.Range.End()]; covered != word {
			test.Fatalf("finding %d covers %q, expected the word %q", index, covered, word)
		}
		if !strings.Contains(diagnostic.Message.Description, `"`+word+`"`) {
			test.Fatalf("finding %d's message does not name %q: %s", index, word, diagnostic.Message.Description)
		}
	}
}
