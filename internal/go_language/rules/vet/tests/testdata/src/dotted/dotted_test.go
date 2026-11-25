package dotted

import . "testing"

// testing imported with a dot: *T is written without the package.

func TestdotImported(t *T) {}

func FuzzDotImported(f *F) {
	f.Add("seed", 1)
	f.Fuzz(func(t *T, text string) {})
}
