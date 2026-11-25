// Package facts is the build's package the standing_facts tests settle: each function is one a test's host names, with
// the fact or answer of it that the build holds said beside it.
package facts

import "below"

// Probe's one method is a function only an objectpath reaches.
var Probe interface{ Ping() }

type gauge struct{}

// read returns in this build.
func (gauge) read() int { return 3 }

// halt never returns in this build.
func (gauge) halt() { panic("halted") }

// platformSize is pure in this build, where a host's file could declare it otherwise.
func platformSize(words int) int { return words * 8 }

// size is pure in this build, since platformSize is.
func size(words int) int { return platformSize(words) }

// scale is pure, and its one parameter holds no purity.
func scale(factor int) int { return factor * 2 }

// Warm uses size.
func Warm() { size(2) }

// Reading uses gauge's read and scale.
func Reading() int { return scale(gauge{}.read()) }

// stop never returns in this build.
func stop() { panic("stopped") }

// Caller uses stop.
func Caller() { stop() }

// Quit calls below.Stop as a statement, so the package reads its no-return fact.
func Quit() { below.Stop() }

// Total uses below.Pure, whose fact reaches this package, and below.Through, which reaches deep.Pure, whose fact
// does not.
func Total() int { return below.Pure(1) + below.Through(1) }

// Ping uses Probe's method.
func Ping() { Probe.Ping() }
