//go:build !windows

package platform

// Printed is a name, which printf's %d does not fit.
type Printed struct{ Name string }

// Locked holds no lock.
type Locked struct{ Count int }

// Encoded is of a fixed size.
type Encoded struct{ Count int32 }

// Measured holds no float.
type Measured struct{ Count int }

// Basic holds basic values alone.
type Basic struct{ Count int }

// Hidden has no exported field.
type Hidden struct{ count int }

// Source is copied field by field into a Target.
type Source struct{ Name string }

// Target has Source's fields.
type Target struct{ Name string }

// Pair is two ints.
type Pair struct{ Left, Right int }

// Stamped reaches its time through stamp.
type Stamped struct{ stamp }

// Channeled marshals.
type Channeled struct{ Name string }

// Tagged is named name.
type Tagged struct {
	Name string `json:"name"`
}

// Titled is named name too.
type Titled struct {
	Title string `json:"name"`
}

// Key is comparable.
type Key struct{ Name string }
