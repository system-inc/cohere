package tailwind

// repositorySuggestionTracking is upstream's per-function storage: what one of `--value` and
// `--modifier` was seen naming.
type repositorySuggestionTracking struct {
	usedSpacingInteger bool
	usedSpacingNumber  bool
	namespaces         []string
	literals           []string
}

// list is the tracking as a recipe. The spacing scale goes in as literals, the whole scale for a
// `number` argument and its integers for an `integer` one, since unlike the framework's it does not
// depend on the theme.
func (tracking *repositorySuggestionTracking) list() SuggestionList {
	list := SuggestionList{Literals: append([]string(nil), tracking.literals...), Namespaces: tracking.namespaces}
	if tracking.usedSpacingNumber {
		list.Literals = append(list.Literals, DefaultSpacingSuggestions...)
	} else if tracking.usedSpacingInteger {
		for _, value := range DefaultSpacingSuggestions {
			if isPositiveInteger(value) {
				list.Literals = append(list.Literals, value)
			}
		}
	}
	return list
}
