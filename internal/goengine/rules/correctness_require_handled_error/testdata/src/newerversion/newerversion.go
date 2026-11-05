// Package newerversion is a module at go 1.24, from which crypto/rand.Read never fails, so its error needs no
// handling.
package newerversion

import "crypto/rand"

func random(data []byte) {
	rand.Read(data)
	_, _ = rand.Read(data)
}
