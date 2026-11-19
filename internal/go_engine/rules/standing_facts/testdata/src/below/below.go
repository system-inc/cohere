// Package below is the package the facts fixture imports directly.
package below

import "deep"

// Pure is pure, and exported, so its fact reaches every package that imports this one.
func Pure(count int) int { return count + 4 }

// Through is pure, since deep.Pure is.
func Through(count int) int { return deep.Pure(count) }

// Stop never returns.
func Stop() { panic("stopped") }
