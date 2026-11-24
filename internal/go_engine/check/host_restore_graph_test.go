package check

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The graph fixture's packages: base, which windows checks from source, and user, which it restores from the host's
// run.
const (
	graphBasePath = "example.com/hostgraph/base"
	graphUserPath = "example.com/hostgraph/user"
)

// graphFiles are a module whose base has in-package tests, so the load gives "base [base.test]" to analyze in place of
// base, which leaves plain base a dependency only user imports, and a file only windows compiles, so windows checks it
// from source. user reads printf's fact of base's wrapper Logf, and windows restores it from the host's run, its own
// inputs and every read alike there.
var graphFiles = map[string]string{
	"go.mod": "module example.com/hostgraph\n\ngo 1.24\n",
	"base/base.go": "// Package base logs.\npackage base\n\nimport \"fmt\"\n\n" +
		"// Logf formats as fmt.Sprintf does.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn fmt.Sprintf(format, arguments...)\n}\n",
	"base/base_windows.go": "package base\n\n// Windows says the build is windows's.\nconst Windows = true\n",
	"base/base_test.go": "package base\n\nimport \"testing\"\n\n" +
		"func TestLogf(test *testing.T) {\n\tif Logf(\"%s\", \"text\") != \"text\" {\n\t\ttest.Fatal(\"Logf\")\n\t}\n}\n",
	"user/user.go": "// Package user logs through base.\npackage user\n\nimport \"example.com/hostgraph/base\"\n\n" +
		"// Use hands Logf a format its argument does not fit.\nfunc Use() string {\n" +
		"\treturn base.Logf(\"%d\", \"text\")\n}\n",
}

// filesReportAnalyzer is a report (Options.Report) of how many files each package's pass was given, whose result the
// findings cache keeps with the package.
var filesReportAnalyzer = &analysis.Analyzer{
	Name:       "files_report",
	Doc:        "files_report reports how many files each package's pass was given",
	ResultType: reflect.TypeFor[*filesReport](),
	Run: func(pass *analysis.Pass) (any, error) {
		return &filesReport{Path: pass.Pkg.Path(), Files: len(pass.Files)}, nil
	},
}

// graphRun is one run of graphFiles: the host's, or windows's restoring from host when it is not nil, or from source
// when it is, with the files report beside the rules and every graph the run analyzed handed to graphs, loaded again
// as the engine loads an extra build, until no reload is asked. It returns the result of the last load's run, its
// session and its packages to analyze.
func graphRun(
	test *testing.T,
	root string,
	storeDirectory string,
	host *HostRun,
	windows bool,
	graphs func(analyzed []*checker.Graph),
) (*Result, *CacheSession, []*packages.Package) {
	test.Helper()
	rules := throughRules()
	caching := cachingAt(storeDirectory, rules)
	environment := []string{"GOWORK=off"}
	if windows {
		caching = windowsCachingAt(storeDirectory, rules)
		environment = append(environment, "GOOS=windows")
	}
	session := NewCacheSession(caching, rules, filesReportAnalyzer)
	loadCaching := load.Caching{Restorer: session}
	if windows {
		session.RestoreFromHost(host)
	} else {
		session.KeepForBuilds()
		loadCaching.RecordReads = load.EveryPackage
	}
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, environment, nil, loadCaching)
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading windows %t: %v %+v", windows, loadError, loaded.Diagnostics)
		}
		options := Options{Cache: session, Withheld: loaded.Withheld, Bodiless: loaded.Bodiless,
			Report: filesReportAnalyzer, analyzedGraphs: graphs}
		if !windows {
			options.Reads, options.WalkSlots, options.KeepForBuilds = loaded.Reads, loaded.WalkSlots, true
		}
		result, runError := Run(loaded.Packages, rules, options)
		if runError != nil {
			test.Fatalf("running windows %t: %v", windows, runError)
		}
		if !windows {
			// The builds read the host's reads whole and its dependence sets in, as the engine has them.
			result.ReadWalks.Wait()
			result.Host.DependenceComputed()
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			if len(result.Crashes) > 0 {
				test.Fatalf("windows %t crashed: %+v", windows, result.Crashes)
			}
			return result, session, loaded.Packages
		}
	}
}

// graphFixture is graphFiles written to a new root, with the host's run over it and a directory for the builds'
// stores.
func graphFixture(test *testing.T) (root string, stores string, host *HostRun) {
	test.Helper()
	root = test.TempDir()
	writeFiles(test, root, graphFiles)
	stores = test.TempDir()
	hostResult, _, _ := graphRun(test, root, filepath.Join(stores, "host"), nil, false, nil)
	return root, stores, hostResult.Host
}

// A package windows checks from source that nothing it was given reaches, plain base beside the "base [base.test]" it
// analyzes, still has the analyzers that learn facts run on it (checker.Options.FactRoots), so windows judges user,
// restored from the host's run, by the facts base exports there (#xqbqcbt). With no host restore rooted, only those
// fact roots reach plain base: without them the decide has no printf fact of base to ask, declines user, and loads
// windows again to check it from source.
func TestAFromSourcePackageNothingGivenReachesStillHasItsFacts(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixture(test)
	var factRooted []string
	windows, _, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true,
		func(graphs []*checker.Graph) {
			for _, graph := range graphs {
				for _, action := range graph.FactRoots {
					factRooted = append(factRooted, action.Package.ID)
				}
			}
		})
	if !slices.Contains(factRooted, graphBasePath) {
		test.Errorf("windows rooted the analyzers that learn facts on %q, want plain %s among them", factRooted,
			graphBasePath)
	}
	if !slices.Contains(windows.FromHost.Restored, graphUserPath) || windows.FromHost.DeclinedAtCheck != 0 ||
		windows.FromHost.ReloadRounds != 0 {
		test.Errorf("windows restored %q from the host, declined %d at check (%v) over %d reloads, want %s "+
			"restored, none declined and no reload", windows.FromHost.Restored, windows.FromHost.DeclinedAtCheck,
			windows.FromHost.Declined, windows.FromHost.ReloadRounds, graphUserPath)
	}
}

// A package windows restores from the host's run gives its findings, its report and its files' walk from its entry,
// since windows makes no action on it to give them: windows restoring user reads as windows checking every package
// from source, the same findings, reports and walk, with user's report, the one package to analyze it restored, read
// from its entry, as main's gather read it.
func TestAHostRestoresFindingsReportAndFilesComeFromItsEntry(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixture(test)
	restoring, _, analyzed := graphRun(test, root, filepath.Join(stores, "restoring"), host, true, nil)
	fromSource, _, _ := graphRun(test, root, filepath.Join(stores, "source"), nil, true, nil)
	if !slices.Contains(restoring.FromHost.Restored, graphUserPath) {
		test.Fatalf("windows restored %q from the host, want %s", restoring.FromHost.Restored, graphUserPath)
	}
	if len(restoring.Findings) == 0 || !reflect.DeepEqual(restoring.Findings, fromSource.Findings) {
		test.Errorf("windows restoring found %+v, from source %+v, want the same, and printf's finding in user",
			restoring.Findings, fromSource.Findings)
	}
	if restoring.FilesWalked != fromSource.FilesWalked || restoring.NodesVisited != fromSource.NodesVisited {
		test.Errorf("windows restoring walked %d files and %d nodes, from source %d and %d, want the same",
			restoring.FilesWalked, restoring.NodesVisited, fromSource.FilesWalked, fromSource.NodesVisited)
	}
	reportsByIdentifier := func(result *Result) map[string]filesReport {
		reports := map[string]filesReport{}
		for analyzedPackage, report := range result.Reports {
			counted, isCounted := report.(*filesReport)
			if !isCounted {
				test.Fatalf("the report on %s is a %T", analyzedPackage.ID, report)
			}
			reports[analyzedPackage.ID] = *counted
		}
		return reports
	}
	if restored, checked := reportsByIdentifier(restoring), reportsByIdentifier(fromSource); len(restored) !=
		len(analyzed) || !reflect.DeepEqual(restored, checked) || len(restoring.ReportCrashes) > 0 {
		test.Errorf("windows restoring reported %+v (crashes %+v), from source %+v, want one for each of the %d "+
			"packages to analyze, the same", restored, restoring.ReportCrashes, checked, len(analyzed))
	}
	if restoring.ReportsRestored != 1 || fromSource.ReportsRestored != 0 {
		test.Errorf("windows restoring read %d reports from entries and from source %d, want user's alone and none",
			restoring.ReportsRestored, fromSource.ReportsRestored)
	}
}

// An extra build roots nothing on a package it restores from the host's run (#xqbqcbt): every rule's root on user,
// and the chain of fact analyzers they seeded over what it imports, ran actions nothing read. user is still restored,
// and reached, restored, only where a pass above it would inherit its facts, which nothing here does.
func TestAnExtraBuildRootsNothingOnAHostRestore(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixture(test)
	rootedOnRestores := 0
	actionsOnUser := 0
	windows, session, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true,
		func(graphs []*checker.Graph) {
			for _, graph := range graphs {
				for _, action := range graph.Roots {
					if action.Package.PkgPath == graphUserPath {
						rootedOnRestores++
					}
				}
				for action := range graph.All() {
					if action.Package.PkgPath == graphUserPath {
						actionsOnUser++
					}
				}
			}
		})
	if !slices.Contains(windows.FromHost.Restored, graphUserPath) {
		test.Fatalf("windows restored %q from the host, want %s", windows.FromHost.Restored, graphUserPath)
	}
	if user := throughPackage(test, session, graphUserPath); session.fromHost(user) == nil {
		test.Fatalf("windows's %s is no restore from the host", graphUserPath)
	}
	if rootedOnRestores != 0 || actionsOnUser != 0 {
		test.Errorf("windows rooted %d actions on %s, restored from the host, and made %d on it, want none",
			rootedOnRestores, graphUserPath, actionsOnUser)
	}
}

// A run restoring from the host's run is refused beside its load and under the facts census, each of which would read
// a host restore off an action it no longer has (restoringFromHostRefuses), and a run restoring from no host's takes
// either.
func TestARunRestoringFromTheHostRefusesTheLoadOverlapAndTheCensus(test *testing.T) {
	test.Parallel()
	ready := func(*packages.Package) <-chan struct{} { return nil }
	restoring := NewCacheSession(&Caching{}, nil)
	restoring.RestoreFromHost(&HostRun{})
	plain := NewCacheSession(&Caching{}, nil)
	for _, each := range []struct {
		name    string
		options Options
		refused bool
	}{
		{name: "beside the load, restoring", options: Options{Cache: restoring, Ready: ready}, refused: true},
		{name: "the census, restoring", options: Options{Cache: restoring, ExportFacts: true}, refused: true},
		{name: "restoring alone", options: Options{Cache: restoring}},
		{name: "beside the load, restoring nothing", options: Options{Cache: plain, Ready: ready}},
		{name: "the census, restoring nothing", options: Options{Cache: plain, ExportFacts: true}},
	} {
		if refusal := restoringFromHostRefuses(each.options); (refusal != nil) != each.refused {
			test.Errorf("%s: refused with %v, want refused %t", each.name, refusal, each.refused)
		}
	}
}
