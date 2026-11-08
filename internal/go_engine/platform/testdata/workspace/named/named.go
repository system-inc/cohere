// Package named references a differing object by its name.
package named

import "example.com/dependency"

// Read is what dependency.Named returns.
func Read() int { return dependency.Named() }
