// Package tested_command imports cobra only in its test file. Analyzed alone it imports no cobra, so both judge it no
// command; analyzed with its tests, as its test variant, its imports hold cobra, so both leave its ticks alone.
package tested_command

import "time"

// Found by both in the plain package, and by neither in its test variant, which with its tests imports cobra.
func returns() {
	<-time.Tick(time.Second)
}
