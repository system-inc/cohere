package parity

import "time"

type subtracter struct{}

func (subtracter) Sub(time.Time) time.Duration { return 0 }

type subtracting interface{ Sub(time.Time) time.Duration }

// Now is this package's own, spelled as time's is.
func Now() time.Time { return time.Time{} }

var now = time.Now

func reported(t time.Time, pointer *time.Time, times []time.Time) {
	_ = t.Sub(time.Now())
	t.Sub(time.Now())
	_ = time.Now().Sub(time.Now())
	_ = t.Add(time.Second).Sub(time.Now())
	_ = times[0].Sub(time.Now())
	_ = func() time.Time { return t }().Sub(time.Now())
	_ = time.Duration(t.Sub(time.Now()))

	// The pattern sees through parentheses around the argument, its function and the receiver, and the finding spans
	// the call inside parentheses around it.
	_ = t.Sub((time.Now()))
	_ = t.Sub((time.Now)())
	_ = (t).Sub(time.Now())
	_ = (t.Sub(time.Now()))

	// A receiver that dereferences a pointer is a time.Time.
	_ = (*pointer).Sub(time.Now())

	// A parenthesized callee is no selector as written, so it is reported with no fix.
	_ = (t.Sub)(time.Now())

	// One call inside another's receiver is two findings.
	_ = t.Add(t.Sub(time.Now())).Sub(time.Now())

	// The call and its receiver across lines.
	_ = t.Add(
		time.Second,
	).Sub(
		time.Now(),
	)
}

func leftAlone(t time.Time, other subtracter, subtracted subtracting) {
	_ = t.Sub(t)
	_ = time.Now().Sub(t)
	_ = t.Sub(time.Now().Add(0))
	_ = t.Sub(time.Unix(0, 0))
	_ = t.After(time.Now())
	_ = t.Sub(Now())
	_ = t.Sub(now())
	_ = other.Sub(time.Now())
	_ = subtracted.Sub(time.Now())

	// A method value is a variable when it is called, and a method expression passes the receiver as an argument.
	value := t.Sub
	_ = value(time.Now())
	_ = time.Time.Sub(t, time.Now())
	_ = (*time.Time).Sub(&t, time.Now())
}

// A local named time declared after the call is not in its scope there, so time still names the import.
func declaredAfter(t time.Time) {
	_ = t.Sub(time.Now())
	time := t
	_ = time
}
