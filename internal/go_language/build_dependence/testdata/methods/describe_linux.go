//go:build linux

package methods

// Describe is declared on linux only.
func (Plain) Describe() string { return "linux" }

// Speaking embeds Describer on linux alone, so every interface that embeds it gains Describe there.
type Speaking interface {
	Describer
	Speak() string
}
