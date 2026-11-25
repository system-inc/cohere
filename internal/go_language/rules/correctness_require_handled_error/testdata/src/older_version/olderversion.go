// Package older_version is a module at go 1.23, before crypto/rand.Read promised never to fail, so its error is
// judged.
package older_version

import (
	"crypto/rand"
	"hash/maphash"
)

func random(data []byte) {
	rand.Read(data)        // want `^.rand\.Read. returns an error`
	_, _ = rand.Read(data) // want `^.rand\.Read. returns an error`
	// An exemption with no version holds at every version.
	var seeded maphash.Hash
	seeded.WriteString("text")
}
