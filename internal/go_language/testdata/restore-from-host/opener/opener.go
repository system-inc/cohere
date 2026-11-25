// Package opener asks whether Open failed, which every build answers by Open's body.
package opener

import "example.com/restore/platform"

// Failed is whether Open failed.
func Failed() bool { return platform.Open() != nil }
