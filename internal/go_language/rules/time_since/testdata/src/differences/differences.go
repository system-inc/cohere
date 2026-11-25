package differences

import clock "time"

type clocks struct {
	Now func() clock.Time
}

// The file imports time as clock alone, so the original's time.Since names nothing, and the port writes clock.Since.
func renamed(t clock.Time) clock.Duration {
	// The callee is time.Now by the name clock, so the fix is clock.Since.
	_ = clock.Now().Sub(t) // port moves: should use time.Since instead of time.Now().Sub
	// Parentheses around the callee leave its qualifier clock.
	return (clock.Now)().Sub(t) // port moves: should use time.Since instead of time.Now().Sub
}

// A local named time is a time.Time, which has no Since, and the fix's call of clock.Since takes it as its argument.
func shadowed(t clock.Time) {
	time := clock.Now()
	// The original writes time.Since(time), selecting from the local.
	_ = clock.Now().Sub(time) // port moves: should use time.Since instead of time.Now().Sub
	_ = time.Sub(t)
}

// A field named Now holding time.Now is a variable, not the function, even where a local named time selects it.
func field(t clock.Time) {
	time := clocks{Now: clock.Now}
	_ = time.Now().Sub(t)
}

// An argument across lines, with comments among its parts and around it, is printed by the fix from its own
// positions, the comments left out, as format prints a node with no comments of its own.
func commented(times []clock.Time, index int) {
	// The argument across lines moves to clock.Since as the one-line calls do.
	_ = clock.Now().Sub( // port moves: should use time.Since instead of time.Now().Sub
		// the earlier time
		times[ /* the index */ index], // and nothing else
	)
	// The argument's parentheses and the comment inside them go, as the original's binding drops them.
	_ = clock.Now().Sub(( /* inside */ times[0])) // port moves: should use time.Since instead of time.Now().Sub
}

// An argument of a conversion and of a composite literal, each a time.Time, as Since takes.
func converted(t clock.Time) {
	type named clock.Time
	// A conversion to time.Time by the name clock.
	_ = clock.Now().Sub(clock.Time(named(t))) // port moves: should use time.Since instead of time.Now().Sub
	// A field promoted from a composite literal's embedded time.Time.
	_ = clock.Now().Sub(struct{ clock.Time }{t}.Time) // port moves: should use time.Since instead of time.Now().Sub
}
