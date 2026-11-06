package micromark

// blankLine is micromark-core-commonmark/lib/blank-line.js.
var blankLine = &Construct{Partial: true, Tokenize: tokenizeBlankLine}

// tokenizeBlankLine is tried at the start of every line of flow, so its state is a blankLineRun from the
// parse's Memory, whose states were bound when the slot was made, and a try allocates nothing (#vbjv3d6).
func tokenizeBlankLine(self *Self, effects *Effects, ok State, nok State) State {
	run := effects.memory().blankLine()
	run.effects, run.ok, run.nok = effects, ok, nok
	return run.startState
}

func newBlankLineRun() *blankLineRun {
	run := &blankLineRun{}
	run.startState, run.afterState = run.start, run.after
	return run
}
