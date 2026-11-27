package check

import "github.com/system-inc/cohere/internal/go_language/cache"

// The host reading cold (#s8e6z0n). A host whose store holds every package restores nearly all of them, so it keeps for
// the extra builds only what it checks from source, nearly nothing, and a build whose own store holds nothing of its
// own then checks every package itself: at cohere's root about 1,595 each, its width falling to one, 26 to 31 s on a
// 16-core box against a fully cold run's 8.7 s. Read cold, the host checks every package from source and keeps each
// for the builds, which restore from it as they do on a cold run: 8.8 to 9.1 s there, its findings the same. So the
// host's session asks once, at its first Restore, whether this run is such a one (ReadColdWhen); when it is, every
// lookup of the project's store, by a package's first key or its second, reads as absent, as a cold run's would, and
// it writes no entry the store holds already, so the warm store it read nothing of stays exactly as it was. Everything
// else is as in any run: the system cache is read and written, the session keeps for the builds what it checks, and
// the builds read and write the project's store.

// ReadColdWhen has the session, the host's of a run with extra builds, ask decide once, at its first Restore and before
// it reads anything of the project's store, whether it reads that store cold this run, and why: decide answers the
// reason, a word the record carries (CacheCounts.HostReadCold), or nothing for a run that reads as always. The answer
// holds for every load of the run, a reload's and the long-line wrap's included. It is asked at the restore rather
// than now since what it reads, each build's go env, may still be running when the host's load begins.
func (session *CacheSession) ReadColdWhen(decide func() string) {
	session.decideReadCold = decide
}

// takeReadCold asks the session's decideReadCold, once, before the first Restore reads anything (ReadColdWhen).
func (session *CacheSession) takeReadCold() {
	if session.decideReadCold == nil {
		return
	}
	decide := session.decideReadCold
	session.decideReadCold = nil
	session.readCold = decide()
}

// readsColdFrom reports whether the session reads store cold: the project's store, once the host's session has taken
// the cold read (ReadColdWhen), and never the system cache's.
func (session *CacheSession) readsColdFrom(store *cache.Store) bool {
	return session.readCold != "" && store != nil && store == session.caching.Store
}
