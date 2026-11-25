package go_language

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// bodyReachVariantsFixtures holds the reader's two modules of #wzre5eg's review (#bed0mnk), each a package audit whose
// in-package test file windows alone compiles, so windows checks audit's test variant, `audit [audit.test]`, which the
// host never builds, and that file declares a package-level true, so audit's Enabled is false there. Beside them is
// the hunter's module, variant-method-set, whose test file gives a type of the variant a method the plain package's
// lacks.
var bodyReachVariantsFixtures = filepath.Join("testdata", "body-reach-variants")

// An import may resolve, in a build, to a package of another ID than on the host, with every file alike (#bed0mnk, the
// reader's must 2 on #wzre5eg). In variant-through-dependency via folds audit's Enabled into a constant of its own,
// and go test builds via again for audit's tests, against audit's test variant. So the external test's import of via
// is `via` on the host and `via [audit.test]` on windows, while windows holds plain via and plain audit too: every ID
// the host holds the build holds, loaded alike, and no file of via differs. The reach reads an import by the ID it
// resolves to as well as by its path (differingImports), so the external test's package is reached whole, Imports,
// and its enabled, a bool that is true on the host and false on windows, is never compared. audit's and via's own
// constants, whose plain packages import alike in both, stay unreached and compare equal.
func TestAPackageWhoseImportResolvesToAnotherPackageIsReachedWhole(test *testing.T) {
	test.Parallel()

	const via = "example.com/audit/via"
	const rebuilt = "example.com/audit/via [example.com/audit.test]"
	const external = "example.com/audit_test [example.com/audit.test]"
	root := copyFixture(test, filepath.Join(bodyReachVariantsFixtures, "variant-through-dependency"))
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.bodyReachFromSourceRecord = test.TempDir()
		engine.bodyDifferential = true
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	windows := on.buildRuns[1]
	if windows.bodyReach == nil || windows.bodyDifferential == nil {
		test.Fatal("windows's run has no census or no body differential")
	}
	// The fixture's premise: the host holds via by its plain ID alone, and windows checks it plain and built again.
	if _, isHosted := on.dependence.paths[rebuilt]; isHosted || on.dependence.paths[via] == "" ||
		!slices.Contains(windows.fromSourceIdentifiers, via) ||
		!slices.Contains(windows.fromSourceIdentifiers, rebuilt) {
		test.Fatalf("want the host to hold via plain alone and windows to check it plain and built again, got the "+
			"host's %v and windows's %v", slices.Sorted(maps.Keys(on.dependence.paths)), windows.fromSourceIdentifiers)
	}
	got, constantsOnly, drivers := map[string]string{}, map[string]string{}, map[string]string{}
	for _, body := range windows.bodyReach.bodies {
		name := body.packageIdentifier + " " + body.name
		got[name] = cmp.Or(string(body.reason), "unreached")
		constantsOnly[name] = cmp.Or(string(body.constantsOnlyReason), "unreached")
		drivers[name] = body.driver
	}
	// Under the constants-only load mode rule too, whose count the census reads beside the general one's.
	want := map[string]string{"example.com/audit Enabled": "unreached", via + " Enabled": "unreached",
		external + " enabled": string(reachedImports)}
	if !maps.Equal(got, want) || !maps.Equal(constantsOnly, want) {
		test.Errorf("windows's census judges\n%v\nand under the constants-only rule\n%v\nwant by both\n%v", got,
			constantsOnly, want)
	}
	wantDriver := "the package's imports differing, by " + via + " resolving to " + via + " on the host and to " +
		rebuilt + " in the build"
	if drivers[external+" enabled"] != wantDriver {
		test.Errorf("enabled is reached first by %q, want %q", drivers[external+" enabled"], wantDriver)
	}
	var failures []string
	for _, failure := range windows.bodyDifferential.failures {
		failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
	}
	if len(failures) > 0 || windows.bodyDifferential.compared != 2 {
		test.Errorf("want the body differential to compare the two constants and read no failure, got %d compared "+
			"and\n%s", windows.bodyDifferential.compared, strings.Join(failures, "\n"))
	}
}

// A test variant may differ from its plain package by a type's method set, with no constant read anywhere (#bed0mnk,
// the hunter's promote-win). In variant-method-set p's W embeds T, and an in-package test file windows alone compiles
// declares a method on T, so W's method set holds the promoted M in `p [p.test]` and nothing in plain p. q imports p,
// so windows holds plain p by its ID beside the variant, and builds q again for p's tests, against the variant. The
// external test's F takes a p.W: its p is plain p on the host and the variant on windows, so the parameter's type is
// another type under the same name, though no file of the external test differs, every ID the host holds the build
// holds, and no bit of plain p's W is set. Both of the external test's imports resolve to other packages on windows
// (differingImports), so its package is reached whole, Imports, and F is never compared. Plain q's F, whose p is
// plain p in both builds, stays unreached and compares equal.
func TestABodyNamingATypeOfAPackageBuiltAgainForItsTestsIsReached(test *testing.T) {
	test.Parallel()

	const plain = "example.com/audit/p"
	const variant = "example.com/audit/p [example.com/audit/p.test]"
	const through = "example.com/audit/q"
	const rebuilt = "example.com/audit/q [example.com/audit/p.test]"
	const external = "example.com/audit/p_test [example.com/audit/p.test]"
	root := copyFixture(test, filepath.Join(bodyReachVariantsFixtures, "variant-method-set"))
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
		engine.bodyReachFromSourceRecord = test.TempDir()
		engine.bodyDifferential = true
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	windows := on.buildRuns[1]
	if windows.bodyReach == nil || windows.bodyDifferential == nil {
		test.Fatal("windows's run has no census or no body differential")
	}
	// The fixture's premise: the host holds p plain alone, and windows checks it plain and as its test variant.
	if _, isHosted := on.dependence.paths[variant]; isHosted || on.dependence.paths[plain] == "" ||
		!slices.Contains(windows.fromSourceIdentifiers, plain) ||
		!slices.Contains(windows.fromSourceIdentifiers, variant) {
		test.Fatalf("want the host to hold p plain alone and windows to check it plain and as its test variant, got "+
			"the host's %v and windows's %v", slices.Sorted(maps.Keys(on.dependence.paths)),
			windows.fromSourceIdentifiers)
	}
	got, constantsOnly, drivers := map[string]string{}, map[string]string{}, map[string]string{}
	for _, body := range windows.bodyReach.bodies {
		name := body.packageIdentifier + " " + body.name
		got[name] = cmp.Or(string(body.reason), "unreached")
		constantsOnly[name] = cmp.Or(string(body.constantsOnlyReason), "unreached")
		drivers[name] = body.driver
	}
	// Under the constants-only load mode rule too, whose count the census reads beside the general one's.
	want := map[string]string{through + " F": "unreached", external + " F": string(reachedImports)}
	if !maps.Equal(got, want) || !maps.Equal(constantsOnly, want) {
		test.Errorf("windows's census judges\n%v\nand under the constants-only rule\n%v\nwant by both\n%v", got,
			constantsOnly, want)
	}
	wantDriver := "the package's imports differing, by " + plain + " resolving to " + plain + " on the host and to " +
		variant + " in the build, " + through + " resolving to " + through + " on the host and to " + rebuilt +
		" in the build"
	if drivers[external+" F"] != wantDriver {
		test.Errorf("F is reached first by %q, want %q", drivers[external+" F"], wantDriver)
	}
	var failures []string
	for _, failure := range windows.bodyDifferential.failures {
		failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
	}
	if len(failures) > 0 || windows.bodyDifferential.compared != 1 {
		test.Errorf("want the body differential to compare q's F alone and read no failure, got %d compared and\n%s",
			windows.bodyDifferential.compared, strings.Join(failures, "\n"))
	}
}

// A body naming an object of a package the build holds nothing of by its ID is reached whatever the host's mode
// (#bed0mnk, the reader's must 1 on #wzre5eg). In direct-rule-variant the external test's half and enabled name
// audit's constants, and windows holds audit as its test variant alone. Three runs over one copy with the findings
// cache on and one system cache: the first fills the stores, the second restores every package, and before the third
// the external test's file alone is edited, so the host restores audit from its store, export data, and checks the
// external test from source. Read by its mode alone, a package the host restored and the build did not check from
// source agreed, and both bodies were left unreached, enabled a bool that is true on the host and false on windows.
// Both are reached in the third run as in the first. Either of two rules does it alone: the direct load mode rule,
// failing wide for a package the build lacks by its ID (bodyReacher.loadModeDiffers), which judges a body first and
// so names it here, and the external test's import of audit resolving to the variant on windows (differingImports).
func TestABodyNamingAPackageTheBuildLacksByItsIdentifierIsReachedOverAWarmCache(test *testing.T) {
	test.Parallel()

	const audit = "example.com/audit"
	const variant = "example.com/audit [example.com/audit.test]"
	const external = "example.com/audit_test [example.com/audit.test]"
	root := copyFixture(test, filepath.Join(bodyReachVariantsFixtures, "direct-rule-variant"))
	systemCache := test.TempDir()
	// run is one run over the copy, its findings cache on and in the copy's own store, with windows's build of it.
	run := func() (*pipeline, buildRun) {
		sharing := sharingSystemCache(systemCache)
		on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
			sharing(engine)
			engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
			engine.bodyReachFromSourceRecord = test.TempDir()
			engine.bodyDifferential = true
		}, "--no-fix", "--build-dependence")
		if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
			test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
		}
		return on, on.buildRuns[1]
	}
	// reasons are what windows's census gives the external test's bodies, by name.
	reasons := func(windows buildRun) map[string]string {
		got := map[string]string{}
		if windows.bodyReach == nil {
			return got
		}
		for _, body := range windows.bodyReach.bodies {
			if body.packageIdentifier == external {
				got[body.name] = cmp.Or(string(body.reason), "unreached")
			}
		}
		return got
	}

	// The first run, every store cold: the fixture's premise, the host checking audit plain from source and windows
	// its test variant, with no audit of the plain ID.
	first, firstWindows := run()
	if first.dependence.exportData[audit] || slices.Contains(firstWindows.fromSourceIdentifiers, audit) ||
		!slices.Contains(firstWindows.fromSourceIdentifiers, variant) {
		test.Fatalf("want the host to check audit from source and windows its test variant and no audit, got export "+
			"data %t on the host and windows's %v", first.dependence.exportData[audit],
			firstWindows.fromSourceIdentifiers)
	}
	reached := map[string]string{"half": string(reachedLoadMode), "enabled": string(reachedLoadMode)}
	if got := reasons(firstWindows); !maps.Equal(got, reached) {
		test.Errorf("the first run's census judges the external test's bodies %v, want %v", got, reached)
	}
	run()
	file := filepath.Join(root, "outside_test.go")
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	edited := append(contents, []byte("\n// Edited between the runs.\n")...)
	if writeError := os.WriteFile(file, edited, 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	// The third run: the host restores audit and checks the external test alone from source, as windows does.
	third, thirdWindows := run()
	if !third.dependence.exportData[audit] || third.dependence.exportData[external] ||
		!slices.Equal(thirdWindows.fromSourceIdentifiers, []string{external}) {
		test.Fatalf("want the host to restore audit and check the external test from source, and windows to check "+
			"the external test alone, got export data %t and %t on the host and windows's %v",
			third.dependence.exportData[audit], third.dependence.exportData[external],
			thirdWindows.fromSourceIdentifiers)
	}
	got := reasons(thirdWindows)
	test.Logf("the third run's census judges the external test's bodies %v", got)
	if len(got) != 2 || got["half"] == "unreached" || got["enabled"] == "unreached" {
		test.Errorf("the third run's census judges the external test's bodies %v, want half and enabled reached", got)
	}
	// With both reached the differential compares no body, which it says, and reads no body differing.
	if thirdWindows.bodyDifferential == nil {
		test.Fatal("windows's third run has no body differential")
	}
	for _, failure := range thirdWindows.bodyDifferential.failures {
		if !strings.HasPrefix(failure, "compared no body") {
			test.Errorf("the third run's body differential reads %s", strings.ReplaceAll(failure,
				root+string(filepath.Separator), ""))
		}
	}
}
