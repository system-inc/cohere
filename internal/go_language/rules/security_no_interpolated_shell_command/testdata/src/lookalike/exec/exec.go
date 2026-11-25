// Package exec is a stand-in with os/exec's spelling and none of its identity, so a Command called through it is
// someone else's function.
package exec

// Cmd is what Command returns.
type Cmd struct{ Program string }

// Command records the program and runs nothing.
func Command(name string, arguments ...string) *Cmd {
	return &Cmd{Program: name}
}
