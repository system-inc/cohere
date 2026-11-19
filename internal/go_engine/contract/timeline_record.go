package contract

// TimelineRecord is additive within contract 3, and the Go engine's alone: under `--timeline` it says when each phase
// of the run and each build began and ended, on one clock from the engine process's start, with the process's CPU so
// far at each, so a reader sees where the wall time went and how many cores were busy between any two stamps. It is
// written once, the last record before the summary, after the inputs record when there is one.
//
// It changes nothing about the run: the cache, the builds' width and every finding are what they are without it. A run
// also under `--timing` has a timed run's shape, no cache and builds one at a time, which Timed says, so its timeline
// is never read as a plain run's.
//
// Microseconds throughout, as the timing record's, because a stamp a few hundred microseconds after the one before it
// is a real segment, and a 0 would read as none.
type TimelineRecord struct {
	// Unavailable says why no CPU was read, when the process's CPU clock cannot be read on this platform. Every CPU
	// field is then 0 and means nothing; the wall is still real. Empty when the CPU was read.
	Unavailable string `json:"unavailable,omitempty"`

	// Timed is whether the run was also under `--timing`, so its shape is a timed run's: no cache at all, and the extra
	// builds one at a time. Omitted when false.
	Timed bool `json:"timed,omitempty"`

	// Stamps holds each stamp in the order it was taken, which is the order of its wall. Written `[]` when there are
	// none.
	Stamps []TimelineStamp `json:"stamps"`
}

func (TimelineRecord) kind() Kind { return KindTimeline }

// MarshalJSON writes no stamps as `[]`, never `null`, so a reader never has to tell "nothing stamped" from "not
// recorded".
func (record TimelineRecord) MarshalJSON() ([]byte, error) {
	// withoutMarshaler has TimelineRecord's fields and not its methods, so encoding it does not recurse here.
	type withoutMarshaler TimelineRecord
	declared := withoutMarshaler(record)
	if declared.Stamps == nil {
		declared.Stamps = []TimelineStamp{}
	}
	return encodeWithoutHtmlEscaping(declared)
}

// TimelineStamp is one moment of the run: its label, such as `host.load.end` or `build windows/amd64.gate.taken`, the
// wall since the engine process started, and the process's user and system CPU from its start to then.
type TimelineStamp struct {
	Label              string `json:"label"`
	WallMicroseconds   int64  `json:"wallMicroseconds"`
	UserMicroseconds   int64  `json:"userMicroseconds"`
	SystemMicroseconds int64  `json:"systemMicroseconds"`
}
