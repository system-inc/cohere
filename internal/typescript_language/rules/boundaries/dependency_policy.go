package boundaries

// dependencyPolicy is one entry of `policies`: an outer from and to, narrowed by its allow or disallow.
type dependencyPolicy struct {
	From     entitySelectors     `json:"from"`
	To       entitySelectors     `json:"to"`
	Allow    dependencySelectors `json:"allow"`
	Disallow dependencySelectors `json:"disallow"`
	Message  string              `json:"message"`
}

// entryMatches reports whether one allow or disallow entry, composed with its policy, matches.
func (policy *dependencyPolicy) entryMatches(entry dependencySelector, from element, to element) bool {
	return selectorsMatch(mergeEntitySelectors(policy.From, entry.From), from) &&
		selectorsMatch(mergeEntitySelectors(policy.To, entry.To), to)
}
