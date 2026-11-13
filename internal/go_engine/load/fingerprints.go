package load

import (
	"crypto/sha256"
	"sync"
	"sync/atomic"
)

// Fingerprints is the kept listing's directory fingerprints one run's extra builds share (#5jpss95): each directory's
// directoryFingerprint, read once for every build. A directory's fingerprint is of its entries and its Go files'
// headers, each read from the run's held bytes where it holds them and from disk otherwise, none of which depends on
// the build, so every extra build hashed what the first had just hashed, 1.2 to 1.5 CPU-s a run with the cache on
// (#74ka2zg). A directory is keyed by its path alone: the held bytes are the run's, the same for every build.
//
// It is shared by loads after which nothing writes, which the extra builds' are and the host's first load is not: the
// long-line wrap writes between that load and the builds, and a header it rewrote would keep the fingerprint of the
// bytes before it. A nil Fingerprints shares nothing, and each fingerprint is read again.
type Fingerprints struct {
	// computed counts the directories fingerprinted, which a test reads: one for each directory however many builds
	// ask.
	computed atomic.Int64

	mutex       sync.Mutex
	directories map[string]*fingerprintedDirectory
}

// fingerprintedDirectory is one directory's fingerprint, read once.
type fingerprintedDirectory struct {
	once   sync.Once
	digest [sha256.Size]byte
	whole  bool
}

// NewFingerprints is fingerprints no build has read yet.
func NewFingerprints() *Fingerprints {
	return &Fingerprints{directories: map[string]*fingerprintedDirectory{}}
}

// directory is directoryFingerprint of directory, read once for the run, or read now when fingerprints is nil.
func (fingerprints *Fingerprints) directory(directory string, held Held) ([sha256.Size]byte, bool) {
	if fingerprints == nil {
		return directoryFingerprint(directory, held)
	}
	fingerprints.mutex.Lock()
	read := fingerprints.directories[directory]
	if read == nil {
		read = &fingerprintedDirectory{}
		fingerprints.directories[directory] = read
	}
	fingerprints.mutex.Unlock()
	read.once.Do(func() {
		fingerprints.computed.Add(1)
		read.digest, read.whole = directoryFingerprint(directory, held)
	})
	return read.digest, read.whole
}
