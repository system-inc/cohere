package boundaries

// dependencySelectors is upstream's selector-or-array-of-selectors for `allow` and `disallow`.
type dependencySelectors []dependencySelector

func (selectors *dependencySelectors) UnmarshalJSON(raw []byte) error {
	return decodeOneOrMany(
		raw,
		(*[]dependencySelector)(selectors),
		"a dependency selector such as {\"to\": {\"element\": {\"type\": \"api\"}}}",
	)
}
