package regexp

import (
	"strings"
)

// A JavaScript regexp carrying the `i` flag compares two characters by
// Canonicalize, which is neither Go's case folding nor the Unicode
// folding regexp2 reaches for when told to ignore case. It is also not one
// reading but two: `u` and `v` select simple case folding, and their absence
// selects an uppercase mapping that never crosses into ASCII. U+212A KELVIN
// SIGN against `k` comes out differently under each, so every entry point
// here carries the flag through rather than picking one.
//
// So rather than ask regexp2 to ignore case, a pattern is widened: every
// literal and every character class is rewritten to name each character it
// should compare equal to, and the widened pattern is matched exactly.

// CaseClass widens one literal character to the class of characters a `/i`
// comparison accepts for it, reporting false when the character stands alone
// and needs no widening.
func CaseClass(character rune, unicode bool) (string, bool) {
	members := CaseEquivalents(character, unicode)
	if len(members) == 0 {
		return "", false
	}
	var class strings.Builder
	class.WriteByte('[')
	for _, member := range members {
		class.WriteString(EscapeClassRune(member))
	}
	class.WriteByte(']')
	return class.String(), true
}

// CaseCloseClass widens a character class to cover every character a `/i`
// comparison accepts for one it already covers. It takes the body of a class —
// what sits between the brackets, leading `^` included — and returns the body
// to write instead.
//
// What the class already names is left alone and the rest is appended, so a
// negated class goes on negating whatever the widened class covers. That is
// how JavaScript reads one too: `[^a]` asks whether any member compares equal
// to the character at hand, and answers no to `A`.
func CaseCloseClass(body string, unicode bool) string {
	options := rewriteOptions{ignoreCase: true, unicode: unicode}
	atoms, _, classAtomsError := classAtoms(body, options, escapeContext{unicode: unicode})
	if classAtomsError != nil {
		return body
	}
	extras := caseExtras(atoms, unicode)
	if extras == "" {
		return body
	}
	return escapeTrailingDash(body) + extras
}

// EscapeClassRune escapes a character that would carry a meaning of its own
// inside a character class.
func EscapeClassRune(character rune) string {
	if strings.ContainsRune(`\]^-[`, character) {
		return `\` + string(character)
	}
	return string(character)
}

// escapeTrailingDash escapes the `-` that stands for itself at the end of a
// character class, so that appending to the class does not read it as opening
// a range instead.
func escapeTrailingDash(body string) string {
	if !strings.HasSuffix(body, "-") {
		return body
	}
	backslashes := 0
	for index := len(body) - 2; index >= 0 && body[index] == '\\'; index-- {
		backslashes++
	}
	if backslashes%2 == 1 {
		return body
	}
	return body[:len(body)-1] + `\-`
}
