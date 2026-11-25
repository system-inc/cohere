package flagged

// A test file is judged like any other, so two helpers with methods are two types.
type recorder struct {
	calls []string
}

func (recorder *recorder) record(call string) {
	recorder.calls = append(recorder.calls, call)
}

type clock struct { // want "already serves `recorder`, and `clock` is a second type with methods"
	ticks int
}

func (clock *clock) tick() {
	clock.ticks++
}
