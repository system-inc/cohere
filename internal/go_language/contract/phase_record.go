package contract

// PhaseRecord is one phase's outcome. Every run writes exactly one per phase, in pipeline order,
// including the phases that did not run, because a phase absent from the report reads the same as one
// the reporter forgot.
type PhaseRecord struct {
	Name                PhaseName `json:"name"`
	Outcome             Outcome   `json:"outcome"`
	ElapsedMilliseconds int       `json:"elapsedMilliseconds"`
	Detail              string    `json:"detail"`
}

func (PhaseRecord) kind() Kind { return KindPhase }
