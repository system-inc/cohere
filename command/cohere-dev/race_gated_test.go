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
	"sync"
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
// by its name, its file or its own body, so a test that says it runs code at once is caught by the race detector at
// the gate however its package's entry narrows the run (#e8w514b), and its run is one the gate's scripts carry whole.
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

// goroutineStartersDirectory is the package whose exported functions are the house's ways to start goroutines.
var goroutineStartersDirectory = filepath.Join("..", "..", "internal", "go_language", "bounded")

// skippersDirectories are the packages whose exported functions are the house's ways for a test to skip: the needs a
// test states (needs.Tool, OptIn, NoRace, Platform) and cgo translation's requirement.
var skippersDirectories = []string{
	filepath.Join("..", "..", "internal", "testing", "needs"),
	filepath.Join("..", "..", "internal", "go_language", "cgo_testing"),
}

// concurrencyCallNames are the calls the guard reads as starting goroutines: an errgroup's or a WaitGroup's Go, and
// every exported function or method of the bounded package that starts one, read from that package's code
// (goroutineStarters), so a new way to start goroutines there is a site wherever it is called (#q1v0ern).
var concurrencyCallNames = sync.OnceValues(func() ([]string, error) {
	starters, startersError := goroutineStarters(goroutineStartersDirectory)
	return append([]string{"Go"}, starters...), startersError
})

// skipCallNames are the calls the guard reads as a test skipping: testing's own, and every exported function of the
// skippers' packages that skips, read from their code (skippers), so a claimant that skips through a need doesn't
// count toward a site's claims (#q1v0ern, the Go Square's S6).
var skipCallNames = sync.OnceValues(func() ([]string, error) {
	names := []string{"Skip", "Skipf", "SkipNow"}
	for _, directory := range skippersDirectories {
		found, foundError := skippers(directory)
		if foundError != nil {
			return nil, foundError
		}
		names = append(names, found...)
	}
	return names, nil
})

// goroutineStarters is every exported function and method of the non-test Go files of directory that starts a
// goroutine, by a go statement or a Go call, itself or through the package's own functions.
func goroutineStarters(directory string) ([]string, error) {
	return exportedReaching(directory, func(node ast.Node) bool {
		if _, isGo := node.(*ast.GoStmt); isGo {
			return true
		}
		call, isCall := node.(*ast.CallExpr)
		return isCall && calleeName(call) == "Go"
	})
}

// skippers is every exported function and method of the non-test Go files of directory that skips its test, by a
// Skip, Skipf or SkipNow call, itself or through the package's own functions.
func skippers(directory string) ([]string, error) {
	return exportedReaching(directory, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return false
		}
		name := calleeName(call)
		return name == "Skip" || name == "Skipf" || name == "SkipNow"
	})
}

// exportedReaching is every exported function and method of the non-test Go files of directory whose body holds a
// node matches is true of, or calls a function or method of the package that does, followed to a fixed point, by its
// bare name. Every file is read, whatever its build constraints, so a function that does so under one build counts.
func exportedReaching(directory string, matches func(ast.Node) bool) ([]string, error) {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return nil, readError
	}
	reaches := map[string]bool{}
	calls := map[string][]string{}
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
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			caller := function.Name.Name
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if node == nil {
					return true
				}
				if matches(node) {
					reaches[caller] = true
				}
				if call, isCall := node.(*ast.CallExpr); isCall {
					if callee := calleeName(call); callee != "" {
						calls[caller] = append(calls[caller], callee)
					}
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for caller, callees := range calls {
			if !reaches[caller] && slices.ContainsFunc(callees, func(callee string) bool { return reaches[callee] }) {
				reaches[caller] = true
				changed = true
			}
		}
	}
	exported := []string{}
	for name := range reaches {
		if token.IsExported(name) {
			exported = append(exported, name)
		}
	}
	slices.Sort(exported)
	if len(exported) == 0 {
		return nil, fmt.Errorf("%s exports no function the guard is reading for, so it would read no call as one",
			directory)
	}
	return exported, nil
}

// calleeName is the bare name a call calls, its selector's or its identifier's, looking through parentheses and the
// type arguments of a generic call, empty for a call of anything else.
func calleeName(call *ast.CallExpr) string {
	function := call.Fun
	for {
		switch unwrapped := function.(type) {
		case *ast.ParenExpr:
			function = unwrapped.X
			continue
		case *ast.IndexExpr:
			function = unwrapped.X
			continue
		case *ast.IndexListExpr:
			function = unwrapped.X
			continue
		case *ast.SelectorExpr:
			return unwrapped.Sel.Name
		case *ast.Ident:
			return unwrapped.Name
		}
		return ""
	}
}

// startsGoroutines is whether node is a go statement or a call the guard reads as starting goroutines.
func startsGoroutines(node ast.Node) bool {
	switch node := node.(type) {
	case *ast.GoStmt:
		return true
	case *ast.CallExpr:
		// Ignored: concurrencySites and bodyConcurrentTests, the guard's only ways here, return the error of a bounded
		// package they can't read before anything reaches this.
		names, _ := concurrencyCallNames()
		return slices.Contains(names, calleeName(node))
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
	if _, namesError := concurrencyCallNames(); namesError != nil {
		return nil, namesError
	}
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
		// Counted per owner across the file's declarations, so two of one name, two init functions or two package
		// variables' literals, still give each site a key of its own.
		ordinals := map[string]int{}
		for _, declaration := range parsed.Decls {
			owner := "package"
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction {
				owner = declarationName(function)
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				if node != nil && startsGoroutines(node) {
					ordinals[owner]++
					sites = append(sites, fmt.Sprintf("%s:%s#%d", name, owner, ordinals[owner]))
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
	if _, namesError := concurrencyCallNames(); namesError != nil {
		return nil, namesError
	}
	return testsReaching(directory, startsGoroutines)
}

// skippingTests is every top-level test of directory's test files that may skip: a call skipCallNames reads as one,
// testing's own or a need's that skips when the box lacks what it needs (needs.OptIn, cgo_testing.RequireTranslation),
// in its own body or a function of the package's test files it calls. A claimant that may skip on a gate box may run
// nothing there, so it never counts toward a site's claims (#q1v0ern, the Go Square's S6).
func skippingTests(directory string) ([]string, error) {
	names, namesError := skipCallNames()
	if namesError != nil {
		return nil, namesError
	}
	return testsReaching(directory, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		return isCall && slices.Contains(names, calleeName(call))
	})
}

// testsReaching is every top-level test of directory's test files whose body holds a node matches is true of, or
// calls a function of the package's test files that does, followed to a fixed point, by its bare name.
func testsReaching(directory string, matches func(ast.Node) bool) ([]string, error) {
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		return nil, readError
	}
	reaches := map[string]bool{}
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
				if matches(node) {
					reaches[caller] = true
				}
				if call, isCall := node.(*ast.CallExpr); isCall {
					if callee := calleeName(call); callee != "" {
						calls[caller] = append(calls[caller], callee)
					}
				}
				return true
			})
		}
	}
	for changed := true; changed; {
		changed = false
		for caller, callees := range calls {
			if !reaches[caller] && slices.ContainsFunc(callees, func(callee string) bool { return reaches[callee] }) {
				reaches[caller] = true
				changed = true
			}
		}
	}
	found := []string{}
	for _, test := range tests {
		if reaches[test] {
			found = append(found, test)
		}
	}
	return found, nil
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
func narrowedRaceRunRefusals(test *testing.T, entry raceEntry, root string) []string {
	test.Helper()
	directory := filepath.Join(root, filepath.FromSlash(entry.directory))
	if _, namesError := concurrencyCallNames(); namesError != nil {
		test.Fatalf("reading the bounded package's ways to start goroutines: %v", namesError)
	}
	refusals := []string{}
	kept := entry.kept
	if len(kept.tests()) == 0 {
		refusals = append(refusals, "its kept list keeps no test, so its run selects nothing and passes")
	}
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
			skips, refusal := claimSkips(test, entry, root, claim)
			if refusal != "" {
				refusals = append(refusals, fmt.Sprintf("its claim of %s %s", site.site, refusal))
				continue
			}
			if !skips {
				wide = append(wide, claim.from+":"+claim.test)
			}
		}
		slices.Sort(wide)
		if len(slices.Compact(wide)) < minimumSiteClaims {
			refusals = append(refusals, fmt.Sprintf("%s has %d kept tests at least two goroutines wide that never "+
				"skip, fewer than %d", site.site, len(wide), minimumSiteClaims))
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

// claimSkips is whether claim's test may skip on a gate box (skippingTests), read in entry's own package, or, for a
// claim from another narrowed entry, in that entry's package under root, which must keep the test itself, so the
// -race run that holds the site to the claim is that entry's. Its refusal names a claim from no other narrowed entry,
// or of a test the claiming entry doesn't keep.
func claimSkips(test *testing.T, entry raceEntry, root string, claim raceClaim) (bool, string) {
	test.Helper()
	claimingDirectory := filepath.Join(root, filepath.FromSlash(entry.directory))
	if claim.from != "" {
		if claim.from == entry.directory {
			return false, fmt.Sprintf("names its own entry, %s, as another's", claim.from)
		}
		if !regexp.MustCompile(`^J-[0-9a-z]{7}$`).MatchString(claim.census) {
			return false, fmt.Sprintf("from %s names its census as %q, no Loom job's id: a claim from another entry "+
				"is counted by that entry's census", claim.from, claim.census)
		}
		index := slices.IndexFunc(raceGated, func(other raceEntry) bool { return other.directory == claim.from })
		if index < 0 || raceGated[index].kept == nil {
			return false, fmt.Sprintf("names %s, which is no other narrowed entry of the -race list", claim.from)
		}
		if !slices.Contains(raceGated[index].kept.tests(), claim.test) {
			return false, fmt.Sprintf("by %s's %s, which %s's run doesn't keep", claim.from, claim.test, claim.from)
		}
		claimingDirectory = filepath.Join(root, filepath.FromSlash(claim.from))
	}
	skipping, skippingError := skippingTests(claimingDirectory)
	if skippingError != nil {
		test.Fatal(skippingError)
	}
	return slices.Contains(skipping, claim.test), ""
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
		for _, refusal := range narrowedRaceRunRefusals(test, entry, filepath.Join("..", "..")) {
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
	if want := []string{"sites.go:fanOut#1", "sites.go:fanOut#2", "sites.go:pool.run#1", "sites.go:besides#1",
		"sites.go:init#1", "sites.go:init#2", "sites.go:set.walk#1", "sites.go:package#1"}; !slices.Equal(sites, want) {
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

// The guard reads the bounded package's ways to start goroutines, and the needs' ways to skip, by following each
// exported function through the package's own calls: an exported function that starts or skips only through an
// unexported helper is one, the helper itself is not, and one that does neither is not.
func TestStartersAndSkippersAreFollowedThroughTheirPackagesHelpers(test *testing.T) {
	test.Parallel()
	directory := filepath.Join("testdata", "race_starters")
	starters, startersError := goroutineStarters(directory)
	if startersError != nil {
		test.Fatal(startersError)
	}
	if want := []string{"Start"}; !slices.Equal(starters, want) {
		test.Errorf("the fixture's starters read %v, want %v", starters, want)
	}
	found, skippersError := skippers(directory)
	if skippersError != nil {
		test.Fatal(skippersError)
	}
	if want := []string{"Needs"}; !slices.Equal(found, want) {
		test.Errorf("the fixture's skippers read %v, want %v", found, want)
	}
}

// A narrowed entry's list is refused for each way it can fail its package, and a whole list is taken: so dropping a
// claimed test, a site claimed once or one goroutine wide, a claimant that may skip counted as one of the two, a new
// site, a stale one, a concurrent test left out, a run beside the list, an empty list, and a claim from no other
// narrowed entry each read red. The fixture's sites include a call of a starter by its bounded name, two init
// functions, a method of a generic type and a package variable's literal, each keyed apart.
func TestANarrowedRaceRunIsRefusedForEachWayItFailsItsPackage(test *testing.T) {
	test.Parallel()
	// Read from the module root, as the guard reads the list, so a claim from check reads check's own package.
	directory := "command/cohere-dev/testdata/race_sites"
	claimedTwice := func(site string) raceSite {
		return raceSite{site: site, claims: []raceClaim{{test: "TestFansOut", width: 2},
			{test: "TestDirect", width: 2}}}
	}
	whole := func() *raceKept {
		return &raceKept{
			census: "J-0000000",
			concurrent: []raceKeptTest{
				{test: "TestDirect", reason: "starts a goroutine"},
				{test: "TestThroughHelpers", reason: "starts one through helpers"},
			},
			sites: []raceSite{
				{site: "sites.go:fanOut#1", claims: []raceClaim{{test: "TestFansOut", width: 2},
					{test: "TestDirect", width: 3}}},
				{site: "sites.go:fanOut#2", claims: []raceClaim{{test: "TestFansOut", width: 2},
					{test: "TestThroughHelpers", width: 2}}},
				{site: "sites.go:pool.run#1", claims: []raceClaim{{test: "TestRuns", width: 2},
					{test: "TestFansOut", width: 2}}},
				{site: "sites.go:besides#1", claims: []raceClaim{{test: "TestBesides", width: 2},
					{test: "TestFansOut", width: 2}}},
				claimedTwice("sites.go:init#1"),
				claimedTwice("sites.go:init#2"),
				claimedTwice("sites.go:set.walk#1"),
				claimedTwice("sites.go:package#1"),
			},
		}
	}
	for name, change := range map[string]struct {
		change  func(entry *raceEntry)
		refusal string
	}{
		"whole": {func(*raceEntry) { /* The whole list, unchanged, which nothing refuses. */ }, ""},
		"a claimed test dropped": {func(entry *raceEntry) {
			entry.kept.sites[2].claims = entry.kept.sites[2].claims[:1]
		}, "sites.go:pool.run#1 has 1 kept tests"},
		"a claim one goroutine wide": {func(entry *raceEntry) { entry.kept.sites[0].claims[0].width = 1 },
			"can't race"},
		"a claimant that may skip": {func(entry *raceEntry) {
			entry.kept.sites[2].claims[0].test = "TestSkips"
		}, "sites.go:pool.run#1 has 1 kept tests at least two goroutines wide that never skip"},
		"a claimant that may skip through a need": {func(entry *raceEntry) {
			entry.kept.sites[2].claims[0].test = "TestOptsIn"
		}, "sites.go:pool.run#1 has 1 kept tests at least two goroutines wide that never skip"},
		"a claim from another entry that it doesn't keep": {func(entry *raceEntry) {
			entry.kept.sites[0].claims[0] = raceClaim{test: "TestDirect", width: 2, from: "internal/go_language/check",
				census: "J-0000000"}
		}, "which internal/go_language/check's run doesn't keep"},
		"a claim from the entry's own directory": {func(entry *raceEntry) {
			entry.kept.sites[0].claims[0].from = directory
		}, "names its own entry"},
		"a claim from another entry with no census": {func(entry *raceEntry) {
			entry.kept.sites[0].claims[0] = raceClaim{test: "TestDirect", width: 2, from: "internal/go_language/check"}
		}, "no Loom job's id: a claim from another entry"},
		"a site no test claims": {func(entry *raceEntry) {
			entry.kept.sites = slices.Delete(entry.kept.sites, 3, 4)
		}, "starts goroutines at sites.go:besides#1, which no kept test claims"},
		"a stale site": {func(entry *raceEntry) {
			entry.kept.sites = append(entry.kept.sites, claimedTwice("sites.go:gone#1"))
		}, "no longer holds"},
		"a concurrent test left out": {func(entry *raceEntry) { entry.kept.concurrent = entry.kept.concurrent[:1] },
			"leaves out TestThroughHelpers"},
		"a test the package lacks": {func(entry *raceEntry) {
			entry.kept.sites[0].claims = append(entry.kept.sites[0].claims, raceClaim{test: "TestGone", width: 2})
		}, "doesn't hold"},
		"a census that is no job": {func(entry *raceEntry) { entry.kept.census = "by hand" }, "no Loom job's id"},
		"a run beside the list":   {func(entry *raceEntry) { entry.run = "Direct" }, "names a run beside"},
		"an empty list": {func(entry *raceEntry) { entry.kept = &raceKept{census: "J-0000000"} },
			"keeps no test"},
		"a claim from no other narrowed entry": {func(entry *raceEntry) {
			entry.kept.sites[0].claims[0].from = "internal/edit"
			entry.kept.sites[0].claims[0].census = "J-0000000"
		}, "names internal/edit, which is no other narrowed entry"},
	} {
		entry := raceEntry{directory: directory, kept: whole()}
		change.change(&entry)
		refusals := strings.Join(narrowedRaceRunRefusals(test, entry, filepath.Join("..", "..")), "; ")
		if (change.refusal == "") != (refusals == "") || !strings.Contains(refusals, change.refusal) {
			test.Errorf("%s: the list is refused as %q, want %q", name, refusals, change.refusal)
		}
	}
}
