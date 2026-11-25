package q

// platformLog is windows's, which prints the format and how many values it was handed, and wraps nothing.
func platformLog(format string, values ...any) {
	println(format, len(values))
}
