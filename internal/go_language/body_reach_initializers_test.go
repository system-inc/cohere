package go_language

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Go lowers a package's variable initializers into one synthetic function, in the order `types.Info.InitOrder` gives,
// which among initializers that depend on none other is the order of the files as the load lists them, so an
// initializer that never returns leaves every one after it unreachable, and a rule reading the representation reports
// nothing there (#bed0mnk). The body-reach-initializers fixture's shared file holds three initializers staticcheck
// reports, SA1007, SA1000 and SA1002, none of which uses anything a build changes. Each case writes a platform file
// whose `var first = stop()` never returns on one platform and returns on the other: on windows, then on the host's
// platform, and sorted before the shared file, then after it. Sorted before, the shared initializers' findings are the
// other platform's alone, so the reach must not leave them unreached: it treats the package's initializers as one body,
// every one reached where any sits in a file the builds do not share. Sorted after, the findings are alike in both
// builds, and the initializers are reached all the same, the rule reading no order. Two cases more hold the stopping
// initializer on one platform alone, the other's file declaring none, so each cause stands by itself: a build-only
// file's initializer, and a host's file's the build does not compile. The last moves `var first = stop()` into a shared
// file of its own, each platform's file declaring stop alone: first is reached by stop's name, and so every initializer
// with it.
func TestTheReachTreatsAPackagesVariableInitializersAsOneBody(test *testing.T) {
	test.Parallel()

	// stopping never returns, and returning does, each declaring first by the call.
	const stopping = `
package initializers

// first never returns here, so every initializer after it is unreachable.
var first = stop()

// stop never returns.
func stop() int {
	panic("stop")
}
`
	const returning = `
package initializers

// first returns here, so every initializer after it runs.
var first = stop()

// stop returns.
func stop() int {
	return 0
}
`
	// bare declares no initializer, its platform's builds holding the shared file's alone.
	const bare = `
package initializers
`
	// calling is a shared file's initializer calling stop, which stopper and returner declare, each on its platform.
	const calling = `package initializers

// first calls stop, which each platform's file declares.
var first = stop()
`
	const stopper = `
package initializers

// stop never returns.
func stop() int {
	panic("stop")
}
`
	const returner = `
package initializers

// stop returns.
func stop() int {
	return 0
}
`
	for _, each := range []struct {
		name string
		// stem names the platform files, sorting before the shared file or after it.
		stem string
		// windows and host are windows's file and the host's platform's, which builds everywhere but windows.
		windows string
		host    string
		// hostStops is whether the host's file never returns, so the host reports none of the three sorted before.
		hostStops bool
		// shared is a shared file's, named by stem alone, empty for none.
		shared string
	}{
		{"stopping on windows, sorted before", "early", stopping, returning, false, ""},
		{"stopping on windows, sorted after", "trailing", stopping, returning, false, ""},
		{"stopping on the host, sorted before", "early", returning, stopping, true, ""},
		{"stopping on the host, sorted after", "trailing", returning, stopping, true, ""},
		{"stopping on windows alone, sorted before", "early", stopping, bare, false, ""},
		{"stopping on the host alone, sorted before", "early", bare, stopping, true, ""},
		{"stopping by a shared initializer, sorted before", "early", stopper, returner, false, calling},
	} {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			// The reach and its census judge what each build checks from source, the standard library's packages
			// among them, so every run computes them rather than restore them from the template.
			coldSystemCacheFor(test)

			root := copyFixture(test, filepath.Join("testdata", "body-reach-initializers"))
			for name, contents := range map[string]string{
				each.stem + "_windows.go": strings.TrimPrefix(each.windows, "\n"),
				each.stem + "_other.go":   "//go:build !windows\n" + each.host,
				each.stem + ".go":         each.shared,
			} {
				if contents == "" {
					continue
				}
				if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
					test.Fatal(writeError)
				}
			}
			on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
				engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				engine.bodyDifferential = true
			}, "--no-cache", "--no-fix", "--build-dependence")

			if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
				test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
			}
			census, differential := on.buildRuns[1].bodyReach, on.buildRuns[1].bodyDifferential
			if census == nil || differential == nil {
				test.Fatal("windows's run has no census or no body differential")
			}
			// The fixture's premise: the host reports the three where its own initializer returns, sorted before, and
			// always sorted after. The shared file's other findings, U1000's and the dropped errors', read alike.
			sharedFile := filepath.Join(root, "shared.go")
			var hostRules []string
			for _, finding := range on.bodyReachHost.findings["example.com/initializers"][sharedFile] {
				if rule := strings.TrimPrefix(finding.Rule, "cohere-go/"); strings.HasPrefix(rule, "SA") {
					hostRules = append(hostRules, rule)
				}
			}
			slices.Sort(hostRules)
			wantHostRules := []string{"SA1000", "SA1002", "SA1007"}
			if each.hostStops && each.stem == "early" {
				wantHostRules = nil
			}
			if !slices.Equal(hostRules, wantHostRules) {
				test.Errorf("the host reports %v in the shared file, want %v", hostRules, wantHostRules)
			}

			reasons := map[string]string{}
			for _, body := range census.bodies {
				if body.packageIdentifier == "example.com/initializers" && body.file == sharedFile {
					reasons[body.name] = string(body.reason)
				}
			}
			for _, name := range []string{"parsed", "pattern", "when"} {
				if reasons[name] != string(reachedInitializers) {
					test.Errorf("the shared initializer %s is reached by %q, want %s, of %v", name, reasons[name],
						reachedInitializers, reasons)
				}
			}
			var failures []string
			for _, failure := range differential.failures {
				failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
			}
			if len(failures) > 0 {
				test.Errorf("want the body differential to read no failure, got\n%s", strings.Join(failures, "\n"))
			}
		})
	}
}
