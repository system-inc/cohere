package main

import "fmt"

// readinessSummary is a run's readiness, as the footer and --json say it.
type readinessSummary struct {
	// Measured is false when the run could not say, and Reason then says why, in a clause.
	Measured bool   `json:"measured"`
	Reason   string `json:"reason,omitempty"`

	// Files is every file the walk measured or tried to, Ready the ones that pass, and Unmeasured the ones a
	// set rule skipped or the cache replayed without a count.
	Files      int `json:"files"`
	Ready      int `json:"ready"`
	Unmeasured int `json:"unmeasured"`

	// OptionsMissing is the Adamic compiler options the project's tsconfig leaves off.
	OptionsMissing []string `json:"optionsMissing"`

	// FailingFilesByRule counts, by set rule, the files it found something in.
	FailingFilesByRule map[string]int `json:"failingFilesByRule"`
}

// segment is the footer's readiness segment, empty for a run that took none.
func (summary *readinessSummary) segment() string {
	if summary == nil {
		return ""
	}
	if !summary.Measured {
		return "Adamic readiness not measured: " + summary.Reason
	}
	if summary.Files == 0 {
		return "Adamic readiness not measured: no file was linted"
	}
	text := fmt.Sprintf(
		"%d%% Adamic-ready (%s of %s",
		summary.Ready*100/summary.Files,
		grouped(summary.Ready),
		grouped(summary.Files),
	)
	if summary.Unmeasured > 0 {
		text += "; " + grouped(summary.Unmeasured) + " unmeasured"
	}
	if len(summary.OptionsMissing) > 0 {
		text += "; tsconfig lacks " + joinedOptions(summary.OptionsMissing)
	}
	return text + ")"
}
