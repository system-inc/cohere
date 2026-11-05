package regexsyntax

// Hex digits, which the class parser reaches for in three places: `\xNN`, `\uNNNN`, and the
// brace form `\u{NNNNN}`. They are here rather than in a shared string package because the class
// parser is their only caller, and a helper with one caller in the package that uses it is easier
// to reason about than the same helper one import away.

// IsHexDigit reports whether `character` is a hexadecimal digit in either case.
func IsHexDigit(character byte) bool {
	return (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')
}

// AllHexDigits reports whether `text` is non-empty and every byte in it is a hex digit.
//
// Empty is false rather than vacuously true on purpose: the callers are asking whether an escape
// has digits after it, and `\x` with nothing following is not a valid escape.
func AllHexDigits(text string) bool {
	if text == "" {
		return false
	}
	for index := range len(text) {
		if !IsHexDigit(text[index]) {
			return false
		}
	}
	return true
}

// ParseHexUint reads `text` as a hexadecimal number.
//
// A non-hex byte contributes zero rather than failing, because every caller checks AllHexDigits
// first. The pairing is the contract: ask whether it is hex, then read it.
func ParseHexUint(text string) uint32 {
	value := uint32(0)
	for index := range len(text) {
		value = (value << 4) | hexValue(text[index])
	}
	return value
}

func hexValue(character byte) uint32 {
	switch {
	case character >= '0' && character <= '9':
		return uint32(character - '0')
	case character >= 'a' && character <= 'f':
		return uint32(character-'a') + 10
	case character >= 'A' && character <= 'F':
		return uint32(character-'A') + 10
	}
	return 0
}
