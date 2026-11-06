package generated

// tally is what the generator counts.
type tally struct {
	Result int
}

// result is declared by hand, so the generated file's stutter on it is a finding the engine withholds rather than
// one the rule leaves alone, as it leaves a variable the generated file declares.
var result = tally{Result: 1}
