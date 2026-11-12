package check

import (
	"path/filepath"
	"slices"
	"testing"
)

// The host's run lets go of what its analysis built to answer questions once it has kept its packages for the extra
// builds, with a store as without one (#px1yxq8): kept, the resolver held every package's syntax, type maps and
// intermediate representation as the builds started, about 3.3 GB at cohere's root, so a cold run with the findings
// cache ran its builds one at a time. Over the write-through fixture with a store, the host's session holds neither
// the resolver nor its answer memo after the run, and windows still restores user from what the host kept.
func TestTheHostLetsItsResolverGoOnceItHasKeptWithAStore(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	host, session := throughHostSession(test, root, filepath.Join(stores, "host"))
	if session.caching.Store == nil || host.EntriesKept() == 0 {
		test.Fatalf("the host's run has a store %t and kept %d entries, want a store and some kept",
			session.caching.Store != nil, host.EntriesKept())
	}
	session.answering.lock.Lock()
	answerMemo := session.answering.byTypes
	session.answering.lock.Unlock()
	if session.resolver != nil || answerMemo != nil {
		test.Errorf("the host's session still holds its resolver (%t) and its answer memo (%t) once it kept for the "+
			"builds, want neither", session.resolver != nil, answerMemo != nil)
	}
	through, _ := throughBuild(test, root, filepath.Join(stores, "build"), host)
	if !slices.Contains(through.FromHost.Restored, throughUserPath) {
		test.Errorf("windows restored %q from the host, want %s among them", through.FromHost.Restored, throughUserPath)
	}
}
