package load

import (
	"crypto/sha256"
	"strconv"
	"sync"
	"sync/atomic"
)

// Fingerprints is what one run's extra builds share of their kept listings' fingerprints, each read once for every
// build. Each directory's directoryFingerprint (#5jpss95): a directory's fingerprint is of its entries and its Go
// files' headers, each read from the run's held bytes where it holds them and from disk otherwise, none of which
// depends on the build, so every extra build hashed what the first had just hashed, 1.2 to 1.5 CPU-s a run with the
// cache on (#74ka2zg). A directory is keyed by its path alone: the held bytes are the run's, the same for every build.
//
// Each kept listing's steady fingerprint is read once too (steadyFingerprint), for every build whose kept listing names
// the same roots, directories and steady module files (#qqyq61s), and what two sets that differ still share, each
// root's walk and each steady module file's digest, once for every set: a build checking its kept listing walked
// cohere's root again, about 25 ms of the 26 its fingerprint cost once the directories were shared. A steady
// fingerprint is keyed by its inputs exactly as given, so a build whose listing names another directory reads its own.
// Nothing else need be in the key: besides the held bytes, the run's for every build, it reads the disk, module files,
// directory entries, Go files the run does not hold and the go.mod each walk looks for, and nothing the run writes
// moves any of that once the builds start.
//
// A module file a go command of the run can rewrite is the exception, and is never held here (rewritableByGo):
// go.work.sum, which a build's go list rewrites when the workspace's guard cannot run it beside a side copy of the
// go.work (workspaceSumGuard), and go.mod and go.sum under -mod=mod. listingFingerprint reads each again at every ask
// and folds it in after the steady fingerprint, so a build asking after another build's go list rewrote one reads the
// bytes it left, and keeps its listing against them.
//
// It is shared by the loads that read the same bytes: a run's are the host's and every extra build's, and the run
// starts its Fingerprints over at every change to the bytes it holds (the pipeline's heldChanged), so a header the
// fix, the wrap or a rename rewrote is read again by the load after it and never keeps the fingerprint of the bytes
// before it (#t7h2sm4). A writer outside the run, an editor saving while the builds run, is read by the next run: the
// builds check the tree as the host's last load read it. One is made for each run (newPipeline), never kept past it,
// since any file can move between runs. A nil Fingerprints shares nothing, and each fingerprint is read again.
type Fingerprints struct {
	// computed counts the directories fingerprinted, which a test reads: one for each directory however many builds
	// ask.
	computed atomic.Int64

	// listingsComputed, rootsWalked and moduleFilesRead count the steady fingerprints read, the roots walked and the
	// steady module files read, which a test reads: one for each distinct set, root and file however many builds ask.
	// steadyFingerprint counts itself when it is handed fingerprints, so a build that read past the memo and handed
	// them on is counted too; one that handed on nil counts nothing, and a test sees its set missing from listings.
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

// fingerprintedListing is one kept listing's steady fingerprint, read once for its set of inputs.
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

// readModuleFile is one steady module file as steadyFingerprint hashes it, read once: the digest of its bytes, or that
// it is absent, or that it could not be read, which is a doubt.
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

// DirectoriesFingerprinted is how many directories fingerprints holds and how many times one was fingerprinted, which
// the engine's tests read to see that a load of the run filled the ones the loads after it share (#t7h2sm4).
func (fingerprints *Fingerprints) DirectoriesFingerprinted() (int, int64) {
	fingerprints.mutex.Lock()
	defer fingerprints.mutex.Unlock()
	return len(fingerprints.directories), fingerprints.computed.Load()
}

// steady is steadyFingerprint of roots, directories and moduleFiles, read once for the run for each distinct set of
// the three, or read now when fingerprints is nil. moduleFiles never names a file a go command of the run can
// rewrite, since listingFingerprint leaves each out and reads it itself, so nothing held here is of bytes since
// rewritten. A doubt is kept as one, so every build with the set asks go list.
func (fingerprints *Fingerprints) steady(
	roots []string,
	directories []string,
	moduleFiles []string,
	held Held,
) ([sha256.Size]byte, bool) {
	if fingerprints == nil {
		return steadyFingerprint(roots, directories, moduleFiles, held, nil)
	}
	// Each list is hashed with its length ahead of it, so no part moves from one list into another, and two sets share
	// a key only when each list is the same in the same order. The fingerprint sorts the directories it reads, so the
	// key is stricter than it there, which costs at most a second read of one set, and none while every kept listing
	// names its directories sorted, as keepListing does.
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
		read.digest, read.certain = steadyFingerprint(roots, directories, moduleFiles, held, fingerprints)
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
// now when fingerprints is nil. Only steadyFingerprint asks, so file is never one a go command of the run can rewrite.
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
