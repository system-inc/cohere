// Package promoted reads a field promoted through dependency.Outer's embedded Inner, which no identifier names.
package promoted

import "example.com/dependency"

// Ours embeds the dependency's struct.
type Ours struct{ dependency.Outer }

// Read is the promoted field.
func Read(ours Ours) int { return ours.Field }
