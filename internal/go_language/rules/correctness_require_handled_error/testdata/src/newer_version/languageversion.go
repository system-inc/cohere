//go:build go1.23

// This file's //go:build line lowers its language to go1.23, but the module's go line still requires a 1.24
// toolchain to build it, so crypto/rand.Read cannot fail here either.
package newer_version

import "crypto/rand"

func randomUnderAnOlderLanguage(data []byte) {
	rand.Read(data)
}
