package tailwind

// ClassListEntry is one class the class list holds, with the modifiers it is offered with.
type ClassListEntry struct {
	// Name is the class, without a prefix.
	Name string
	// Modifiers are the modifiers offered with it, deduplicated, in the order first offered.
	Modifiers []string
	// sources are what offered the class: a static, or a functional root and value, each recorded once.
	// Usually one; a static and a functional suggestion of the same name are two.
	sources []classListSource
}

// addSource records a source once.
func (entry *ClassListEntry) addSource(source classListSource) {
	for _, existing := range entry.sources {
		if existing == source {
			return
		}
	}
	entry.sources = append(entry.sources, source)
}
