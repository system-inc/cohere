package go_language

import (
	"cmp"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rules"
)

// A universe name one side alone declares at package level reaches a shared body that uses it on either side, each by
// its own route (#b255cfy). In body-reach-universe-names, Caller calls copy beside Plain, which names none, and a file
// of one build alone declares a func copy: windows's in build, and the host's, every build's but windows's, in host.
// The host's record names the object the host's type check resolved copy to. Where the build alone declares copy, that
// is the universe's, which no bit says moved, so Shadowed reaches Caller by the build-only name. Where the host alone
// declares it, that is the host's own func copy, in a file windows does not compile, whose bits the graph sets, so
// Caller is reached Named by them. A name the host's files alone declare is never Shadowed's to reach: on the host a
// use of the universe's copy, or of a dot-imported name, means no package-level declaration of the name is there, so
// shadowed never meets one. Plain, which names no copy, is reached by the package's scope in both, ScopeName, since a
// rule asks the scope about copy at any body's place, S1018 before it offers copy() for a loop.
func TestAUniverseNameOneSideDeclaresReachesTheBodiesThatUseIt(test *testing.T) {
	test.Parallel()

	for side, want := range map[string]string{
		"build": "Shadowed universe.copy shadowed in the body",
		"host":  "Named",
	} {
		test.Run(side, func(test *testing.T) {
			test.Parallel()

			root := copyFixture(test, filepath.Join("testdata", "body-reach-universe-names", side))
			on, standardOutput, standardError, exitCode := runOverExiting(test, root, rules.All(),
				func(engine *pipeline) {
					engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				}, "--no-cache", "--no-fix", "--build-dependence")
			if exitCode == exitBroken || exitCode == exitRefused {
				test.Fatalf("the run exited %d:\n%s\n%s", exitCode, standardOutput, standardError)
			}
			if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
				test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
			}
			census := on.buildRuns[1].bodyReach
			if census == nil {
				test.Fatal("windows's run has no census")
			}
			got := map[string]string{}
			for _, body := range census.bodies {
				if body.packageIdentifier == "example.test/gate" {
					got[body.name] = cmp.Or(string(body.reason), "unreached") + " " + body.driver
				}
			}
			if !strings.HasPrefix(got["Caller"], want) || !strings.HasPrefix(got["Plain"], "ScopeName") {
				test.Errorf("windows's census judges %v, want Caller reached as %q and Plain by ScopeName", got, want)
			}
		})
	}
}
