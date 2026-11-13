package check

import (
	"go/ast"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// sizeProbe reports each `unsafe.Sizeof` call with the size the type checker gave it, which the load's sizes decide.
var sizeProbe = &analysis.Analyzer{
	Name: "sizeProbe",
	Doc:  "reports each unsafe.Sizeof call with the size the type checker gave it",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				selector, isSelector := call.Fun.(*ast.SelectorExpr)
				if value := pass.TypesInfo.Types[call].Value; isSelector && selector.Sel.Name == "Sizeof" &&
					value != nil {
					pass.Reportf(call.Pos(), "size %s", value)
				}
				return true
			})
		}
		return nil, nil
	},
}

// sizesFiles are a module of two packages that measure a pair of words: sized, which imports nothing, so its closure
// misses whatever differs between two builds and the walk leaves it untouched, and user, which reads os, whose files
// differ by build, so the walk visits it and its reads are judged.
var sizesFiles = map[string]string{
	"go.mod": "module example.com/sizes\n\ngo 1.24\n",
	"sized/sized.go": "// Package sized measures a pair of words.\npackage sized\n\nimport \"unsafe\"\n\n" +
		"// Pair is two words.\ntype Pair struct {\n\tFirst  uintptr\n\tSecond uintptr\n}\n\n" +
		"// Size is a Pair's size.\nconst Size = unsafe.Sizeof(Pair{})\n",
	"user/user.go": "// Package user measures a pair of its own beside the arguments.\npackage user\n\n" +
		"import (\n\t\"os\"\n\t\"unsafe\"\n)\n\n" +
		"// Measured is a pair's size and the arguments' count.\nfunc Measured() uintptr {\n" +
		"\treturn unsafe.Sizeof([2]uintptr{}) + uintptr(len(os.Args))\n}\n",
}

// sizesRun is one build's run over root under environment, with its store in storeDirectory: the host's, keeping its
// packages for the builds once every read of them is whole, when host is nil, and otherwise a build restoring from
// host, loaded again until no reload is asked.
func sizesRun(test *testing.T, root string, storeDirectory string, environment []string, host *HostRun) *Result {
	test.Helper()
	rules := []rule.Rule{houseRule(sizeProbe)}
	caching := cachingAt(storeDirectory, rules)
	// Each build's keys hold its target, as the engine's do, so they never meet another's.
	caching.Context.Build = append(caching.Context.Build, environment...)
	session := NewCacheSession(caching, rules)
	if host == nil {
		session.KeepForBuilds()
	} else {
		session.RestoreFromHost(host)
	}
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, append([]string{"GOWORK=off"},
			environment...), nil, load.Caching{Restorer: session, RecordReads: host == nil})
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading the build under %q: %v %+v", environment, loadError, loaded.Diagnostics)
		}
		result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
			Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots, KeepForBuilds: host == nil})
		if runError != nil || len(result.Crashes) > 0 {
			test.Fatalf("running the build under %q: %v %+v", environment, runError, result.Crashes)
		}
		if host == nil {
			// The builds judge the host's reads, which are whole only once their walks end.
			result.ReadWalks.Wait()
			return result
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			return result
		}
	}
}

// sizesFound are the messages of the findings of result, sorted.
func sizesFound(result *Result) []string {
	var messages []string
	for _, finding := range result.Findings {
		messages = append(messages, filepath.Base(finding.File)+" "+finding.Message)
	}
	slices.Sort(messages)
	return messages
}

// A build measured by other sizes than the host's restores nothing from the host's run, untouched or held (#61qj12v):
// the host checks both packages under its 64-bit sizes, where a pair of words is 16 bytes, and a linux/386 build,
// where it is 8, declines each by its sizes and finds 8 in both, which restoring either would have made 16. sized's
// closure misses everything 386 changes and user's reads of os hold there, so neither is declined by its reads.
func TestABuildUnderOtherSizesRestoresNothingFromTheHost(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, sizesFiles)
	stores := test.TempDir()
	hostResult := sizesRun(test, root, filepath.Join(stores, "host"), nil, nil)
	if found := sizesFound(hostResult); !slices.Equal(found, []string{"sized.go size 16", "user.go size 16"}) {
		test.Fatalf("the host found %q, want a pair of words of 16 bytes in each package", found)
	}
	if hostResult.Host == nil || hostResult.Host.EntriesKept() == 0 {
		test.Fatal("the host kept nothing for the builds, so the build could restore nothing either way")
	}
	built := sizesRun(test, root, filepath.Join(stores, "build"),
		[]string{"GOOS=linux", "GOARCH=386", "CGO_ENABLED=0"}, hostResult.Host)
	if found := sizesFound(built); !slices.Equal(found, []string{"sized.go size 8", "user.go size 8"}) {
		test.Errorf("386 found %q, want a pair of words of 8 bytes in each package", found)
	}
	fromHost := built.FromHost
	if len(fromHost.Restored) != 0 || len(fromHost.Untouched) != 0 || fromHost.DeclinedBy["buildSizes"] < 2 {
		test.Errorf("386 restored %q and left %q untouched, declining %v, want nothing restored and both declined "+
			"by its sizes", fromHost.Restored, fromHost.Untouched, fromHost.DeclinedBy)
	}
}
