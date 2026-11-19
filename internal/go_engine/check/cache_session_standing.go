package check

import "github.com/system-inc/cohere/internal/go_engine/rule"

// keepingStandingOut is standingBodies, an extra build's Options.StandingBodies, marking each package it answers
// something standing for as one the session writes no entry of (stood, #e4rd5dw): a confined rule there reports the
// host's findings in the standing bodies as its own, so the package's entry would carry the host's verdict under the
// build's key, a partial lint read back later as a whole one. It is called from several actions at once.
func (session *CacheSession) keepingStandingOut(
	standingBodies func(packageId string, after any) rule.StandingBodies,
) func(packageId string, after any) rule.StandingBodies {
	return func(packageId string, after any) rule.StandingBodies {
		standing := standingBodies(packageId, after)
		if standing != nil {
			session.stoodLock.Lock()
			defer session.stoodLock.Unlock()
			if session.stood == nil {
				session.stood = map[string]bool{}
			}
			session.stood[packageId] = true
		}
		return standing
	}
}

// stoodIn reports whether anything stood in the package identified by packageIdentifier in any load of the session,
// which a reload never clears: a package that stood once is checked from source again by the build's next run.
func (session *CacheSession) stoodIn(packageIdentifier string) bool {
	session.stoodLock.Lock()
	defer session.stoodLock.Unlock()
	return session.stood[packageIdentifier]
}
