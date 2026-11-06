package tailwind

// classOrderAlwaysAnswers answers every class, including the ones the engine declined.
type classOrderAlwaysAnswers struct{}

func (classOrderAlwaysAnswers) Name() string { return "always answers" }

func (classOrderAlwaysAnswers) Reading(string) (Reading, bool) {
	return Reading{Order: []int{1}, Count: 1}, true
}
