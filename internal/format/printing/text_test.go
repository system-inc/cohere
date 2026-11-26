package printing

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/format/oracle_testing"
)

/*
 * The text utilities against upstream's, across the offset conversion.
 *
 * Upstream's run on UTF-16 indices and these on byte offsets, so the comparison maps every start index
 * from bytes to UTF-16 before asking upstream and maps every answer back. The alphabet is chosen for where
 * the two can disagree: U+2028 and U+2029, which skipNewline treats as newlines and which are three bytes
 * in UTF-8; the non-ASCII whitespace JavaScript's \s matches; a wide character and an astral emoji, which
 * are not whitespace but change where offsets land; and CRLF. The core's toy-language differential is
 * pure ASCII, so it never reached any of this.
 */

var textAlphabet = []string{
	" ",
	"\t",
	"\n",
	"\r",
	"\r\n",
	" ",
	" ",
	" ",
	"　",
	"\ufeff",
	"a",
	"中",
	"😀",
	"/",
	"*",
	",",
	";",
	"//",
	"/*",
	"*/",
}

// byteToUnit and unitToByte convert between offsets at rune boundaries. -1, -2 and len map to themselves.
func offsetMaps(text string) (byteToUnit map[int]int, unitToByte map[int]int) {
	byteToUnit, unitToByte = map[int]int{}, map[int]int{}
	unit := 0
	for offset, character := range text {
		byteToUnit[offset], unitToByte[unit] = unit, offset
		unit += len(utf16.Encode([]rune{character}))
	}
	byteToUnit[len(text)], unitToByte[unit] = unit, len(text)
	for _, special := range []int{-1, -2} {
		byteToUnit[special], unitToByte[special] = special, special
	}
	return byteToUnit, unitToByte
}

type textAnswers struct {
	SkipWhitespaceForward, SkipWhitespaceBackward int
	SkipSpacesForward, SkipSpacesBackward         int
	SkipToLineEnd, SkipEverythingButNewLine       int
	SkipNewlineForward, SkipNewlineBackward       int
	HasNewlineForward, HasNewlineBackward         bool
	IsPreviousLineEmpty                           bool
	SkipInlineComment, SkipTrailingComment        int
}

// textUpstreamScript asks upstream's text utilities, the ones the standalone bundle exports as prettier.util
// from the fork's src/utilities, about __input's text at its UTF-16 start. false answers as -2, as the Go side
// spells it.
const textUpstreamScript = `(() => {
  const { text, start } = JSON.parse(__input);
  const u = prettier.util;
  const f = (value) => value === false ? -2 : value;
  const back = { backwards: true };
  return JSON.stringify({
    SkipWhitespaceForward: f(u.skipWhitespace(text, start)), SkipWhitespaceBackward: f(u.skipWhitespace(text, start, back)),
    SkipSpacesForward: f(u.skipSpaces(text, start)), SkipSpacesBackward: f(u.skipSpaces(text, start, back)),
    SkipToLineEnd: f(u.skipToLineEnd(text, start)), SkipEverythingButNewLine: f(u.skipEverythingButNewLine(text, start)),
    SkipNewlineForward: f(u.skipNewline(text, start)), SkipNewlineBackward: f(u.skipNewline(text, start, back)),
    HasNewlineForward: u.hasNewline(text, start), HasNewlineBackward: u.hasNewline(text, start, back),
    IsPreviousLineEmpty: u.isPreviousLineEmpty(text, start),
    SkipInlineComment: f(u.skipInlineComment(text, start)), SkipTrailingComment: f(u.skipTrailingComment(text, start)),
  });
})()`

// textCaseCount is how many generated cases TestTextAgreesWithUpstream holds, the first of seed 20261003.
// The live differential ran 20,000; this slice keeps the golden small.
const textCaseCount = 700

// TestTextAgreesWithUpstream is the differential, against upstream's answers recorded (see oracle_testing), so
// no JavaScript runs here.
func TestTextAgreesWithUpstream(test *testing.T) {
	test.Parallel()
	random := rand.New(rand.NewSource(20261003))
	type pending struct {
		text      string
		byteStart int
		unitStart int
	}
	var originals []pending
	for len(originals) < textCaseCount {
		var builder strings.Builder
		for count := 1 + random.Intn(10); count > 0; count-- {
			builder.WriteString(textAlphabet[random.Intn(len(textAlphabet))])
		}
		text := builder.String()
		byteToUnit, _ := offsetMaps(text)
		// Starts at every rune boundary and both ends, since edges are where off-by-one lives.
		var starts []int
		for offset := range text {
			starts = append(starts, offset)
		}
		starts = append(starts, len(text), -1)
		start := starts[random.Intn(len(starts))]
		originals = append(originals, pending{text: text, byteStart: start, unitStart: byteToUnit[start]})
	}

	oracle := newBundleOracle(test)
	mismatches, unmapped, nonAscii := 0, 0, 0
	for index, original := range originals {
		text, start := original.text, original.byteStart
		if !isAsciiOnly(text) {
			nonAscii++
		}
		_, unitToByte := offsetMaps(text)
		input, marshalError := json.Marshal(struct {
			Text  string `json:"text"`
			Start int    `json:"start"`
		}{text, original.unitStart})
		if marshalError != nil {
			test.Fatalf("encoding case %d: %v", index, marshalError)
		}
		answer, upstreamError := oracle.answer(
			oracle_testing.Key("textUtilities", string(input)),
			textUpstreamScript,
			string(input),
		)
		if upstreamError != nil {
			test.Fatalf("the oracle failed on %q at %d: %v", text, start, upstreamError)
		}
		var want textAnswers
		if decodeError := json.Unmarshal([]byte(answer), &want); decodeError != nil {
			test.Fatalf("the oracle answered %q for %q at %d: %v", answer, text, start, decodeError)
		}
		mapped := func(unit int) int {
			if offset, present := unitToByte[unit]; present {
				return offset
			}
			// Upstream stepped one UTF-16 unit back into an astral character and stopped on its low
			// surrogate, a position with no byte offset. The byte version stops at that character's first
			// byte. Every caller only tests which character sits at the position, and neither a lone
			// surrogate nor an astral rune's first byte equals any character these functions look for, so
			// the two are equivalent. That is a claim, so the comparison holds it exactly: Go must answer
			// the start of the very rune upstream's unit sits inside, and these are counted separately.
			if offset, present := unitToByte[unit-1]; present {
				unmapped++
				return offset
			}
			return -100
		}
		got := textAnswers{
			SkipWhitespaceForward:    SkipWhitespace(text, start, false),
			SkipWhitespaceBackward:   SkipWhitespace(text, start, true),
			SkipSpacesForward:        SkipSpaces(text, start, false),
			SkipSpacesBackward:       SkipSpaces(text, start, true),
			SkipToLineEnd:            SkipToLineEnd(text, start, false),
			SkipEverythingButNewLine: SkipEverythingButNewLine(text, start, false),
			SkipNewlineForward:       SkipNewline(text, start, false),
			SkipNewlineBackward:      SkipNewline(text, start, true),
			HasNewlineForward:        HasNewline(text, start, false),
			HasNewlineBackward:       HasNewline(text, start, true),
			IsPreviousLineEmpty:      IsPreviousLineEmpty(text, start),
			SkipInlineComment:        SkipInlineComment(text, start),
			SkipTrailingComment:      SkipTrailingComment(text, start),
		}
		expected := textAnswers{
			SkipWhitespaceForward:    mapped(want.SkipWhitespaceForward),
			SkipWhitespaceBackward:   mapped(want.SkipWhitespaceBackward),
			SkipSpacesForward:        mapped(want.SkipSpacesForward),
			SkipSpacesBackward:       mapped(want.SkipSpacesBackward),
			SkipToLineEnd:            mapped(want.SkipToLineEnd),
			SkipEverythingButNewLine: mapped(want.SkipEverythingButNewLine),
			SkipNewlineForward:       mapped(want.SkipNewlineForward),
			SkipNewlineBackward:      mapped(want.SkipNewlineBackward),
			HasNewlineForward:        want.HasNewlineForward,
			HasNewlineBackward:       want.HasNewlineBackward,
			IsPreviousLineEmpty:      want.IsPreviousLineEmpty,
			SkipInlineComment:        mapped(want.SkipInlineComment),
			SkipTrailingComment:      mapped(want.SkipTrailingComment),
		}
		if got != expected {
			mismatches++
			if mismatches <= 5 {
				test.Errorf("text %q start %d\nwant %+v\ngot  %+v", text, start, expected, got)
			}
		}
	}
	test.Logf(
		"%d cases, %d with non-ASCII, %d answers upstream lands mid surrogate pair (held to the containing rune's start), %d mismatches",
		len(originals),
		nonAscii,
		unmapped,
		mismatches,
	)
	if nonAscii < len(originals)/2 {
		test.Errorf(
			"only %d of %d cases contain non-ASCII text, which is the population this test exists for",
			nonAscii,
			len(originals),
		)
	}
}

func isAsciiOnly(text string) bool {
	for index := 0; index < len(text); index++ {
		if text[index] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}
