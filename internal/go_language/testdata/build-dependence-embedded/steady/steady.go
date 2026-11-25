// Package steady embeds nothing a platform shapes, so no build changes it.
package steady

// Steady is a count, the same in every build.
type Steady struct {
	Count int
}
