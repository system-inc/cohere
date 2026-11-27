package doc

import (
	"strconv"
	"testing"

	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

// widthCases are the shapes most likely to separate a port from upstream, each for a stated reason.
var widthCases = []string{
	"",
	"plain ascii",
	"tab\there",    // a control character takes the slow path and counts zero
	"中文字符",         // wide
	"ｆｕｌｌｗｉｄｔｈ",    // fullwidth forms
	"é vs é",      // a combining mark counts zero
	"smile 😀 done", // astral emoji, a surrogate pair in UTF-16
	"flag 🇺🇸",      // regional indicator pair
	// Not here: a ZWJ family, "family 👨‍👩‍👧", which node's getStringWidth measures as one
	// emoji (9) and the bundle under goja as three (12), since goja's regexp misses the sequence. StringWidth
	// answers 9, as node does, so the bundle's answer is not upstream's here and is not recorded.
	"keycap 1️⃣ and #️⃣",          // keycap sequences, the first branch of emoji-regex
	"heart ❤ vs ❤️",               // text presentation against emoji presentation
	"copyright © and ™",           // narrow emoji candidates
	"ambiguous ± §",               // ambiguous width counts 1
	"skin 👍🏽",                     // a modifier sequence
	"lone \xed\xa0\x80 surrogate", // invalid UTF-8, which both sides decode to U+FFFD
	"ｱｲｳ halfwidth katakana",      // halfwidth counts 1
	"\u00a0nbsp",
	"️ stray selector",
	"한국어 hangul",  // Hangul syllables are wide
	"𠮷野家",         // an astral CJK ideograph, a surrogate pair that is wide
	"ｈｅｌｌｏ　ｗｏｒｌｄ", // fullwidth with an ideographic space
	"中文😀é\t中",     // wide, emoji, a combining mark and a tab in one line
}

// TestStringWidthAgreesWithUpstream measures the port against the fork's own getStringWidth. Its answers are
// what the fork's standalone bundle, run in-process, gave as prettier.util.getStringWidth, recorded (see
// oracle_testing). A case where that answer differs from node's is left out (see widthCases).
func TestStringWidthAgreesWithUpstream(test *testing.T) {
	test.Parallel()
	golden := oracle_testing.Open(test, test.Name())
	wide := 0
	for _, input := range widthCases {
		answer, answerError := golden.Answer(oracle_testing.Key("getStringWidth", input))
		if answerError != nil {
			test.Fatalf("the oracle failed on %q: %v", input, answerError)
		}
		expected, atoiError := strconv.Atoi(answer)
		if atoiError != nil {
			test.Fatalf("the oracle answered %q for %q, not a width", answer, input)
		}
		if got := StringWidth(input); got != expected {
			test.Errorf("StringWidth(%q) = %d, upstream %d", input, got, expected)
		}
		if expected > len([]rune(input)) {
			wide++
		}
	}
	// Some case must measure wider than it has characters, or a rune count would pass.
	if wide == 0 {
		test.Error("no case measured wider than its rune count, so wide characters went untested")
	}
}

// TestWidthControlCanFail checks the widths that separate a port from a byte or rune count: a wide character,
// an astral emoji and a combining mark.
func TestWidthControlCanFail(test *testing.T) {
	test.Parallel()
	if StringWidth("中") != 2 || StringWidth("a") != 1 || StringWidth("😀") != 2 || StringWidth("é") != 1 {
		test.Fatalf("basic widths wrong: 中=%d a=%d 😀=%d e+combining=%d",
			StringWidth("中"), StringWidth("a"), StringWidth("😀"), StringWidth("é"))
	}
}
