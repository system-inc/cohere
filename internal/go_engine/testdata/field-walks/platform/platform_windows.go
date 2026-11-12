package platform

import "sync"

// Printed is a count, which printf's %d fits.
type Printed struct{ Count int }

// Locked holds a lock.
type Locked struct{ lock sync.Mutex }

// Encoded is of no fixed size.
type Encoded struct{ Count int }

// Measured holds a float.
type Measured struct{ Ratio float64 }

// Basic holds a slice.
type Basic struct{ Names []string }

// Hidden has an exported field.
type Hidden struct{ Count int }

// Source is copied field by field into a Target.
type Source struct{ Name string }

// Target has a field Source lacks.
type Target struct {
	Name  string
	Extra int
}

// Pair is two int64s.
type Pair struct{ Left, Right int64 }

// Stamped declares a field Time that shadows the time stamp holds.
type Stamped struct {
	stamp
	Time int
}

// Channeled holds a channel, which does not marshal.
type Channeled struct{ Done chan int }

// Tagged is named title.
type Tagged struct {
	Name string `json:"title"`
}

// Titled is named heading.
type Titled struct {
	Title string `json:"heading"`
}

// Key holds a slice, so it is not comparable.
type Key struct{ Names []string }
