package cache

import "sync"

// executableDigest is an opened executable's sha256, computed once, by whichever asks first: start, which hashes on a
// goroutine of its own as soon as the run knows it will key by cohere-go's identity, or get. Hashing is tens of
// milliseconds over tens of megabytes, and a run that asks only as it keys its first package would wait for it there,
// between the listing and the type check; started early, it runs beside go env and go list. What is hashed is the
// file the process opened as it started either way, so starting early changes when, never what.
type executableDigest struct {
	computed func() (string, error)
}

// newExecutableDigest is the digest of opened, not yet computed. hashing, when not nil, is called as the hash begins,
// for a test to tell an early start from a hash made only when asked.
func newExecutableDigest(opened openedExecutable, hashing func()) *executableDigest {
	return &executableDigest{computed: sync.OnceValues(func() (string, error) {
		if hashing != nil {
			hashing()
		}
		return opened.digest()
	})}
}

// start begins the hash on a goroutine of its own, and returns at once. A later get waits for that one hash.
func (digest *executableDigest) start() {
	go func() {
		// Ignored: get returns the same digest and error to whoever asks.
		_, _ = digest.computed()
	}()
}

// get is the digest, waiting for the hash when start began it and it has not ended, or making it when nothing has.
func (digest *executableDigest) get() (string, error) {
	return digest.computed()
}
