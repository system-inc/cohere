package cst

// JavaScript's string methods over UTF-16 code units, with their clamping rules, so the lexer can call
// them where upstream does without each call site re-deriving what happens out of range.

// characterAt is `text[index]`: the code unit, or -1 for undefined past either end.
func characterAt(text []uint16, index int) int {
	if index < 0 || index >= len(text) {
		return -1
	}
	return int(text[index])
}

// indexOf is `text.indexOf(character, from)`. A from past the end finds nothing; a negative from is 0.
func indexOf(text []uint16, character uint16, from int) int {
	for index := max(from, 0); index < len(text); index++ {
		if text[index] == character {
			return index
		}
	}
	return -1
}

// substring is `text.substring(start, end)`: both clamped to [0, length], swapped when start > end.
func substring(text []uint16, start int, end int) []uint16 {
	clampedStart := clamp(start, 0, len(text))
	clampedEnd := clamp(end, 0, len(text))
	if clampedStart > clampedEnd {
		clampedStart, clampedEnd = clampedEnd, clampedStart
	}
	return text[clampedStart:clampedEnd:clampedEnd]
}

// substr is `text.substr(start, length)`: a negative start counts from the end, the length is clamped.
func substr(text []uint16, start int, length int) []uint16 {
	resolvedStart := start
	if resolvedStart < 0 {
		resolvedStart = max(len(text)+resolvedStart, 0)
	}
	resolvedStart = min(resolvedStart, len(text))
	end := clamp(resolvedStart+length, resolvedStart, len(text))
	return text[resolvedStart:end:end]
}

// slice is `text.slice(start, end)`: negatives count from the end, an end before the start is empty.
func slice(text []uint16, start int, end int) []uint16 {
	resolvedStart := start
	if resolvedStart < 0 {
		resolvedStart = max(len(text)+resolvedStart, 0)
	}
	resolvedEnd := end
	if resolvedEnd < 0 {
		resolvedEnd = max(len(text)+resolvedEnd, 0)
	}
	resolvedStart = min(resolvedStart, len(text))
	resolvedEnd = min(resolvedEnd, len(text))
	if resolvedEnd < resolvedStart {
		return text[resolvedStart:resolvedStart:resolvedStart]
	}
	return text[resolvedStart:resolvedEnd:resolvedEnd]
}

// startsWith is `text.startsWith(prefix)` for an ASCII prefix.
func startsWith(text []uint16, prefix string) bool {
	if len(text) < len(prefix) {
		return false
	}
	for index := 0; index < len(prefix); index++ {
		if text[index] != uint16(prefix[index]) {
			return false
		}
	}
	return true
}

// equals is `text === literal` for an ASCII literal.
func equals(text []uint16, literal string) bool {
	return len(text) == len(literal) && startsWith(text, literal)
}

func clamp(value int, low int, high int) int {
	return min(max(value, low), high)
}
