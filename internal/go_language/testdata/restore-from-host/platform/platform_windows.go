package platform

// Size is the value's size.
func (shared Shared) Size() int64 { return int64(shared.Value) }

// Open is no error.
func Open() error { return nil }

// Logf is its format, its arguments left alone.
func Logf(format string, arguments ...any) string { return format + string(rune(len(arguments))) }
