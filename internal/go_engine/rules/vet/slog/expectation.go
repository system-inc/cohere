package slog

// expectation is what the next argument in a call's key-value pairs should hold. x/tools' slog names it position.
type expectation int

const (
	// expectKey is a position that should hold a string key or a slog.Attr. x/tools' key.
	expectKey expectation = iota
	// expectValue is a position that should hold a value, which may be anything. x/tools' value.
	expectValue
	// expectUnknown is a position whose expectation is unknown, since an argument before it may have been a key or an
	// Attr. x/tools' unknown.
	expectUnknown
)
