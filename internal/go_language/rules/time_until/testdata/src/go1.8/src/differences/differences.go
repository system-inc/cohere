package differences

import clock "time"

type clocks struct {
	Now func() clock.Time
}

// The file imports time as clock alone, so the original's time.Until names nothing, and the port names it clock, as
// the call names time.Now.
func renamed(t clock.Time, pointer *clock.Time) {
	// The original writes time.Until(t), and the port clock.Until(t).
	_ = t.Sub(clock.Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
	// The original writes time.Until(pointer), wrong twice, and the port clock.Until(*pointer).
	_ = pointer.Sub(clock.Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
}

// A local named time holds a time, and the original's time.Until selects from it.
func shadowed() {
	time := clock.Now()
	// The original writes time.Until(time), and the port clock.Until(time).
	_ = time.Sub(clock.Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
}

// A field named Now holding time.Now is a variable, not the function, even where a local named time selects it.
func field(t clock.Time) {
	time := clocks{Now: clock.Now}
	_ = t.Sub(time.Now())
}

// A receiver across lines, with comments among its parts, is printed by the fix from its own positions.
func commented(times []clock.Time, index int) {
	// The original writes time.Until(times[index]), and the port clock.Until(times[index]).
	_ = times[ /* the index */ index].Sub( // port moves: should use time.Until instead of t.Sub(time.Now())
		// the time now
		clock.Now(), // and nothing else
	)
	// The original writes time.Until around the function literal, and the port clock.Until.
	_ = func() clock.Time { // port moves: should use time.Until instead of t.Sub(time.Now())
		// the first
		return times[0]
	}().Sub(clock.Now())
}

// A receiver of a conversion and of a composite literal.
func converted(t clock.Time) {
	type named clock.Time
	// The original writes time.Until around the conversion, and the port clock.Until.
	_ = clock.Time(named(t)).Sub(clock.Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
	// The original writes time.Until(struct{ clock.Time }{t}), wrong twice, and the port selects the field.
	_ = struct{ clock.Time }{t}.Sub(clock.Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
}
