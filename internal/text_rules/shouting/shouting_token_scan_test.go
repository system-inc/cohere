package shouting

import (
	"slices"
	"strings"
	"testing"
)

// tokenScanCases are the shapes a hand scan of uppercaseToken could get wrong: a word glued to an underscore, a digit,
// a lowercase letter, a multi-byte rune or a byte of invalid `UTF-8`, on either side, and every mask's delimiters
// pressed against a word, so the masking test below sees each mask open and close beside one.
var tokenScanCases = []string{
	"", "A", "AB", "a", "_", "NEVER", "NEVER do", "do NEVER", "the API is NEVER down",
	"_ABC", "ABC_", "A_B", "ABc", "aBC", "1ABC", "ABC1", "A1B2", "MAX_RETRY_COUNT", "__ABC__",
	"éABC", "ABCé", "café NEVER crème", "\xffABC\xff", "AB\x80CD", "ABC.DEF", "ABC-DEF", "ABC'S", "BOSS'S ORDERS'",
	"ABC`x`DEF", "`NEVER`ABC", "ABC\"x\"DEF", "\"NEVER\"ABC", "ABC'x'DEF", "'NOT NULL'ABC", "a 'RENAME COLUMN'd table",
	"ABC```x```DEF", "```\nNEVER\n```ABC", "the \"quiet\nphrase\" is NEVER loud", "a 5\" board\nis NEVER\"xAB",
	"prose\n\n    HOME=/x NEVER\nThen ALWAYS", "@example\nNEVER\n@param ALWAYS",
	" * @example\n * LOUD()\n * @returns X",
	"git commit NEVER\nthe NEVER", "$ ls ALWAYS\nNEVER", "s  c ALWAYS\nLOUD", "said\ngit and go are NEVER quiet",
}

// The hand scan finds exactly what uppercaseToken finds, every match where the pattern puts it, over the shapes above,
// and every string of up to four bytes from an alphabet of each kind of byte the boundary tells apart.
func TestNextUppercaseTokenMatchesThePattern(test *testing.T) {
	test.Parallel()
	texts := slices.Clone(tokenScanCases)
	alphabet := []string{"A", "Z", "a", "1", "_", " ", "'", "\xc3\xa9", "\xff"}
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
	for _, text := range texts {
		want := uppercaseToken.FindAllStringIndex(text, -1)
		var got [][]int
		for start, end := nextUppercaseToken(text, 0); start >= 0; start, end = nextUppercaseToken(text, end) {
			got = append(got, []int{start, end})
		}
		if !slices.EqualFunc(got, want, slices.Equal) {
			test.Fatalf("over %q the scan finds %v, and uppercaseToken finds %v", text, got, want)
		}
	}
	test.Logf("the scan matches uppercaseToken over %d texts", len(texts))
}

// Every token the masked text holds is a token of the text before masking, the same bytes at the same offset, which is
// what lets HasShoutedToken be asked before masking. A mask that blanked inside a word would leave a piece of it that
// was never a token on its own, `NOT` out of `NOTx`, and fail here, as would one that blanked part of a joined run and
// left a token the whole run held back, `RAY` out of `X-RAY`.
func TestMaskingMakesNoNewToken(test *testing.T) {
	test.Parallel()
	texts := slices.Concat(tokenScanCases, runScanCases)
	texts = append(texts, strings.Join(tokenScanCases, "\n"), strings.Join(runScanCases, "\n"))
	for _, text := range texts {
		before := map[[2]int]bool{}
		for start, end := range uppercaseTokens(text) {
			before[[2]int{start, end}] = true
		}
		masked := MaskCodeAndCommands(text)
		for start, end := range uppercaseTokens(masked) {
			if !before[[2]int{start, end}] {
				test.Fatalf("masking %q made the token %q at %d, which the text did not hold", text, masked[start:end],
					start)
			}
		}
	}
}

// HasShoutedToken says no only where masking and the token scan would find nothing, and yes on a shout behind
// acronyms, so a gate that read only the first capital word, or the masked text's words, fails here.
func TestHasShoutedTokenSkipsOnlyWhatReportsNothing(test *testing.T) {
	test.Parallel()
	texts := slices.Concat(tokenScanCases, runScanCases)
	skipped := 0
	for _, text := range texts {
		if HasShoutedToken(text) {
			continue
		}
		skipped++
		if shouted := ShoutedTokensInMasked(MaskCodeAndCommands(text)); len(shouted) > 0 {
			test.Fatalf("HasShoutedToken skips %q, which reports %v", text, shouted)
		}
	}
	if !HasShoutedToken("the API and the HTTP client, the JSON and the SQL, and a retry is NEVER safe") {
		test.Error("HasShoutedToken stops at the acronyms before the shout")
	}
	if HasShoutedToken("the API and the HTTP client, the JSON and the SQL, MAX_RETRY_COUNT and C1") {
		test.Error("HasShoutedToken admits a text of acronyms and identifiers, so it skips no masking")
	}
	test.Logf("HasShoutedToken skips %d of %d texts", skipped, len(texts))
}
