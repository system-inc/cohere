// Package namesake reaches a package named foreign that is not the one lookalike reaches, so what one package declares
// is told from another by the package itself, never by its name, and no package is given what another reaches.
package namesake

import "elsewhere/foreign"

// The same Cmd and Wait lookalike's shell has, where no package this one reaches declares Cmd, so Cmd is ours.
type shell struct{}

func (shell) Cmd() string { return "" } // want `Identifier "Cmd" abbreviates "Cmd"\. Use "Command"\.`
func (shell) Wait() error { return nil }

var _ foreign.Commander = shell{}
