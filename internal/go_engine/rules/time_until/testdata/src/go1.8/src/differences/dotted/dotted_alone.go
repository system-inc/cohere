package dotted

import . "time"

// The file imports time into its own scope alone, so the original's time.Until names nothing, and the port names
// Until alone, as the call names Now.
func dotted(t Time, pointer *Time) {
	// The original writes time.Until(t), and the port Until(t).
	_ = t.Sub(Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
	// The original writes time.Until(pointer), and the port Until(*pointer).
	_ = pointer.Sub(Now()) // port moves: should use time.Until instead of t.Sub(time.Now())
	// A parenthesized callee takes no fix on either side.
	_ = (t.Sub)(Now())
}

// A local named Until stands over time's, so no name reaches time.Until here.
func covered(t Time) {
	Until := t
	_ = Until
	// The original writes time.Until(t), which names nothing, and Until here is a time.
	_ = t.Sub(Now()) // port withholds: Replace with call to time.Until
}
