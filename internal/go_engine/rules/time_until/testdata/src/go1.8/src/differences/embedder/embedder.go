// Package embedder declares structs that embed a time.Time behind an unexported field, for the differences fixture.
package embedder

import "time"

type inner struct{ time.Time }

// Promoted's time.Time is named Time through its unexported field.
type Promoted struct{ inner }

// Hidden's field named Time is an int, so its time.Time is reached through its unexported field alone.
type Hidden struct {
	inner
	Time int
}
