// Package locked copies a Locked.
package locked

import "example.com/fieldwalks/platform"

// Copy copies what locked points to.
func Copy(locked *platform.Locked) {
	held := *locked
	_ = held
}
