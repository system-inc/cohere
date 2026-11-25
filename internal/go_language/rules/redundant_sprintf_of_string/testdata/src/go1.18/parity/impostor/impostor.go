// Package impostor calls a Sprintf that is not fmt's: one of a package named fmt, and one of its own.
package impostor

import "example.com/parity/impostor/fmt"

func Sprintf(format string, values ...any) string { return format }

func impostor(text string) {
	_ = fmt.Sprintf("%s", text)
	_ = Sprintf("%s", text)
}
