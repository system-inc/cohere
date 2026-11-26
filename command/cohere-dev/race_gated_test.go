package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// concurrencyTestName is a test whose name says it runs code at once: side by side, as extra builds and graphs run, and
// beside the load, as the lint and a run's passes do, are cohere's words for it (#2xddz1z).
var concurrencyTestName = regexp.MustCompile(
	`^Test\w*(Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad)`)

// concurrencyTestFile is a test file whose name says its tests run code at once.
var concurrencyTestFile = regexp.MustCompile(`(_concurrency_test\.go|^parallel_\w*_test\.go|_parallel_test\.go)$`)

// Every package holding a test whose name or file says it runs code at once is on the -race list, so a new
// concurrency test is caught by the race detector at the gate from its first landing (#bnabymg), and every entry on
// the list is a package of the module that says why it is there. Read from cohere's own tree, from its source alone.
func TestEveryConcurrencyTestIsRaceGated(test *testing.T) {
	test.Parallel()
	root := filepath.Join("..", "..")
	gated := map[string]bool{}
	for _, entry := range raceGated {
		if strings.TrimSpace(entry.reason) == "" {
			test.Errorf("%s is on the -race list with no reason", entry.directory)
		}
		if information, statError := os.Stat(filepath.Join(root, entry.directory)); statError != nil ||
			!information.IsDir() {
			test.Errorf("%s is on the -race list and is no directory of the module (%v)", entry.directory, statError)
		}
		gated[entry.directory] = true
	}
	found := concurrencyTestDirectories(test, root)
	if len(found) == 0 {
		test.Fatal("no concurrency test was found in the module, so the list was checked against nothing")
	}
	for directory, why := range found {
		if !gated[directory] {
			test.Errorf("%s holds %s, and is not on the -race list in race_gated.go: add it, with why", directory, why)
		}
	}
}

// concurrencyTestDirectories is every directory under root, by its path from root, holding a test whose name or file
// says it runs code at once, with the first such test or file. It leaves out testdata, nested modules, the TypeScript
// checkout and hidden directories, none of which the gate tests as cohere's module.
func concurrencyTestDirectories(test *testing.T, root string) map[string]string {
	test.Helper()
	found := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (name == "testdata" || name == "TypeScript" || strings.HasPrefix(name, ".") ||
				strings.HasPrefix(name, "_") || fileExists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if _, seen := found[relative]; seen {
			return nil
		}
		if concurrencyTestFile.MatchString(name) {
			found[relative] = name
			return nil
		}
		tests, testsError := concurrencyTestsInFile(path)
		if testsError != nil {
			return testsError
		}
		if len(tests) > 0 {
			found[relative] = tests[0]
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return found
}

// concurrencyTestsInFile is every top-level test in the test file at path that the guard calls concurrent: each whose
// name says it runs code at once, and every one when the file's name says its tests do.
func concurrencyTestsInFile(path string) ([]string, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	wholeFile := concurrencyTestFile.MatchString(filepath.Base(path))
	tests := []string{}
	for _, declaration := range parsed.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
			continue
		}
		if wholeFile || concurrencyTestName.MatchString(function.Name.Name) {
			tests = append(tests, function.Name.Name)
		}
	}
	return tests, nil
}

// concurrencyTestsInDirectory is every test the guard calls concurrent in the test files of directory itself, its
// subdirectories left out, since go test runs a package by its directory alone.
func concurrencyTestsInDirectory(test *testing.T, directory string) []string {
	test.Helper()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		test.Fatal(readError)
	}
	tests := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		found, foundError := concurrencyTestsInFile(filepath.Join(directory, entry.Name()))
		if foundError != nil {
			test.Fatal(foundError)
		}
		tests = append(tests, found...)
	}
	return tests
}

// raceGatedEntry is the -race list's entry for directory, failing test when the list holds none.
func raceGatedEntry(test *testing.T, directory string) raceEntry {
	test.Helper()
	index := slices.IndexFunc(raceGated, func(entry raceEntry) bool { return entry.directory == directory })
	if index < 0 {
		test.Fatalf("%s is not on the -race list", directory)
	}
	return raceGated[index]
}

// raceRunRefusal is why the -race list can't carry run, a go test -run pattern, for its package's concurrency tests,
// empty when it can: a run the wire or the shell can't carry, one that isn't a regular expression, or one that leaves
// out any of tests, matched as go test matches a top-level test's name.
func raceRunRefusal(run string, tests []string) string {
	// The scripts take the races comma-separated, split a token's run off at its at sign, and hand it single-quoted
	// to the box's shell and double-quoted to go test, and a run with a space would split there.
	if strings.ContainsAny(run, ",@'\"`") || strings.ContainsFunc(run, unicode.IsSpace) {
		return fmt.Sprintf("its run %q holds a comma, an at sign, a quote or a space, which the gate's scripts can't "+
			"carry", run)
	}
	selects, compileError := regexp.Compile(run)
	if compileError != nil {
		return fmt.Sprintf("its run %q is no regular expression: %v", run, compileError)
	}
	missed := []string{}
	for _, name := range tests {
		if !selects.MatchString(name) {
			missed = append(missed, name)
		}
	}
	if len(missed) > 0 {
		return fmt.Sprintf("its run %q leaves out %s, which the guard calls concurrent", run,
			strings.Join(missed, ", "))
	}
	return ""
}

// An entry of the -race list that runs only some of its package's tests runs every one the guard calls concurrent,
// by its name, its file or its own body, so a test that says it runs code at once is caught by the race detector at the gate however
// its package's entry narrows the run (#e8w514b), and its run is one the gate's scripts carry whole.
func TestEveryRaceRunSelectsItsPackagesConcurrencyTests(test *testing.T) {
	test.Parallel()
	root := filepath.Join("..", "..")
	for _, entry := range raceGated {
		if entry.run == "" {
			continue
		}
		directory := filepath.Join(root, entry.directory)
		tests := concurrencyTestsInDirectory(test, directory)
		// A test that starts goroutines in its own body runs code at once whatever its name says (#q1v0ern).
		byBody, bodyError := bodyConcurrentTests(directory)
		if bodyError != nil {
			test.Fatal(bodyError)
		}
		if refusal := raceRunRefusal(entry.run, append(tests, byBody...)); refusal != "" {
			test.Errorf("%s is on the -race list, and %s", entry.directory, refusal)
		}
	}
}

// raceRunRefusal refuses each run the wire, the shell or go test can't take, and one that leaves out a concurrency
// test, and takes one that selects every one.
func TestARaceRunIsRefusedWhenItCantBeCarriedOrLeavesATestOut(test *testing.T) {
	test.Parallel()
	tests := []string{"TestExtraBuildsSideBySideWriteWhatOneAtATimeWrites", "TestTheLintBesideTheLoadLeavesNoTrace"}
	for run, refusal := range map[string]string{
		"SideBySide|BesideTheLoad":  "",
		"SideBySide":                "leaves out TestTheLintBesideTheLoadLeavesNoTrace",
		"SideBySide,BesideTheLoad":  "the gate's scripts can't carry",
		"SideBySide@BesideTheLoad":  "the gate's scripts can't carry",
		"SideBySide|'Beside'":       "the gate's scripts can't carry",
		"SideBySide|\"Beside\"":     "the gate's scripts can't carry",
		"SideBySide| BesideTheLoad": "the gate's scripts can't carry",
		"SideBySide|(BesideTheLoad": "is no regular expression",
	} {
		got := raceRunRefusal(run, tests)
		if (refusal == "") != (got == "") || !strings.Contains(got, refusal) {
			test.Errorf("the run %q is refused as %q, want %q", run, got, refusal)
		}
	}
}

// The guard's two patterns read as intended: a name or a file that says it runs at once is found, and a test that
// merely mentions parallel or race in a rule's name is not.
func TestConcurrencyTestsAreFoundByNameAndFile(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{
		"TestConcurrentReadersNeverSeeAPartialFile":                             true,
		"TestDependenceSetsComputedAtOnceOverOneHostAgree":                      true,
		"TestRunCacheRecordsInParallelInTheSerialOrder":                         true,
		"TestBuildConcurrently":                                                 true,
		"TestExtraBuildsSideBySideWriteWhatOneAtATimeWrites":                    true,
		"TestTheLintBesideTheLoadLeavesNoTraceWhenALatePackageDoesNotTypeCheck": true,
		"TestNoUnclearedRaceTimeout":                                            false,
		"TestRequireParallelFlagsASerialTest":                                   false,
		"TestTheParallelRuleNamesItsCategory":                                   false,
		"TestTheBuildsWidthIsReadFromTheMachine":                                false,
	} {
		if got := concurrencyTestName.MatchString(name); got != want {
			test.Errorf("%s reads as a concurrency test: %t, want %t", name, got, want)
		}
	}
	for name, want := range map[string]bool{
		"dependence_concurrency_test.go":                true,
		"parallel_build_test.go":                        true,
		"fix_parallel_test.go":                          true,
		"concurrency_no_lost_update_test.go":            false,
		"correctness_no_uncleared_race_timeout_test.go": false,
		"analyzer_test.go":                              false,
	} {
		if got := concurrencyTestFile.MatchString(name); got != want {
			test.Errorf("%s reads as a concurrency test file: %t, want %t", name, got, want)
		}
	}
	for directory, home := range map[string]string{
		"internal/go_language/check": "TestDependenceSetsComputedAtOnceOverOneHostAgree",
		// Its test of the early start's handoff can fail only under -race (#e8w514b), and the package's entry runs
		// only the tests the guard calls concurrent, so the test's name says it runs code at once.
		"internal/go_language": "TestTheHostsGoEnvironmentIsWholeWhenALoadReadsItAtOnceWithTheEarlyStart",
	} {
		index := slices.IndexFunc(raceGated, func(entry raceEntry) bool { return entry.directory == directory })
		if index < 0 {
			test.Errorf("%s, home of %s, is not on the -race list", directory, home)
			continue
		}
		// Read from the package itself, so a home renamed or moved away is caught here rather than left named.
		if !slices.Contains(concurrencyTestsInDirectory(test, filepath.Join("..", "..", directory)), home) {
			test.Errorf("%s is no test of %s the guard calls concurrent", home, directory)
		}
		if run := raceGated[index].keptRun(); run != "" && !regexp.MustCompile(run).MatchString(home) {
			test.Errorf("%s's run on the -race list, %q, leaves out %s", directory, run, home)
		}
	}
}

// concurrencyCallNames are the calls the guard reads as starting goroutines: an errgroup's or a WaitGroup's Go, and
// the bounded pool's fan-outs over indexes.
var concurrencyCallNames = []string{"Go", "ForEach", "ForEachWriting"}

// startsGoroutines is whether node is a go statement or a call the guard reads as starting goroutines.
func startsGoroutines(node ast.Node) bool {
	switch node := node.(type) {
	case *ast.GoStmt:
		return true
	case *ast.CallExpr:
		switch function := node.Fun.(type) {
		case *ast.SelectorExpr:
			return slices.Contains(concurrencyCallNames, function.Sel.Name)
		case *ast.Ident:
			return slices.Contains(concurrencyCallNames, function.Name)
		}
	}
	return false
}

// declarationName is a function declaration's name as a site names it, `Type.Method` for a method.
func declarationName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	if star, isStar := receiver.(*ast.StarExpr); isStar {
		receiver = star.X
	}
	// A generic receiver, `Set[T]`, is named by its type alone.
	if index, isIndex := receiver.(*ast.IndexExpr); isIndex {
		receiver = index.X
	}
	if index, isIndex := receiver.(*ast.IndexListExpr); isIndex {
		receiver = index.X
	}
	if identifier, isIdentifier := receiver.(*ast.Ident); isIdentifier {
		return identifier.Name + "." + function.Name.Name
	}
	return function.Name.Name
}

// concurrencySites is every place the non-test Go files of directory itself start goroutines, each
// `<file>:<function>#<ordinal>` with the ordinal counting the function's sites in source order from 1, and a site
// outside any function, a package variable's literal, named for the file's package scope. Every file is read,
// whatever its build constraints, so a site only another platform compiles still needs its claims.
func concurrencySites(directory string) ([]string, error) {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return nil, readError
	}
	sites := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil,
			parser.SkipObjectResolution)
		if parseError != nil {
			return nil, parseError
		}
		for _, declaration := range parsed.Decls {
			owner := "package"
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				owner = declarationName(function)
			}
			ordinal := 0
			ast.Inspect(declaration, func(node ast.Node) bool {
				if node != nil && startsGoroutines(node) {
					ordinal++
					sites = append(sites, fmt.Sprintf("%s:%s#%d", name, owner, ordinal))
				}
				return true
			})
		}
	}
	return sites, nil
}

// bodyConcurrentTests is every top-level test of directory's test files that starts goroutines itself: a go statement
// or a call the guard reads as starting goroutines in its own body, or a call of a function of the package's test
// files that does, followed through such functions however deep. A test whose concurrency lives in its own file
// reaches no site of the package's code, so the sites alone would leave it out (#q1v0ern).
func bodyConcurrentTests(directory string) ([]string, error) {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return nil, readError
	}
	starts := map[string]bool{}
	calls := map[string][]string{}
	tests := []string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, name), nil,
			parser.SkipObjectResolution)
		if parseError != nil {
			return nil, parseError
		}
		for _, declaration := range parsed.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Recv != nil || function.Body == nil {
				continue
			}
			caller := function.Name.Name
			if strings.HasPrefix(caller, "Test") && caller != "TestMain" {
				tests = append(tests, caller)
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if node == nil {
					return true
				}
				if startsGoroutines(node) {
					starts[caller] = true
				}
				if call, isCall := node.(*ast.CallExpr); isCall {
					if callee, isIdentifier := call.Fun.(*ast.Ident); isIdentifier {
						calls[caller] = append(calls[caller], callee.Name)
					}
				}
				return true
			})
		}
	}
	// Followed to a fixed point, so a helper calling a helper that starts goroutines counts.
	for changed := true; changed; {
		changed = false
		for caller, callees := range calls {
			if starts[caller] {
				continue
			}
			if slices.ContainsFunc(callees, func(callee string) bool { return starts[callee] }) {
				starts[caller] = true
				changed = true
			}
		}
	}
	concurrent := []string{}
	for _, test := range tests {
		if starts[test] {
			concurrent = append(concurrent, test)
		}
	}
	return concurrent, nil
}

// testsOfDirectory is every top-level test of directory's own test files.
func testsOfDirectory(test *testing.T, directory string) []string {
	test.Helper()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		test.Fatal(readError)
	}
	tests := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, parseError := parser.ParseFile(token.NewFileSet(), filepath.Join(directory, entry.Name()), nil,
			parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		for _, declaration := range parsed.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Recv == nil &&
				strings.HasPrefix(function.Name.Name, "Test") && function.Name.Name != "TestMain" {
				tests = append(tests, function.Name.Name)
			}
		}
	}
	return tests
}

// narrowedRaceRunRefusals is every way entry's kept list fails the package at directory: a concurrent test the
// guard sees that the list leaves out or one it lists that isn't, a site of the package's code with fewer than
// minimumSiteClaims claims at least two goroutines wide or one the code no longer holds, a kept test the package
// doesn't hold, and a run the gate's scripts can't carry.
func narrowedRaceRunRefusals(test *testing.T, entry raceEntry, directory string) []string {
	test.Helper()
	refusals := []string{}
	kept := entry.kept
	if entry.run != "" {
		refusals = append(refusals, "it names a run beside its kept list, and the list is the run")
	}
	if !regexp.MustCompile(`^J-[0-9a-z]{7}$`).MatchString(kept.census) {
		refusals = append(refusals, fmt.Sprintf("its census %q is no Loom job's id", kept.census))
	}
	held := testsOfDirectory(test, directory)
	for _, name := range kept.tests() {
		if !slices.Contains(held, name) {
			refusals = append(refusals, fmt.Sprintf("it keeps %s, which the package doesn't hold", name))
		}
	}
	concurrent := concurrencyTestsInDirectory(test, directory)
	byBody, bodyError := bodyConcurrentTests(directory)
	if bodyError != nil {
		test.Fatal(bodyError)
	}
	concurrent = append(concurrent, byBody...)
	slices.Sort(concurrent)
	concurrent = slices.Compact(concurrent)
	listed := []string{}
	for _, keptTest := range kept.concurrent {
		listed = append(listed, keptTest.test)
		if strings.TrimSpace(keptTest.reason) == "" {
			refusals = append(refusals, fmt.Sprintf("it keeps %s with no reason", keptTest.test))
		}
		if !slices.Contains(concurrent, keptTest.test) {
			refusals = append(refusals, fmt.Sprintf("it lists %s as concurrent, which the guard doesn't call "+
				"concurrent: claim it at a site instead", keptTest.test))
		}
	}
	for _, name := range concurrent {
		if !slices.Contains(listed, name) {
			refusals = append(refusals, fmt.Sprintf("it leaves out %s, which the guard calls concurrent", name))
		}
	}
	sites, sitesError := concurrencySites(directory)
	if sitesError != nil {
		test.Fatal(sitesError)
	}
	claimed := map[string]bool{}
	for _, site := range kept.sites {
		claimed[site.site] = true
		if !slices.Contains(sites, site.site) {
			refusals = append(refusals, fmt.Sprintf("it claims %s, which the package's code no longer holds",
				site.site))
		}
		wide := []string{}
		for _, claim := range site.claims {
			if claim.width < 2 {
				refusals = append(refusals, fmt.Sprintf("its claim of %s by %s is %d wide, and a site one "+
					"goroutine wide can't race", site.site, claim.test, claim.width))
				continue
			}
			wide = append(wide, claim.test)
		}
		slices.Sort(wide)
		if len(slices.Compact(wide)) < minimumSiteClaims {
			refusals = append(refusals, fmt.Sprintf("%s has %d kept tests at least two goroutines wide, fewer "+
				"than %d", site.site, len(wide), minimumSiteClaims))
		}
	}
	for _, site := range sites {
		if !claimed[site] {
			refusals = append(refusals, fmt.Sprintf("its code starts goroutines at %s, which no kept test claims",
				site))
		}
	}
	if refusal := raceRunRefusal(entry.keptRun(), kept.tests()); refusal != "" {
		refusals = append(refusals, refusal)
	}
	return refusals
}

// An entry of the -race list that keeps a list runs exactly that list, and the list holds every test of its package
// the guard calls concurrent, by name, file or own body, and two tests for every place the package's own code starts
// goroutines, each at least two goroutines wide there. So a new go statement in a narrowed package, or a kept test
// dropped from its list, reads red here (#q1v0ern).
func TestEveryNarrowedRaceRunKeepsItsPackagesConcurrency(test *testing.T) {
	test.Parallel()
	narrowed := 0
	for _, entry := range raceGated {
		if entry.kept == nil {
			continue
		}
		narrowed++
		for _, refusal := range narrowedRaceRunRefusals(test, entry, filepath.Join("..", "..", entry.directory)) {
			test.Errorf("%s is on the -race list, and %s", entry.directory, refusal)
		}
	}
	if narrowed == 0 {
		test.Fatal("no entry of the -race list keeps a list, so nothing was checked")
	}
}

// The guard reads a package's sites and the tests that start goroutines themselves from the code: a go statement,
// a Go and a ForEach call each make a site, numbered within their function, and a test that starts goroutines through
// helpers is found as one that does so directly, while one that calls package code that does is not.
func TestConcurrencySitesAndBodyConcurrentTestsAreReadFromTheCode(test *testing.T) {
	test.Parallel()
	directory := filepath.Join("testdata", "race_sites")
	sites, sitesError := concurrencySites(directory)
	if sitesError != nil {
		test.Fatal(sitesError)
	}
	if want := []string{"sites.go:fanOut#1", "sites.go:fanOut#2", "sites.go:pool.run#1"}; !slices.Equal(sites, want) {
		test.Errorf("the fixture's sites read %v, want %v", sites, want)
	}
	byBody, bodyError := bodyConcurrentTests(directory)
	if bodyError != nil {
		test.Fatal(bodyError)
	}
	if want := []string{"TestDirect", "TestThroughHelpers"}; !slices.Equal(byBody, want) {
		test.Errorf("the fixture's tests that start goroutines themselves read %v, want %v", byBody, want)
	}
}

// A narrowed entry's list is refused for each way it can fail its package, and a whole list is taken: so dropping a
// claimed test, a site claimed once or one goroutine wide, a new site, a stale one, and a concurrent test left out
// each read red.
func TestANarrowedRaceRunIsRefusedForEachWayItFailsItsPackage(test *testing.T) {
	test.Parallel()
	directory := filepath.Join("testdata", "race_sites")
	whole := func() *raceKept {
		return &raceKept{
			census: "J-0000000",
			concurrent: []raceKeptTest{
				{test: "TestDirect", reason: "starts a goroutine"},
				{test: "TestThroughHelpers", reason: "starts one through helpers"},
			},
			sites: []raceSite{
				{site: "sites.go:fanOut#1", claims: []raceClaim{{"TestFansOut", 2}, {"TestDirect", 3}}},
				{site: "sites.go:fanOut#2", claims: []raceClaim{{"TestFansOut", 2}, {"TestThroughHelpers", 2}}},
				{site: "sites.go:pool.run#1", claims: []raceClaim{{"TestRuns", 2}, {"TestFansOut", 2}}},
			},
		}
	}
	for name, change := range map[string]struct {
		change  func(kept *raceKept)
		refusal string
	}{
		"whole": {func(*raceKept) {}, ""},
		"a claimed test dropped": {func(kept *raceKept) { kept.sites[2].claims = kept.sites[2].claims[:1] },
			"sites.go:pool.run#1 has 1 kept tests"},
		"a claim one goroutine wide": {func(kept *raceKept) { kept.sites[0].claims[0].width = 1 },
			"can't race"},
		"a site no test claims": {func(kept *raceKept) { kept.sites = kept.sites[:2] },
			"no kept test claims"},
		"a stale site": {func(kept *raceKept) {
			kept.sites = append(kept.sites, raceSite{site: "sites.go:gone#1",
				claims: []raceClaim{{"TestRuns", 2}, {"TestFansOut", 2}}})
		}, "no longer holds"},
		"a concurrent test left out": {func(kept *raceKept) { kept.concurrent = kept.concurrent[:1] },
			"leaves out TestThroughHelpers"},
		"a test the package lacks": {func(kept *raceKept) {
			kept.sites[0].claims = append(kept.sites[0].claims, raceClaim{"TestGone", 2})
		}, "doesn't hold"},
		"a census that is no job": {func(kept *raceKept) { kept.census = "by hand" }, "no Loom job's id"},
	} {
		kept := whole()
		change.change(kept)
		refusals := strings.Join(narrowedRaceRunRefusals(test, raceEntry{directory: directory, kept: kept},
			directory), "; ")
		if (change.refusal == "") != (refusals == "") || !strings.Contains(refusals, change.refusal) {
			test.Errorf("%s: the list is refused as %q, want %q", name, refusals, change.refusal)
		}
	}
}
