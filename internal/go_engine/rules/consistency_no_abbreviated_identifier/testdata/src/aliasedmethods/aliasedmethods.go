// Package aliasedmethods declares Cmd on a concrete type that has all of foreign.Commanding and on an interface of
// its own with Commanding's exact signature. An alias is indexed where the type it names is declared, and Commanding
// names no declared type, so nothing fixes either Cmd.
package aliasedmethods

import "aliasedmethods/foreign"

type console struct{}

func (console) Cmd() string { return "" } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`

var _ foreign.Commanding = console{}

type commanding interface {
	Cmd() string // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`
}
