package parity

import "time"

// Now is this package's own, spelled as time's is.
func Now() time.Time { return time.Time{} }

var now = time.Now

func identity[Value any](value Value) Value { return value }

type embedding struct{ time.Time }

// start is a time of the package's own, and a call at package scope stands in its file's scope, where time names the
// import.
var start = time.Unix(0, 0)

var elapsed = time.Now().Sub(start)

func reported(t time.Time, times []time.Time, pointer *time.Time, embedded embedding) {
	_ = time.Now().Sub(t)
	time.Now().Sub(t)
	_ = time.Now().Sub(time.Now())
	_ = time.Now().Sub(times[0])
	_ = time.Now().Sub(t.Add(time.Second))
	_ = time.Now().Sub(func() time.Time { return t }())
	_ = time.Now().Sub(identity(t))
	_ = time.Now().Sub(identity[time.Time](t))
	_ = time.Duration(time.Now().Sub(t))

	// The pattern sees through parentheses around the receiver, its function, the callee and the argument, and the
	// fix's argument is what lies inside the argument's parentheses. The finding spans the call inside parentheses
	// around it.
	_ = (time.Now()).Sub(t)
	_ = (time.Now)().Sub(t)
	_ = (time.Now().Sub)(t)
	_ = time.Now().Sub((t))
	_ = time.Now().Sub((t.Add((time.Second))))
	_ = (time.Now().Sub(t))

	// One call inside another's argument is two findings.
	_ = time.Now().Sub(t.Add(time.Now().Sub(t)))

	// The call and its argument across lines.
	_ = time.Now().Sub(
		t.Add(
			time.Second,
		),
	)
	_ = time.
		Now().
		Sub(t)

	// The receiver is a call of time.Now, a time.Time, so no pointer or embedding stands between it and Sub, and the
	// argument, which Sub takes as a time.Time, fits time.Since as it is: a pointer's target and an embedded field.
	_ = time.Now().Sub(*pointer)
	_ = time.Now().Sub(embedded.Time)
}

// A time declared after the call, in the call's own block, does not stand where the call does, so time still names
// the import there, and both write time.Since.
func declaredAfter(t time.Time) {
	_ = time.Now().Sub(t)
	time := t
	_ = time
}

func leftAlone(t time.Time, pointer *time.Time, embedded embedding) {
	_ = t.Sub(t)
	_ = pointer.Sub(t)
	_ = embedded.Sub(t)
	_ = t.Sub(time.Now())
	_ = time.Now().Add(0).Sub(t)
	_ = time.Now().Round(0).Sub(t)
	_ = time.Unix(0, 0).Sub(t)
	_ = time.Now().After(t)
	_ = Now().Sub(t)
	_ = now().Sub(t)

	// A method value is a variable when it is called, and a method expression passes the receiver as an argument.
	value := time.Now().Sub
	_ = value(t)
	_ = time.Time.Sub(time.Now(), t)
	_ = (*time.Time).Sub(&t, t)
}
