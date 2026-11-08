// Package record_above holds two records on consecutive lines, a reason above the first alone: a record on the line
// above is no reason for the one below it.
package record_above

func gained() {}

func calls() {
	// The port rightly finds this call.
	gained() // port finds: call to gained
	gained() // port finds: call to gained
}
