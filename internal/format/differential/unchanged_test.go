package differential

// unchanged returns its input. It is the candidate that flatters an overall score.
type unchanged struct{}

func (unchanged) Handles(string) bool { return true }

func (unchanged) Format(_ string, text string) (string, error) { return text, nil }
