// Package indirect compares with nil the result of a method whose type is declared two packages away: it imports mid
// alone, and mid.New returns a pointer to deep's Core, so the callee is a function of a package indirect never
// imports.
package indirect

import "indirect/mid"

// compared's callee is deep's (*Core).Err, which never returns a nil interface value and may return one holding a nil
// pointer, so the comparison is always true and the port reports it. The original panics before it can: it holds no
// function for a method of a package indirect never imports, and hands irutil.IsTrivial nil.
func compared() bool {
	return mid.New().Err() != nil // port finds: this comparison is always true
}
