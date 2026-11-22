package impl

import "fmt"

// Emit formats the format and how many values it was handed, and drops the text, so it wraps nothing.
func Emit(format string, args ...any) {
	_ = fmt.Sprint(format, len(args))
}
