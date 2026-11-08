// Package tested_command imports cobra only in its test file, and is analyzed with its tests, as its test variant,
// whose imports then hold cobra, so both judge it a command and leave its ticks alone.
package tested_command

import "time"

// Found by neither: the package, with its tests, imports cobra.
func returns() {
	<-time.Tick(time.Second)
}
