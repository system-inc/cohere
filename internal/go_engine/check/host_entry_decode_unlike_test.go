package check

import (
	"maps"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/vet/printf"
)

// decodesAlike holds every difference an entry's decode reads as unlike: a slot moved, a report analyzer one side has
// and the other lacks, a fact type known by another name. Each copy differs from the host's analyzers in that one way
// alone, so each case fails on its own branch (#y8cf1wf).
func TestDecodesAlikeRefusesEachDifferenceAnEntrysDecodeReads(test *testing.T) {
	test.Parallel()

	host := NewCacheSession(cachingAt(test.TempDir(), throughRules()), throughRules()).known
	if !decodesAlike(host, host) {
		test.Fatal("the host's analyzers decode unlike themselves, so no case below could tell anything")
	}
	moved := *host
	moved.slots = maps.Clone(host.slots)
	moved.slots[printf.Analyzer] += "-moved"
	reported := *host
	reported.reports = maps.Clone(host.reports)
	reported.reports[&analysis.Analyzer{Name: "report_only_one_side_has"}] = true
	renamed := *host
	renamed.factTypes = maps.Clone(host.factTypes)
	printfTypes := maps.Clone(host.factTypes[printf.Analyzer])
	if len(printfTypes) == 0 {
		test.Fatal("printf learns no fact type here, so the renamed case would rename nothing")
	}
	for name, factType := range host.factTypes[printf.Analyzer] {
		delete(printfTypes, name)
		printfTypes[name+"-renamed"] = factType
	}
	renamed.factTypes[printf.Analyzer] = printfTypes
	for label, unlike := range map[string]*cacheAnalyzers{"a slot moved": &moved, "a report absent": &reported,
		"a fact type renamed": &renamed} {
		if decodesAlike(host, unlike) || decodesAlike(unlike, host) {
			test.Errorf("%s: decodes alike with the host's, want unlike", label)
		}
	}
}

// A build whose analyzers decode an entry unlike the host's decodes each entry it considers itself: the host's shared
// decode is never handed to it, so a fact can never land under an analyzer another slot names. The entry's decodes
// count, the shared decode's, stays at zero though the build restored both packages (#y8cf1wf).
func TestABuildWhoseAnalyzersDifferDecodesEachEntryItself(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, throughFiles)
	host := throughHost(test, root, filepath.Join(test.TempDir(), "host"))
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.Context.Build = append(caching.Context.Build, "GOOS=windows")
	session := NewCacheSession(caching, rules)
	session.RestoreFromHost(host)
	// The one difference: a report analyzer the host's lack, which decodesAlike reads as unlike, and which decodes the
	// entries cleanly, since none holds its slot, so the build still restores what it considers.
	unlike := *session.known
	unlike.reports = maps.Clone(session.known.reports)
	unlike.reports[&analysis.Analyzer{Name: "report_the_host_lacks"}] = true
	session.known = &unlike
	loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"}, nil,
		load.Caching{Restorer: session})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading windows's build: %v %+v", loadError, loaded.Diagnostics)
	}
	considered := map[string]bool{}
	for candidate := range session.host.restoring {
		considered[candidate.PkgPath] = true
	}
	for candidate := range session.host.untouched {
		considered[candidate.PkgPath] = true
	}
	if !considered[throughBasePath] || !considered[throughUserPath] {
		test.Fatalf("the build restored %v from the host, want base and user, so their decodes are read below",
			considered)
	}
	for _, path := range []string{throughBasePath, throughUserPath} {
		if decodes := host.entries[path].decodes.Load(); decodes != 0 {
			test.Errorf("%s: a build whose analyzers differ took the shared decode (%d decodes), want its own",
				path, decodes)
		}
	}
}
