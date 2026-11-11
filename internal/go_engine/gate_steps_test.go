package go_engine

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/project"
)

// gateSteps is what a test sees of the extra builds' lints at the gate (lintSteps): each build's weight, the builds
// holding weight now, each time a lint took weight beside another while either was heavy, which the gate exists to
// prevent, the order the builds took their weight in, the builds that waited in line, and how many gave weight back.
type gateSteps struct {
	lock    sync.Mutex
	weights map[string]int
	holding map[string]bool
	beside  []string
	taken   []string
	queued  map[string]bool
	given   int
}

// newGateSteps is a watch that has seen no step.
func newGateSteps() *gateSteps {
	return &gateSteps{weights: map[string]int{}, holding: map[string]bool{}, queued: map[string]bool{}}
}

// step records extra's step at the gate with its weight.
func (steps *gateSteps) step(extra project.Build, step lintStep, weight int) {
	steps.lock.Lock()
	defer steps.lock.Unlock()
	name := extra.String()
	steps.weights[name] = weight
	switch step {
	case lintStepReady:
	case lintStepQueued:
		steps.queued[name] = true
	case lintStepTaken:
		for other := range steps.holding {
			if weight >= lintBudget || steps.weights[other] >= lintBudget {
				steps.beside = append(steps.beside, name+" beside "+other)
			}
		}
		steps.holding[name] = true
		steps.taken = append(steps.taken, name)
	case lintStepGiving:
		delete(steps.holding, name)
		steps.given++
	}
}
