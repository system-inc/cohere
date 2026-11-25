package check

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/vet/printf"
)

// wrapperFiles are graphFiles' shape, base under in-package tests and user restored from the host's run, where base's
// Logf forwards its format to fmt only on windows: the host's printf learns no fact of it, and windows's would.
var wrapperFiles = map[string]string{
	"go.mod": "module example.com/hostgraph\n\ngo 1.24\n",
	"base/base.go": "//go:build !windows\n\n// Package base logs.\npackage base\n\n" +
		"// Logf hands back its format.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn format\n}\n",
	"base/base_windows.go": "// Package base logs.\npackage base\n\nimport \"fmt\"\n\n" +
		"// Logf formats as fmt.Sprintf does.\nfunc Logf(format string, arguments ...any) string {\n" +
		"\treturn fmt.Sprintf(format, arguments...)\n}\n",
	"base/base_test.go": graphFiles["base/base_test.go"],
	"user/user.go": "// Package user logs through base.\npackage user\n\nimport \"example.com/hostgraph/base\"\n\n" +
		"// Use hands Logf a format its argument does not fit.\nfunc Use() string {\n" +
		"\treturn base.Logf(\"%d\", \"text\")\n}\n",
}

// printfAlone is printf's port, the one rule wrapperFiles is run under.
func printfAlone() []rule.Rule {
	return []rule.Rule{{Name: rule.Namespace + "test-printf", Origin: rule.OriginVet, UpstreamName: "printf",
		Analyzer: printf.Analyzer}}
}

// The decide's last word on a fact read (buildFacts.factsHold): a restore that read a fact of an owner the build checks
// from source, where the build ran no action of that fact's type, is declined, since the build has nothing to compare
// the read with (#xqbqcbt). Fact roots leave no such owner in a run; withholding plain base's makes one. The host's
// printf learned no fact of base's Logf, which forwards nothing there, and user read that it has none; windows's Logf
// forwards, so a check from source finds printf's finding in user. Were the empty read taken as equal to the build's
// empty facts, user would hold with the host's findings, which have none.
func TestARestoreReadingAnOwnerTheBuildRanNothingOnIsDeclined(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixtureOf(test, wrapperFiles, printfAlone())
	windows, _, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true, printfAlone(),
		func(options *Options) {
			options.withholdFactRoot = func(withheld *packages.Package) bool { return withheld.ID == graphBasePath }
		})
	if why := windows.FromHost.Declined[graphUserPath]; !strings.HasPrefix(why, "F the build has no") ||
		windows.FromHost.ReloadRounds == 0 {
		test.Errorf("windows declined user for %q over %d reloads, want the build's having no facts of base, and a "+
			"reload", why, windows.FromHost.ReloadRounds)
	}
	if !slices.ContainsFunc(windows.Findings, func(found Finding) bool {
		return strings.HasSuffix(found.File, filepath.Join("user", "user.go")) &&
			strings.Contains(found.Message, "Logf format %d")
	}) {
		test.Errorf("windows found %+v, want printf's finding of Logf in user, as its check from source finds it",
			windows.Findings)
	}
}

// lonelyFiles are graphFiles with lonely, which has in-package tests and nothing imports, so plain lonely is a package
// the load reaches that nothing given reaches.
var lonelyFiles = func() map[string]string {
	files := map[string]string{
		"lonely/lonely.go": "// Package lonely is imported by nothing.\npackage lonely\n\n" +
			"// Alone is one.\nfunc Alone() int {\n\treturn 1\n}\n",
		"lonely/lonely_test.go": "package lonely\n\nimport \"testing\"\n\n" +
			"func TestAlone(test *testing.T) {\n\tif Alone() != 1 {\n\t\ttest.Fatal(\"Alone\")\n\t}\n}\n",
	}
	for name, contents := range graphFiles {
		files[name] = contents
	}
	return files
}()

// A restore from the host's run is written through only when its entry holds exactly the analyzers the build runs on
// the package from source (expectedSlots), since the entry's output digest is over every one of them: an entry
// holding fewer would carry a digest the build never computes, as one holding more would. The set is every rule's,
// report's and fact analyzer's on a package given, the fact analyzers' on one the given reach, and none on one nothing
// given reaches, whose own entry holds no analysis.
func TestAWriteThroughTakesExactlyTheSlotsTheBuildWouldWrite(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixtureOf(test, lonelyFiles, throughRules())
	windows, session, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true, throughRules(), nil)
	user := throughPackage(test, session, graphUserPath)
	hosted := session.fromHost(user)
	if hosted == nil {
		test.Fatalf("windows restored %q from the host, want %s", windows.FromHost.Restored, graphUserPath)
	}
	reached := session.givenReach()
	slotsOf := func(analyzers ...[]*analysis.Analyzer) map[string]bool {
		slots := map[string]bool{}
		for _, each := range slices.Concat(analyzers...) {
			if slot, held := session.known.slot(each); held {
				slots[slot] = true
			}
		}
		return slots
	}
	lonely := keyedByIdentifier(test, session, "example.com/hostgraph/lonely")
	for _, each := range []struct {
		owner *packages.Package
		want  map[string]bool
	}{
		{owner: user, want: slotsOf(session.known.rules, session.known.facts)},
		{owner: keyedByIdentifier(test, session, graphBasePath), want: slotsOf(session.known.facts)},
		{owner: lonely, want: map[string]bool{}},
	} {
		if got := session.expectedSlots(each.owner, reached); !reflect.DeepEqual(got, each.want) {
			test.Errorf("windows expects %v of %s's entry, want %v", got, each.owner.ID, each.want)
		}
	}
	held := hosted.restored.slots
	for _, each := range []struct {
		name     string
		slots    []string
		writable bool
	}{
		{name: "the host's own", slots: held, writable: true},
		{name: "one fewer", slots: held[1:]},
		{name: "one more", slots: append(slices.Clone(held), "another analyzer")},
	} {
		changed := *hosted.restored
		changed.slots = each.slots
		_, writable, writeError := session.throughEntry(user, &hostRestore{kept: hosted.kept, restored: &changed},
			session.allFiles(), false, reached)
		if writeError != nil || writable != each.writable {
			test.Errorf("windows's entry of user holding %s slots: writable %t (%v), want %t", each.name, writable,
				writeError, each.writable)
		}
	}
}

// failingLearner learns facts and fails on a package that declares Windows, as plain base does on windows, but not on
// one with test files, so its action fails on plain base alone, a dependency no finding is gathered from, and never
// on "base [base.test]", whose crash would show.
var failingLearner = &analysis.Analyzer{
	Name:      "failing_learner",
	Doc:       "failing_learner learns no fact, and fails on a package declaring Windows that has no test file",
	FactTypes: []analysis.Fact{new(learnerFact)},
	Run: func(pass *analysis.Pass) (any, error) {
		if pass.Pkg.Scope().Lookup("Windows") == nil {
			return nil, nil
		}
		for _, file := range pass.Files {
			if strings.HasSuffix(pass.Fset.File(file.Pos()).Name(), "_test.go") {
				return nil, nil
			}
		}
		return nil, errors.New("failing_learner fails on windows's plain base")
	},
}

// A restore from the host's run above an action of an analyzer that learns facts that failed in the run is held, its
// findings the entry's, as main held it, but is never written through to the build's store (#xqbqcbt). The failed
// action is failing_learner's on plain base, which user reads nothing of, so the decide holds user. A check of user
// from source would fail failing_learner's rule there with it, dropping the rule's findings on user and making the run
// incomplete; an entry written through would hide that crash on every later run that hits it, where main, refusing the
// write once user's restored actions failed with their prerequisite, had the next run check user from source and show
// it.
func TestAJudgedRestoreAboveAFailedFactActionIsNotWrittenThrough(test *testing.T) {
	test.Parallel()
	rules := append(throughRules(), houseRule(failingLearner))
	root, stores, host := graphFixtureOf(test, graphFiles, rules)
	windows, session, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true, rules, nil)
	if !slices.Contains(windows.FromHost.Restored, graphUserPath) || windows.FromHost.DeclinedAtCheck != 0 {
		test.Fatalf("windows restored %q and declined %v, want user restored and nothing declined",
			windows.FromHost.Restored, windows.FromHost.Declined)
	}
	if !slices.ContainsFunc(windows.Findings, func(found Finding) bool {
		return strings.HasSuffix(found.File, filepath.Join("user", "user.go")) &&
			strings.Contains(found.Message, "Logf format %d")
	}) {
		test.Errorf("windows found %+v, want printf's finding in user, from its entry", windows.Findings)
	}
	if windows.Cache.WrittenThrough == 0 {
		test.Fatal("windows wrote nothing through, so user's refusal tells nothing")
	}
	user := throughPackage(test, session, graphUserPath)
	if _, written := session.caching.Store.Get(session.keys[user]); written {
		test.Errorf("windows wrote user through above failing_learner's failed action on plain base")
	}
}

// keyedByIdentifier is the package of session's last load whose ID is identifier, plain p rather than a test variant
// of it, failing the test when the load keyed none.
func keyedByIdentifier(test *testing.T, session *CacheSession, identifier string) *packages.Package {
	test.Helper()
	for keyed := range session.keys {
		if keyed.ID == identifier {
			return keyed
		}
	}
	test.Fatalf("the build keyed no package %s", identifier)
	return nil
}

// firstWaveFiles are a module whose base imports nothing, has in-package tests and a file only windows compiles, and
// declares Fail, which never returns; user, restored from the host's run, calls Fail, reading ctrlflow's fact of it;
// and checked, which windows checks from source by a file of its own, imports base.
var firstWaveFiles = map[string]string{
	"go.mod": "module example.com/hostgraph\n\ngo 1.24\n",
	"base/base.go": "// Package base fails.\npackage base\n\n" +
		"// Fail never returns.\nfunc Fail() {\n\tpanic(\"fail\")\n}\n",
	"base/base_windows.go": graphFiles["base/base_windows.go"],
	"base/base_test.go":    "package base\n\nimport \"testing\"\n\nfunc TestNothing(test *testing.T) {}\n",
	"user/user.go": "// Package user fails through base.\npackage user\n\nimport \"example.com/hostgraph/base\"\n\n" +
		"// Use fails.\nfunc Use() {\n\tbase.Fail()\n}\n",
	"checked/checked.go": "// Package checked fails through base.\npackage checked\n\n" +
		"import \"example.com/hostgraph/base\"\n\n// Check fails.\nfunc Check() {\n\tbase.Fail()\n}\n",
	"checked/checked_windows.go": "package checked\n\n// Windows says the build is windows's.\nconst Windows = true\n",
}

// A package the build's first wave analyzes and saves, plain base, a dependency a miss imports with no miss below it,
// is restored in the second wave (CacheSession.earlier), where checked's fact actions reach it, so its facts are the
// build's to read (buildFacts reads them off those restored actions, since it is no hit), and user, restored from the
// host's run, holds on the fact of Fail it read.
func TestAFirstWavePackageGivesItsFactsThroughTheSecondWave(test *testing.T) {
	test.Parallel()
	root, stores, host := graphFixtureOf(test, firstWaveFiles, throughRules())
	var analyzed []*checker.Graph
	windows, session, _ := graphRun(test, root, filepath.Join(stores, "windows"), host, true, throughRules(),
		collectingGraphs(func(graphs []*checker.Graph) { analyzed = graphs }))
	if !slices.Contains(windows.FromHost.Restored, graphUserPath) || windows.FromHost.DeclinedAtCheck != 0 ||
		windows.FromHost.ReloadRounds != 0 {
		test.Errorf("windows restored %q, declined %v over %d reloads, want user restored, nothing declined, no "+
			"reload", windows.FromHost.Restored, windows.FromHost.Declined, windows.FromHost.ReloadRounds)
	}
	base := keyedByIdentifier(test, session, graphBasePath)
	if session.earlier[base] == nil || windows.Cache.Waves != 2 {
		test.Fatalf("windows's first wave saved base %t over %d waves, want saved over two",
			session.earlier[base] != nil, windows.Cache.Waves)
	}
	exported := newBuildFacts(session, analyzed, nil).of(base)
	noReturns := 0
	for factType, facts := range exported.facts {
		if strings.Contains(factType.String(), "noReturn") && exported.available[factType] {
			noReturns += len(facts)
		}
	}
	if noReturns == 0 {
		test.Errorf("the build's facts of base hold %d of ctrlflow's, want Fail's", noReturns)
	}
}
