package facts

// Renamed is declared after a line directive, as a cgo package's function is, so its place is in the file the
// directive names, at its offset in this one.
//
//line renamed_as_written.go:1
func Renamed() int { return 2 }
