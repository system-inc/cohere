package micromark

// blankLineRun is one blank line: upstream's closure state, with its states made once per slot.
type blankLineRun struct {
	effects *Effects
	ok, nok State

	startState, afterState State
}

// release zeroes the run for its next parse, keeping its bound states.
func (run *blankLineRun) release() {
	*run = blankLineRun{startState: run.startState, afterState: run.afterState}
}

func (run *blankLineRun) start(code Code) State {
	if markdownSpace(code) {
		return factorySpace(run.effects, run.afterState, TypeLinePrefix, 0)(code)
	}
	return run.after(code)
}

func (run *blankLineRun) after(code Code) State {
	if code == CodeEof || markdownLineEnding(code) {
		return run.ok(code)
	}
	return run.nok(code)
}
