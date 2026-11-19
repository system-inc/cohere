// Package deep is two imports below the facts fixture, so no fact on its functions reaches there.
package deep

// Pure is pure, which only a package importing this one directly holds.
func Pure(count int) int { return count * 2 }
