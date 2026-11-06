package boundaries

// elementSelector matches an element by its type.
type elementSelector struct {
	Type  string      `json:"type"`
	Types *typesQuery `json:"types"`
}

// matches reports whether one element satisfies an element selector.
func (selector *elementSelector) matches(subject element) bool {
	if selector.Type != "" && selector.Type != subject.Type {
		return false
	}
	if selector.Types != nil {
		found := false
		for _, name := range selector.Types.AnyOf {
			if name == subject.Type {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
