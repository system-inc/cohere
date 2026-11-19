package check

import (
	"sync"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// decodedBodies is a run's memo of the entry bodies its sessions read from the store and decoded, by the sha256 of each
// body, so a body is decoded once a run however many builds' records name it (#qqyq61s). The host's session fills it
// with every hit it reads from its store, and each extra build with every body it reads from disk that the memo lacked,
// so the next build shares it. A build's hit whose own record names a body the memo holds takes that decode with the
// record laid over it (CacheSession.sharedHit), and reads neither the host's record at BodyAt nor the body's gob.
//
// What it holds of a decode is what comes from the body alone: the analyses, the types' Imports and Withheld, the
// files, output, answers, bodiless and slots. The reads, the export data's digest and reader, and the types' late
// decision are the record's or the build's, so each is left out here and set from the hit's own record. Decoding is a
// function of the body's bytes and the analyzers alone, so a decode is shared only with a session whose analyzers
// decode alike known's (decodesAlike), the host's.
type decodedBodies struct {
	known *cacheAnalyzers

	// lock guards byDigest, which readHits' workers read and add to at once. A decode is added only once it is whole,
	// and never written after, so a reader holds it without the lock.
	lock     sync.RWMutex
	byDigest map[cache.Key]*restoredPackage
}

// newDecodedBodies is an empty memo of the bodies decoded by known, the analyzers of the session that fills it first.
func newDecodedBodies(known *cacheAnalyzers) *decodedBodies {
	return &decodedBodies{known: known, byDigest: map[cache.Key]*restoredPackage{}}
}

// add keeps decoded, a body read from the store and decoded whole by analyzers alike the memo's, by its body's digest,
// unless the memo holds that body already, which is the same decode of the same bytes. It keeps a copy holding only
// what came from the body, so the session that decoded it may go on to set its hit's reader and late decision.
func (bodies *decodedBodies) add(decoded *restoredPackage) {
	if bodies == nil || len(decoded.bodyDigest) != len(cache.Key{}) {
		return
	}
	digest := cache.Key(decoded.bodyDigest)
	kept := *decoded
	kept.types = &load.Restored{Imports: decoded.types.Imports, Withheld: decoded.types.Withheld}
	kept.reads, kept.readsIncomplete, kept.exportDigest = nil, false, nil
	bodies.lock.Lock()
	defer bodies.lock.Unlock()
	if _, held := bodies.byDigest[digest]; !held {
		bodies.byDigest[digest] = &kept
	}
}

// release lets go of every decode the memo holds, once no build is left to ask for one (HostRun.Release), so the host's
// session, which holds the memo too, holds none past the builds.
func (bodies *decodedBodies) release() {
	if bodies == nil {
		return
	}
	bodies.lock.Lock()
	defer bodies.lock.Unlock()
	bodies.byDigest = map[cache.Key]*restoredPackage{}
}

// of is the decode of the body whose sha256 is digest, and whether the memo holds one.
func (bodies *decodedBodies) of(digest []byte) (*restoredPackage, bool) {
	if bodies == nil || len(digest) != len(cache.Key{}) {
		return nil, false
	}
	bodies.lock.RLock()
	defer bodies.lock.RUnlock()
	shared, held := bodies.byDigest[cache.Key(digest)]
	return shared, held
}
