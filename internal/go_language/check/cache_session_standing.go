package check

// markStood marks the package identified by packageIdentifier as one something stood in (stood, #e4rd5dw), which the
// session writes no entry of: a confined rule there reports the host's findings in the standing bodies as its own, so
// the package's entry would carry the host's verdict under the build's key, a partial lint read back later as a whole
// one. Run marks each package an extra build's Options.StandingBodies answers something for, from several actions at
// once.
func (session *CacheSession) markStood(packageIdentifier string) {
	session.stoodLock.Lock()
	defer session.stoodLock.Unlock()
	if session.stood == nil {
		session.stood = map[string]bool{}
	}
	session.stood[packageIdentifier] = true
}

// stoodIn reports whether anything stood in the package identified by packageIdentifier in any load of the session,
// which a reload never clears: a package that stood once is checked from source again by the build's next run.
func (session *CacheSession) stoodIn(packageIdentifier string) bool {
	session.stoodLock.Lock()
	defer session.stoodLock.Unlock()
	return session.stood[packageIdentifier]
}
