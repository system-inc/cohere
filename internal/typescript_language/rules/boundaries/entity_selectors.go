package boundaries

// entitySelectors is upstream's selector-or-array-of-selectors for `from` and `to`.
type entitySelectors []entitySelector

func (selectors *entitySelectors) UnmarshalJSON(raw []byte) error {
	return decodeOneOrMany(
		raw,
		(*[]entitySelector)(selectors),
		"an element selector such as {\"element\": {\"type\": \"api\"}}",
	)
}
