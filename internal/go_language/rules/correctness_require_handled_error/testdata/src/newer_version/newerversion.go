// Package newer_version is a module at go 1.24, from which crypto/rand.Read never fails, so its error needs no
// handling.
package newer_version

import "crypto/rand"

func random(data []byte) {
	rand.Read(data)
	_, _ = rand.Read(data)
}
