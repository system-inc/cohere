package micromark

// spaceRun is one factorySpace: upstream's closure state, with its states made once per slot.
type spaceRun struct {
	effects     *Effects
	ok          State
	tokenType   string
	limit, size int

	startState, prefixState State
}

// release zeroes the run for its next parse, keeping its bound states.
func (run *spaceRun) release() {
	*run = spaceRun{startState: run.startState, prefixState: run.prefixState}
}

// start is upstream's start. Upstream's size is per call of factorySpace, but a state may be entered
// again once the run it began is done (initializeFlow returns the same one for every line), and there
// size must start over, as it did when each entry made its own run.
func (run *spaceRun) start(code Code) State {
	if !markdownSpace(code) {
		return run.ok(code)
	}
	run.effects.Enter(run.tokenType, nil)
	run.size = 0
	return run.prefix(code)
}

func (run *spaceRun) prefix(code Code) State {
	if markdownSpace(code) {
		// `size++ < limit`: the increment happens whether or not the comparison passes.
		below := run.limit < 0 || run.size < run.limit
		run.size++
		if below {
			run.effects.Consume(code)
			return run.prefixState
		}
	}

	run.effects.Exit(run.tokenType)
	return run.ok(code)
}
