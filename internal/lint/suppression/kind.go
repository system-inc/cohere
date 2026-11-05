package suppression

// Kind is the scope a directive covers.
type Kind int

// String names a kind for a failure message, so a fixture that fails says which form it got.
func (kind Kind) String() string {
	switch kind {
	case KindNextLine:
		return "next-line"
	case KindSameLine:
		return "same-line"
	case KindFile:
		return "file"
	}
	return "unknown"
}
