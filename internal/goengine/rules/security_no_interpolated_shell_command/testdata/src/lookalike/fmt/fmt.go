// Package fmt is a stand-in with fmt's spelling and none of its identity, so a Sprintf called through it is someone
// else's function.
package fmt

// Sprintf returns its format and formats nothing.
func Sprintf(format string, arguments ...any) string {
	return format
}
