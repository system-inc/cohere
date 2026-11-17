package load

import (
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

// Fingerprints is the kept listing's directory fingerprints one run's extra builds share (#5jpss95): each directory's
// directoryFingerprint, read once for every build. A directory's fingerprint is of its entries and its Go files'
// headers, each read from the run's held bytes where it holds them and from disk otherwise, none of which depends on
// the build, so every extra build hashed what the first had just hashed, 1.2 to 1.5 CPU-s a run with the cache on
// (#74ka2zg). A directory is keyed by its path alone: the held bytes are the run's, the same for every build.
//
// Each kept listing's whole fingerprint is read once too, for every build whose kept listing names the same roots,
// directories and module files (#qqyq61s), and what two sets that differ still share, each root's walk and each module
// file's digest, once for every set: a build checking its kept listing walked cohere's root again, about 25 ms of the
// 26 its fingerprint cost once the directories were shared. A whole fingerprint is keyed by its inputs exactly as
// given, so a build whose listing names another directory reads its own, and by nothing else, since all it reads
// besides is a directory's held bytes, which are the run's for every build.
//
// It is shared by loads after which nothing writes, which the extra builds' are and the host's first load is not: the
// long-line wrap writes between that load and the builds, and a header it rewrote would keep the fingerprint of the
// bytes before it. One is made for each run, never kept past it, since any file can move between runs. A nil
// Fingerprints shares nothing, and each fingerprint is read again.
type Fingerprints struct {
	// computed counts the directories fingerprinted, which a test reads: one for each directory however many builds
	// ask.
	computed atomic.Int64

	// listingsComputed, rootsWalked and moduleFilesRead count the whole fingerprints read, the roots walked and the
	// module files read, which a test reads: one for each distinct set, root and file however many builds ask.
	// listingFingerprint counts itself, so a build that read its fingerprint past the memo is counted too.
	listingsComputed atomic.Int64
	rootsWalked      atomic.Int64
	moduleFilesRead  atomic.Int64

	mutex       sync.Mutex
	directories map[string]*fingerprintedDirectory
	listings    map[[sha256.Size]byte]*fingerprintedListing
	roots       map[string]*walkedRoot
	moduleFiles map[string]*readModuleFile
}

// fingerprintedDirectory is one directory's fingerprint, read once.
type fingerprintedDirectory struct {
	once   sync.Once
	digest [sha256.Size]byte
	whole  bool
}

// fingerprintedListing is one kept listing's whole fingerprint, read once for its set of inputs.
type fingerprintedListing struct {
	once    sync.Once
	digest  [sha256.Size]byte
	certain bool
}

// walkedRoot is one root's packageDirectories, walked once.
type walkedRoot struct {
	once        sync.Once
	directories []string
	whole       bool
}

// readModuleFile is one module file as listingFingerprint hashes it, read once: the digest of its bytes, or that it is
// absent, or that it could not be read, which is a doubt.
type readModuleFile struct {
	once     sync.Once
	digest   [sha256.Size]byte
	absent   bool
	readable bool
}

// NewFingerprints is fingerprints no build has read yet.
func NewFingerprints() *Fingerprints {
	return &Fingerprints{
		directories: map[string]*fingerprintedDirectory{},
		listings:    map[[sha256.Size]byte]*fingerprintedListing{},
		roots:       map[string]*walkedRoot{},
		moduleFiles: map[string]*readModuleFile{},
	}
}

// listing is listingFingerprint of roots, directories and moduleFiles, read once for the run for each distinct set of
// the three, or read now when fingerprints is nil. A doubt is kept as one, so every build with the set asks go list.
func (fingerprints *Fingerprints) listing(
	roots []string,
	directories []string,
	moduleFiles []string,
	held Held,
) ([sha256.Size]byte, bool) {
	if fingerprints == nil {
		return listingFingerprint(roots, directories, moduleFiles, held, nil)
	}
	// Each list is hashed with its length ahead of it, so no part moves from one list into another and two sets
	// share a key only when every list is the same, in the same order, which the fingerprint hashes them in.
	digest := sha256.New()
	for _, inputs := range [][]string{roots, directories, moduleFiles} {
		writeParts(digest, []string{strconv.Itoa(len(inputs))})
		writeParts(digest, inputs)
	}
	key := [sha256.Size]byte(digest.Sum(nil))
	fingerprints.mutex.Lock()
	read := fingerprints.listings[key]
	if read == nil {
		read = &fingerprintedListing{}
		fingerprints.listings[key] = read
	}
	fingerprints.mutex.Unlock()
	read.once.Do(func() {
		read.digest, read.certain = listingFingerprint(roots, directories, moduleFiles, held, fingerprints)
	})
	return read.digest, read.certain
}

// root is packageDirectories of root, walked once for the run, or walked now when fingerprints is nil. The
// directories it returns are every build's, so no caller may change them.
func (fingerprints *Fingerprints) root(root string) ([]string, bool) {
	if fingerprints == nil {
		return packageDirectories(root)
	}
	fingerprints.mutex.Lock()
	walked := fingerprints.roots[root]
	if walked == nil {
		walked = &walkedRoot{}
		fingerprints.roots[root] = walked
	}
	fingerprints.mutex.Unlock()
	walked.once.Do(func() {
		fingerprints.rootsWalked.Add(1)
		walked.directories, walked.whole = packageDirectories(root)
	})
	return walked.directories, walked.whole
}

// moduleFile is file's digest, whether it is absent, and whether it could be read, read once for the run, or read
// now when fingerprints is nil.
func (fingerprints *Fingerprints) moduleFile(file string) ([sha256.Size]byte, bool, bool) {
	if fingerprints == nil {
		return moduleFileDigest(file)
	}
	fingerprints.mutex.Lock()
	read := fingerprints.moduleFiles[file]
	if read == nil {
		read = &readModuleFile{}
		fingerprints.moduleFiles[file] = read
	}
	fingerprints.mutex.Unlock()
	read.once.Do(func() {
		fingerprints.moduleFilesRead.Add(1)
		read.digest, read.absent, read.readable = moduleFileDigest(file)
	})
	return read.digest, read.absent, read.readable
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
