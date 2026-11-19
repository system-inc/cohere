package shouting

import (
	"regexp"
	"slices"
	"testing"
)

// uppercaseRun is nexus's uppercaseRunPattern, the tokens a hyphen or an ampersand joins read as one run. Only the
// tests match it: nextUppercaseRun is the same scan by hand, as nextUppercaseToken is uppercaseToken's.
var uppercaseRun = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*(?:[-&][A-Z0-9_]+)*\b`)

// joinerPattern and digitPattern are the patterns nexus's isJoinedIdentifier tests a run with, so the reference below
// shares no code with the reader it checks.
var (
	joinerPattern = regexp.MustCompile(`[-&]`)
	digitPattern  = regexp.MustCompile(`[0-9]`)
)

// runScanCases are the shapes a hand scan of uppercaseRun could get wrong: a joiner doubled, trailing or leading, a
// part that opens on a digit or an underscore or holds a lowercase letter, a space beside a joiner, and a mask's
// delimiter pressed against a joiner, so the masking test sees each mask open and close beside a run.
var runScanCases = []string{
	"F-PROMISE-13", "AT&T", "AT &T", "AT& T", "A-TEAM", "WILL-NOT", "LOAD-BEARING", "ROCK&ROLL", "X-RAY", "F-HW",
	"BEN-UNG-00-001", "R&D Q&A P&G", "F-Promise", "WILL-13x", "WILL-NOTx", "WILL--NOT", "WILL-&NOT", "WILL-", "-WILL",
	"&WILL&", "13-WILL-NOT", "WILL-_X", "WILL-_", "_X-WILL", "WILL-NOT-a", "aWILL-NOT", "WILL-NOTé", "éF-WILL",
	"`F`-WILL", "F-`WILL`", "'F'-WILL", "F-'WILL'", "\"F\"-WILL", "F-\"WILL\"", "F-\nWILL", "a 'X-RAY' scan",
	"Per F-PROMISE-13 this is LOAD-BEARING.",
}

// referenceUppercaseTokens is nexus's findUppercaseTokens written with its own patterns: every run uppercaseRun finds,
// skipped when it is a joined identifier, otherwise every token uppercaseToken finds inside it alone.
func referenceUppercaseTokens(text string) [][]int {
	var tokens [][]int
	for _, run := range uppercaseRun.FindAllStringIndex(text, -1) {
		if referenceIsJoinedIdentifier(text[run[0]:run[1]]) {
			continue
		}
		for _, token := range uppercaseToken.FindAllStringIndex(text[run[0]:run[1]], -1) {
			tokens = append(tokens, []int{run[0] + token[0], run[0] + token[1]})
		}
	}
	return tokens
}

// referenceIsJoinedIdentifier is nexus's isJoinedIdentifier line for line: joined, then a digit anywhere, or a part
// one letter long.
func referenceIsJoinedIdentifier(run string) bool {
	if !joinerPattern.MatchString(run) {
		return false
	}
	if digitPattern.MatchString(run) {
		return true
	}
	return slices.ContainsFunc(joinerPattern.Split(run, -1), func(part string) bool {
		return len(part) == 1
	})
}

// The hand scans find exactly what nexus's patterns find, the runs where uppercaseRun puts them and the tokens they
// leave where findUppercaseTokens puts them, over the shapes above, every string of up to four bytes from an alphabet
// of each kind of byte a run's edges tell apart, and the corpus.
func TestNextUppercaseRunMatchesThePattern(test *testing.T) {
	test.Parallel()
	texts := slices.Concat(tokenScanCases, runScanCases)
	alphabet := []string{"A", "Z", "a", "1", "_", " ", "-", "&", "'", "\xc3\xa9"}
	generated := []string{""}
	for range 4 {
		var longer []string
		for _, prefix := range generated {
			for _, character := range alphabet {
				longer = append(longer, prefix+character)
			}
		}
		texts = append(texts, longer...)
		generated = longer
	}
	texts = append(texts, realCommentCorpus(test)...)
	for _, text := range texts {
		want := uppercaseRun.FindAllStringIndex(text, -1)
		var got [][]int
		for start, end := nextUppercaseRun(text, 0); start >= 0; start, end = nextUppercaseRun(text, end) {
			got = append(got, []int{start, end})
		}
		if !slices.EqualFunc(got, want, slices.Equal) {
			test.Fatalf("over %q the scan finds the runs %v, and uppercaseRun finds %v", text, got, want)
		}
		var gotTokens [][]int
		for start, end := range uppercaseTokens(text) {
			gotTokens = append(gotTokens, []int{start, end})
		}
		if wantTokens := referenceUppercaseTokens(text); !slices.EqualFunc(gotTokens, wantTokens, slices.Equal) {
			test.Fatalf("over %q the scan leaves the tokens %v, and nexus's patterns leave %v", text, gotTokens,
				wantTokens)
		}
	}
	test.Logf("the scans match uppercaseRun and findUppercaseTokens over %d texts", len(texts))
}

// A run of capitals joined by a hyphen or an ampersand is read whole first, as nexus reads it (#1z7kbtz): a digit
// anywhere or a one-letter part makes it an identifier that reports nothing, and a run with neither is read a token at
// a time as before. The cases are nexus's own Shouting.test.ts for the rule, with the shapes the task named beside
// them, so each verdict here is the one nexus gives.
func TestJoinedRunsAreReadWholeAsNexusReadsThem(test *testing.T) {
	test.Parallel()
	cases := []struct {
		name string
		text string
		want []string
	}{
		{"a fact id with a digit", "This rests on F-PROMISE-13 and nothing else.", nil},
		{"a fact id with a one-letter part", "This rests on F-RULES and nothing else.", nil},
		{"a one-letter part beside an acronym", "This rests on F-HW and nothing else.", nil},
		{"a story id", "This rests on F-STORY-07 and nothing else.", nil},
		{"a product code", "This rests on BEN-UNG-00-001 and nothing else.", nil},
		{"a one-letter part before a word", "An X-RAY shows it.", nil},
		{"a one-letter part before a shouted word", "The A-TEAM is here.", nil},
		{"ampersand names", "Cancel the unused AT&T lines, then file the R&D notes.", nil},
		{"more ampersand names", "The Q&A with P&G ran long.", nil},
		{"a hyphenated compound", "That is a LOAD-BEARING wall.", []string{"LOAD", "BEARING"}},
		{"another hyphenated compound", "This one is MUST-HAVE for launch.", []string{"MUST", "HAVE"}},
		{"words with no one-letter part", "We WILL-NOT ship it.", []string{"WILL", "NOT"}},
		{"ampersand-joined words", "That was pure ROCK&ROLL tonight.", []string{"ROCK", "ROLL"}},
		{"a compound beside an identifier", "Per F-PROMISE-13 this is LOAD-BEARING.", []string{"LOAD", "BEARING"}},
		{"a space before the ampersand", "Cancel the AT &T lines.", []string{"AT"}},
		{"a two-letter word alone", "Meet me AT the door.", []string{"AT"}},
		{"a bare word", "We PROMISE it works.", []string{"PROMISE"}},
		{"a lowercase part ends the run", "The F-Promise is NOT kept.", []string{"NOT"}},
	}
	for _, testCase := range cases {
		var got []string
		for _, shouted := range ShoutedTokensInMasked(MaskCodeAndCommands(testCase.text)) {
			got = append(got, shouted.Text)
		}
		if !slices.Equal(got, testCase.want) {
			test.Errorf("%s: %q reports %q, want %q", testCase.name, testCase.text, got, testCase.want)
		}
	}
}
