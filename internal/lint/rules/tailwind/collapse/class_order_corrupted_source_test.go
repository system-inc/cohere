package tailwind

// classOrderCorruptedSource wraps an honest source and bends its answers.
type classOrderCorruptedSource struct {
	honest    ReadingSource
	transform func(reading Reading, answered bool) (Reading, bool)
}

func (source classOrderCorruptedSource) Name() string { return source.honest.Name() + " (corrupted)" }

func (source classOrderCorruptedSource) Reading(className string) (Reading, bool) {
	return source.transform(source.honest.Reading(className))
}
