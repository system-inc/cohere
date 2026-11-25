package check

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// nothingStanding is a package's standing bodies under which no function stands and no rule is confined: an answer,
// all the same, so the package is one something was answered for.
type nothingStanding struct{}

// Stands answers that no function stands.
func (nothingStanding) Stands(string, int) bool { return false }

// Confines answers that no rule is confined.
func (nothingStanding) Confines(string) bool { return false }

// Findings answers that the host holds no findings.
func (nothingStanding) Findings(string, string) ([]rule.InheritedFinding, bool) { return nil, false }

// Took records nothing.
func (nothingStanding) Took(string, string, int, int) {
	// No rule is confined, so none takes anything.
}

// standingAt is check.Options.StandingBodies answering nothingStanding for the package identified by
// packageIdentifier alone, and nil for every other.
func standingAt(packageIdentifier string) func(packageId string, after any) rule.StandingBodies {
	return func(packageId string, _ any) rule.StandingBodies {
		if packageId != packageIdentifier {
			return nil
		}
		return nothingStanding{}
	}
}

// standingBuild is windows's run over root under caching, restoring from host when it is not nil, and loaded again
// until no reload is asked, as buildWith is, with standingBodies as each run's Options.StandingBodies, and before,
// when not nil, given the session before the first load.
func standingBuild(
	test *testing.T,
	root string,
	caching *Caching,
	host *HostRun,
	standingBodies func(packageId string, after any) rule.StandingBodies,
	before func(session *CacheSession),
) (*Result, *CacheSession) {
	test.Helper()
	session := NewCacheSession(caching, throughRules())
	session.RestoreFromHost(host)
	if before != nil {
		before(session)
	}
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"},
			nil, load.Caching{Restorer: session})
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading windows's build: %v %+v", loadError, loaded.Diagnostics)
		}
		result, runError := Run(loaded.Packages, throughRules(), Options{Cache: session, Withheld: loaded.Withheld,
			Bodiless: loaded.Bodiless, StandingBodies: standingBodies})
		if runError != nil {
			test.Fatalf("running windows's build: %v", runError)
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			if len(result.Crashes) > 0 {
				test.Fatalf("windows's build crashed: %+v", result.Crashes)
			}
			return result, session
		}
	}
}

// A package something stood in is never saved as a miss (#e4rd5dw): its findings in the standing bodies are the
// host's, so its entry would carry a partial lint under the build's own key. With the standing bodies answering
// something for user alone, windows checks both packages from source and writes base's entry, never user's, which
// the store does not hold, and with nothing answered it writes both, one more entry.
func TestAPackageSomethingStoodInIsNeverSaved(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	result, session := standingBuild(test, root, windowsCachingAt(test.TempDir(), throughRules()), nil,
		standingAt(throughUserPath), nil)
	whole, wholeSession := standingBuild(test, root, windowsCachingAt(test.TempDir(), throughRules()), nil, nil, nil)

	if !session.stoodIn(throughUserPath) || session.stoodIn(throughBasePath) {
		test.Fatalf("the session marked user stood %t and base %t, want user alone",
			session.stoodIn(throughUserPath), session.stoodIn(throughBasePath))
	}
	base, user := throughPackage(test, session, throughBasePath), throughPackage(test, session, throughUserPath)
	if result.Cache.Written != whole.Cache.Written-1 || !session.caching.Store.Holds(session.keys[base]) ||
		session.caching.Store.Holds(session.keys[user]) {
		test.Errorf("windows wrote %d entries, base's held %t and user's %t, want one fewer than the %d it writes "+
			"with nothing standing, base's held and not user's", result.Cache.Written,
			session.caching.Store.Holds(session.keys[base]), session.caching.Store.Holds(session.keys[user]),
			whole.Cache.Written)
	}
	wholeUser := throughPackage(test, wholeSession, throughUserPath)
	if !wholeSession.caching.Store.Holds(wholeSession.keys[wholeUser]) || wholeSession.stoodIn(throughUserPath) {
		test.Errorf("with nothing standing windows held user's entry %t and marked it stood %t, want held and not "+
			"stood", wholeSession.caching.Store.Holds(wholeSession.keys[wholeUser]),
			wholeSession.stoodIn(throughUserPath))
	}
}

// A package restored from the host's run that something stood in, in any load, is never written through (#e4rd5dw),
// as it is never saved as a miss. With user marked stood before windows's first load, windows restores both packages
// from the host and writes base's entry through, never user's, which its store does not hold.
func TestAPackageSomethingStoodInIsNeverWrittenThrough(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	stores := test.TempDir()
	host := throughHost(test, root, filepath.Join(stores, "host"))
	result, session := standingBuild(test, root, windowsCachingAt(filepath.Join(stores, "build"), throughRules()),
		host, nil, func(session *CacheSession) {
			session.markStood(throughUserPath)
		})

	for _, path := range []string{throughBasePath, throughUserPath} {
		if !slices.Contains(result.FromHost.Restored, path) {
			test.Fatalf("windows restored %q from the host, want %s among them", result.FromHost.Restored, path)
		}
	}
	base, user := throughPackage(test, session, throughBasePath), throughPackage(test, session, throughUserPath)
	wantThrough := len(result.FromHost.Restored) + len(result.FromHost.Untouched) - 1
	if result.Cache.WrittenThrough != wantThrough || !session.caching.Store.Holds(session.keys[base]) ||
		session.caching.Store.Holds(session.keys[user]) {
		test.Errorf("windows wrote %d entries through, base's held %t and user's %t, want %d, base's and not user's",
			result.Cache.WrittenThrough, session.caching.Store.Holds(session.keys[base]),
			session.caching.Store.Holds(session.keys[user]), wantThrough)
	}
}
