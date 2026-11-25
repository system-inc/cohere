// Package foreign shares its name with the foreign beside the fixtures, and declares a Commander without Cmd. A
// package that imports this one reaches nothing that fixes Cmd, so its Cmd is its own. The rule is not run over it.
package foreign

// Commander is a plain interface that shares its name with foreign.Commander and none of its methods.
type Commander interface {
	Wait() error
}
