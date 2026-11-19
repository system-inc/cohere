package check

import (
	"fmt"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/settings"
	release "github.com/system-inc/cohere/internal/release/packaging"
)

// verdictDifferentialRootVariable names a tree whose packages the object differential judges for each extra build
// the engine's census needs beside the host's, as the package differential in go_engine judges the same tree's runs.
// Unset, the real-tree run is skipped:
//
//	COHERE_VERDICT_DIFFERENTIAL_ROOT=<tree> go run ./command/cohere-dev test ./internal/go_engine/check/ \
//	    -run 'TestEveryObjectOfARealTreeWhoseLinesDifferHasItsBitSet$' -v
const verdictDifferentialRootVariable = "COHERE_VERDICT_DIFFERENTIAL_ROOT"

// objectBuild is one build the object differential loads a tree under: its name, as a failure names it, and its
// environment.
type objectBuild struct {
	name        string
	environment []string
}

// objectLoad is every package one load reaches, by package ID, and the IDs of those it loaded with errors, which the
// differential leaves out, as their types are not the build's.
type objectLoad struct {
	packages map[string]*packages.Package
	broken   []string
}

// objectEnvironment is the process's environment with goos and tags, on the host's `GOARCH` as every extra build is
// (project.BuildsFor), cgo off and no workspace file, so a tree loads alone and the same on every machine, as block 1's
// fixtures load (build_dependence's buildEnvironment).
func objectEnvironment(goos string, tags string) []string {
	var environment []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		switch key {
		case "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOWORK":
			continue
		}
		environment = append(environment, pair)
	}
	goFlags := ""
	if tags != "" {
		goFlags = "-tags=" + tags
	}
	return append(environment, "GOOS="+goos, "GOARCH="+runtime.GOARCH, "CGO_ENABLED=0", "GOWORK=off",
		"GOFLAGS="+goFlags)
}

// loadObjects is pattern in directory loaded whole under environment, syntax and types for every package it reaches,
// with tests when tests says so, as block 1's graph needs every package it holds and the renderer reads every object
// of both builds as the type checker made it.
func loadObjects(test *testing.T, directory string, environment []string, pattern string, tests bool) objectLoad {
	test.Helper()
	roots, loadError := packages.Load(&packages.Config{
		Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
		Tests: tests,
		Dir:   directory,
		Env:   environment,
	}, pattern)
	if loadError != nil {
		test.Fatal(loadError)
	}
	loaded := objectLoad{packages: map[string]*packages.Package{}}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		loaded.packages[visited.ID] = visited
		if len(visited.Errors) > 0 {
			loaded.broken = append(loaded.broken, visited.ID)
		}
	})
	slices.Sort(loaded.broken)
	return loaded
}

// objectDependence is block 1's judgment of build against host: the graph of every package the host loaded, each
// added from a goroutine of its own, workspace saying which belong to the checked modules, judged against the
// difference of the two loads' files as block 1's fixture tests take it (build_dependence's differenceOf), each file
// matched by its path as written, a build-only one taken from the build's syntax, and each file the two do not share
// scanned for a line directive as the disk holds it, the bytes the loads parsed (build_dependence.HoldsLineDirective).
func objectDependence(
	host objectLoad,
	build objectLoad,
	workspace func(*packages.Package) bool,
) *build_dependence.Dependence {
	graph := build_dependence.NewGraph()
	var group sync.WaitGroup
	for _, loaded := range host.packages {
		group.Go(func() { graph.AddPackage(loaded, workspace(loaded), nil) })
	}
	group.Wait()
	difference := build_dependence.Difference{
		Packages:          map[string]build_dependence.PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	}
	for identifier := range host.packages {
		if _, isShared := build.packages[identifier]; !isShared {
			difference.HostOnlyPackages[identifier] = true
		}
	}
	for identifier, built := range build.packages {
		hosted, isShared := host.packages[identifier]
		if !isShared {
			difference.BuildOnlyPackages[identifier] = true
			difference.Packages[identifier] = build_dependence.PackageDifference{BuildOnlyFiles: built.Syntax,
				Workspace: workspace(built)}
			continue
		}
		var differing build_dependence.PackageDifference
		for _, file := range hosted.GoFiles {
			if slices.Contains(built.GoFiles, file) {
				differing.SharedFiles = append(differing.SharedFiles, file)
			} else {
				differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
				differing.EveryUnitDependent = differing.EveryUnitDependent ||
					build_dependence.HoldsLineDirective(file, os.ReadFile)
			}
		}
		for _, file := range built.Syntax {
			if name := built.Fset.File(file.FileStart).Name(); !slices.Contains(hosted.GoFiles, name) {
				differing.BuildOnlyFiles = append(differing.BuildOnlyFiles, file)
				differing.EveryUnitDependent = differing.EveryUnitDependent ||
					build_dependence.HoldsLineDirective(name, os.ReadFile)
			}
		}
		if len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 {
			difference.Packages[identifier] = differing
		}
	}
	return graph.Against(difference)
}

// declaredObject is one object a read of another package can name, and the name a failure gives it: a member by the
// type that holds it, as `runtime.mPadded._`, since a field's own name can be blank or shared.
type declaredObject struct {
	object types.Object
	name   string
}

// objectsOf are the objects of declaring a read of another package can name: every object of its scope, and of each
// defined type the methods it declares, its struct's fields, those of every unnamed struct a field holds directly, and
// its interface's own methods, each a member TypesHold judges by its own line.
func objectsOf(declaring *types.Package) []declaredObject {
	var objects []declaredObject
	var fieldsOf func(owner string, structure *types.Struct)
	fieldsOf = func(owner string, structure *types.Struct) {
		for field := range structure.Fields() {
			objects = append(objects, declaredObject{object: field, name: owner + "." + field.Name()})
			if inner, isStruct := field.Type().(*types.Struct); isStruct {
				fieldsOf(owner+"."+field.Name(), inner)
			}
		}
	}
	for _, name := range declaring.Scope().Names() {
		object := declaring.Scope().Lookup(name)
		owner := objectName(object)
		objects = append(objects, declaredObject{object: object, name: owner})
		named, isNamed := object.Type().(*types.Named)
		if _, isTypeName := object.(*types.TypeName); !isTypeName || !isNamed {
			continue
		}
		for method := range named.Methods() {
			objects = append(objects, declaredObject{object: method, name: owner + "." + method.Name()})
		}
		switch underlying := named.Underlying().(type) {
		case *types.Struct:
			fieldsOf(owner, underlying)
		case *types.Interface:
			for method := range underlying.ExplicitMethods() {
				objects = append(objects, declaredObject{object: method, name: owner + "." + method.Name()})
			}
		}
	}
	return objects
}

// firstDifference names the first line two renderings of one object differ by, each side's, or absent where one side
// has fewer lines, and false when they are alike.
func firstDifference(host []string, build []string) (string, bool) {
	for index := range max(len(host), len(build)) {
		hostLine, buildLine := "absent", "absent"
		if index < len(host) {
			hostLine = host[index]
		}
		if index < len(build) {
			buildLine = build[index]
		}
		if hostLine != buildLine {
			return fmt.Sprintf("the host's %q, the build's %q", hostLine, buildLine), true
		}
	}
	return "", false
}

// objectKind is what kind of object object is, as a tally counts it and a failure names it: a constant, a variable,
// a function, a type name, a field or a method, each a Go type of object of its own but the last two, which are a
// variable and a function a type holds, and never share a name with a package-level one.
func objectKind(object types.Object) string {
	switch kinded := object.(type) {
	case *types.Const:
		return "const"
	case *types.Var:
		if kinded.IsField() {
			return "field"
		}
		return "var"
	case *types.Func:
		if kinded.Signature().Recv() != nil {
			return "method"
		}
		return "func"
	case *types.TypeName:
		return "type name"
	default:
		return fmt.Sprintf("%T", object)
	}
}

// objectsByName are the objects of declaring objectsOf finds, by the name it gives each, those of one name in the
// order it finds them: several blank fields of one struct share their name, and the nth of the host's is the nth of
// the build's.
func objectsByName(declaring *types.Package) map[string][]types.Object {
	byName := map[string][]types.Object{}
	for _, declared := range objectsOf(declaring) {
		byName[declared.name] = append(byName[declared.name], declared.object)
	}
	return byName
}

// objectTally is what one build's object differential found: the objects compared, those with no counterpart in the
// build, by kind (objectKind), each kind of line that differed and was covered by a bit, and every failure, naming the
// object, which line differs and which bit is unset.
type objectTally struct {
	compared            int
	noCounterpart       int
	noCounterpartByKind map[string]int
	differedCovered     int
	failures            []string
}

// closureWholeReads are the objects a whole read of object reads, as dependenceReach.closure follows its closure digest
// in memo: object itself, the type names the digest's walk enters from object's types and those of the types it
// observes (follow), and from each, every type name its node's rendering names (typeNode.edges) and those of the types
// it observes, to the end of the closure, each type name walked once.
func closureWholeReads(memo *ReadsBuild, object types.Object) map[types.Object]bool {
	sizes := memo.graph.sizesForWalk()
	step := closureWalkStep(rootTypes(object), sizes, false)
	pending := withObserved(slices.Clone(step.typeNames), step.observedTypes)
	wholeReads := map[types.Object]bool{object: true}
	walked := map[*types.TypeName]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if walked[current] {
			continue
		}
		walked[current] = true
		wholeReads[current] = true
		node := memo.graph.renderedNode(current, sizes)
		pending = withObserved(append(pending, node.edges...), node.observedTypes)
	}
	return wholeReads
}

// objectDifferences holds block 1's bits to the lines TypesHold writes: every object block 1 calls unchanged must
// render alike in both builds. For every object of every package both loads hold without errors, each the host's read
// in the host's memo and its counterpart in the build's, the object the build's package of the same ID declares
// under the same name (objectsByName), it compares the lines TypesHold's renderer writes of it (useLines,
// closureDigest). The counterpart is found by name, not by objectpath as the verdict finds it (verdict.counterpart),
// since objectpath names no unexported package-level func, var or const, and those are most of what a body within its
// own package uses: by objectpath, over a quarter of the standard library's objects went uncompared. An object with no
// counterpart is one the build does not declare. It fails where
//   - the build declares an object of the name of another kind (objectKind), and its bit is unset;
//   - a member, any object but a type name, has its own line differ and its bit unset (a UseMember read, judged by
//     its own line, and gated by its own bit alone);
//   - a type name has its name line (UseName) or its method set lines (UseMethodSet) differ and its name bit unset;
//   - a type name has its closure digest differ and no type name its closure reaches has either bit set, the objects
//     a whole read of it reads (closureWholeReads), itself among them.
//
// Each object's bits are bitsOf's, block 1's Dependence.ObjectBits, or a test's that blanks some of them to show a
// leg failing (TestTheClosureDigestLegFailsWhereNoNameItReachesHasABit). Each memo is measured by its own load's
// sizes, as the host's renderings and a build's are (measureBy).
func objectDifferences(
	host objectLoad,
	build objectLoad,
	bitsOf func(types.Object) build_dependence.Bits,
) objectTally {
	hostMemo, buildMemo := NewReadsBuild(), NewReadsBuild()
	for _, loaded := range host.packages {
		hostMemo.measureBy(loaded.TypesSizes)
		break
	}
	for _, loaded := range build.packages {
		buildMemo.measureBy(loaded.TypesSizes)
		break
	}
	tally := objectTally{noCounterpartByKind: map[string]int{}}
	for _, identifier := range slices.Sorted(maps.Keys(host.packages)) {
		hosted, built := host.packages[identifier], build.packages[identifier]
		if built == nil || hosted.Types == nil || built.Types == nil || slices.Contains(host.broken, identifier) ||
			slices.Contains(build.broken, identifier) {
			continue
		}
		builtByName := objectsByName(built.Types)
		met := map[string]int{}
		for _, declared := range objectsOf(hosted.Types) {
			occurrence := met[declared.name]
			met[declared.name]++
			if occurrence >= len(builtByName[declared.name]) {
				tally.noCounterpart++
				tally.noCounterpartByKind[objectKind(declared.object)]++
				continue
			}
			counterpart := builtByName[declared.name][occurrence]
			tally.compared++
			bits := bitsOf(declared.object)
			named := declared.name
			if identifier != hosted.PkgPath {
				named += " in " + identifier
			}
			if hostKind, buildKind := objectKind(declared.object), objectKind(counterpart); hostKind != buildKind {
				if !bits.Name {
					tally.failures = append(tally.failures, named+": it is a "+hostKind+" on the host and a "+
						buildKind+" in the build, and its bit is unset")
				} else {
					tally.differedCovered++
				}
				continue
			}
			typeName, isTypeName := declared.object.(*types.TypeName)
			if !isTypeName {
				if differs, isDifferent := firstDifference(hostMemo.useLines(declared.object, build_reads.UseMember),
					buildMemo.useLines(counterpart, build_reads.UseMember)); isDifferent {
					if !bits.Name {
						tally.failures = append(tally.failures, named+": its own line differs, "+differs+
							", and its bit is unset")
					} else {
						tally.differedCovered++
					}
				}
				continue
			}
			for _, kind := range []build_reads.UseKind{build_reads.UseName, build_reads.UseMethodSet} {
				differs, isDifferent := firstDifference(hostMemo.useLines(typeName, kind),
					buildMemo.useLines(counterpart, kind))
				switch {
				case !isDifferent:
				case !bits.Name:
					tally.failures = append(tally.failures, named+": "+useDiffers[kind]+", "+differs+
						", and its name bit is unset")
				default:
					tally.differedCovered++
				}
			}
			if hostMemo.closureDigest(typeName) == buildMemo.closureDigest(counterpart) {
				continue
			}
			wholeReads := closureWholeReads(hostMemo, typeName)
			covered := false
			for reached := range wholeReads {
				if reachedBits := bitsOf(reached); reachedBits.Name || reachedBits.Underlying {
					covered = true
					break
				}
			}
			if covered {
				tally.differedCovered++
				continue
			}
			tally.failures = append(tally.failures, fmt.Sprintf("%s: its closure digest differs, and neither bit "+
				"is set on any of the %d names its closure reaches", named, len(wholeReads)))
		}
	}
	return tally
}

// reportObjectDifferences logs what tally compared under the build named name and fails naming each failure.
func reportObjectDifferences(test *testing.T, name string, tally objectTally, started time.Time) {
	test.Helper()
	var byKind []string
	for _, kind := range slices.Sorted(maps.Keys(tally.noCounterpartByKind)) {
		byKind = append(byKind, fmt.Sprintf("%d %s", tally.noCounterpartByKind[kind], kind))
	}
	test.Logf("%s: %d objects compared, %d with no counterpart (%s), %d differing lines covered by a bit, %d "+
		"failures, in %s", name, tally.compared, tally.noCounterpart, strings.Join(byKind, ", "),
		tally.differedCovered, len(tally.failures), time.Since(started).Round(time.Millisecond))
	for _, failure := range tally.failures {
		test.Errorf("%s: %s", name, failure)
	}
}

// objectFixtureBuilds are the builds every fixture of block 1 is judged under against the darwin host, as its own
// tests judge them: two platform builds and a tag build.
var objectFixtureBuilds = []objectBuild{
	{name: "linux", environment: objectEnvironment("linux", "")},
	{name: "windows", environment: objectEnvironment("windows", "")},
	{name: "darwin tags dependence_tag", environment: objectEnvironment("darwin", "dependence_tag")},
}

// objectFixtureWorkspace reports whether a fixture's package belongs to its workspace: a package named dependency
// stands for a dependency, as block 1's fixtures name one.
func objectFixtureWorkspace(loaded *packages.Package) bool {
	return !strings.HasSuffix(loaded.PkgPath, "/dependency")
}

// Block 1's bits are a cheaper proof of TypesHold's lines, one level below a reader: every object block 1 calls
// unchanged must render alike, so wherever a line TypesHold writes of an object differs between two builds, the bit
// its verdict gates that read by must be set (objectDifferences). Over every fixture of block 1's testdata, loaded
// from source on the darwin host and under each of objectFixtureBuilds, every object of every package both hold is
// compared. A build a fixture does not type-check under is left out and logged, as the engine analyzes nothing of a
// build with a type error, and a fixture compared under no build fails.
func TestEveryObjectWhoseLinesDifferHasItsBitSet(test *testing.T) {
	test.Parallel()

	fixtures := filepath.Join("..", "build_dependence", "testdata")
	entries, readError := os.ReadDir(fixtures)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		test.Run(entry.Name(), func(test *testing.T) {
			test.Parallel()

			directory := filepath.Join(fixtures, entry.Name())
			host := loadObjects(test, directory, objectEnvironment("darwin", ""), "./...", true)
			if len(host.broken) > 0 {
				test.Fatalf("the fixture does not type-check on the darwin host: %q", host.broken)
			}
			judgedUnder := 0
			for _, build := range objectFixtureBuilds {
				started := time.Now()
				built := loadObjects(test, directory, build.environment, "./...", true)
				if len(built.broken) > 0 {
					test.Logf("%s: not judged, the fixture does not type-check there: %q", build.name, built.broken)
					continue
				}
				judgedUnder++
				dependence := objectDependence(host, built, objectFixtureWorkspace)
				tally := objectDifferences(host, built, dependence.ObjectBits)
				reportObjectDifferences(test, build.name, tally, started)
			}
			if judgedUnder == 0 {
				test.Error("the fixture type-checks under no build, so nothing of it is judged")
			}
		})
	}
}

// The closure-digest leg of objectDifferences fails a type name whose closure digest differs where no type name its
// closure reaches has a bit set, which no fixture reaches while block 1 holds. In the closures fixture Holder points
// at Inner, whose underlying moves on linux, so Holder's and Inner's closure digests differ there and only Inner's
// underlying bit covers them: with block 1's bits as they are, Inner's underlying bit is set and nothing fails, and
// with Inner's bits blanked, the two closure digests fail and nothing else does, two fewer lines covered than block
// 1's bits cover, so the two block 1 covers are those.
func TestTheClosureDigestLegFailsWhereNoNameItReachesHasABit(test *testing.T) {
	test.Parallel()

	directory := filepath.Join("..", "build_dependence", "testdata", "closures")
	host := loadObjects(test, directory, objectEnvironment("darwin", ""), "./...", true)
	built := loadObjects(test, directory, objectEnvironment("linux", ""), "./...", true)
	if len(host.broken) > 0 || len(built.broken) > 0 {
		test.Fatalf("the closures fixture does not type-check: %q on the host, %q on linux", host.broken, built.broken)
	}
	hosted := host.packages["example.com/closures"]
	if hosted == nil {
		test.Fatal("the closures fixture's host holds no example.com/closures")
	}
	inner := hosted.Types.Scope().Lookup("Inner")
	dependence := objectDependence(host, built, objectFixtureWorkspace)

	// Both tallies on this goroutine, one after the other: ObjectBits is asked from one goroutine at a time.
	if bits := dependence.ObjectBits(inner); !bits.Underlying {
		test.Errorf("want Inner's underlying bit set on linux, got %+v", bits)
	}
	asSet := objectDifferences(host, built, dependence.ObjectBits)
	if len(asSet.failures) > 0 {
		test.Errorf("want nothing failing with block 1's bits, got %q", asSet.failures)
	}
	blanked := objectDifferences(host, built, func(object types.Object) build_dependence.Bits {
		if object == inner {
			return build_dependence.Bits{}
		}
		return dependence.ObjectBits(object)
	})
	want := []string{
		"example.com/closures.Holder: its closure digest differs, and neither bit is set on any of the 2 names its " +
			"closure reaches",
		"example.com/closures.Inner: its closure digest differs, and neither bit is set on any of the 1 names its " +
			"closure reaches",
	}
	if !slices.Equal(blanked.failures, want) {
		test.Errorf("with Inner's bits blanked, want the failures %q, got %q", want, blanked.failures)
	}
	if blanked.differedCovered != asSet.differedCovered-len(want) {
		test.Errorf("with Inner's bits blanked, want %d differing lines covered, %d fewer than block 1's bits cover, "+
			"got %d", asSet.differedCovered-len(want), len(want), blanked.differedCovered)
	}
}

// The same rule over the standard library, loaded from source with cgo off on the host's `GOOS` and under every other
// `GOOS` a release ships, on the host's `GOARCH` as every extra build is (project.BuildsFor), one build at a time, each
// against the host's graph with no package of it the workspace's, as the engine's side load judges it. It runs in
// the ordinary run: on the Mac, the host's load took 2.5 s and each build's load and comparison 1.4 s to 3 s, the test
// 6.9 s.
//
// Red under linux and windows on runtime.mPadded._, a field whose spelled type is an array of
// `2048 - mallocHeaderSize - mRedZoneSize - unsafe.Sizeof(m{})` bytes, so its own line moves with m's layout, which
// mOS gives each platform, while block 1 reads the type names a field's spelled type uses by their structure alone,
// never in full as a constant's value reads them, so the field's bit stays unset.
func TestEveryStandardLibraryObjectWhoseLinesDifferHasItsBitSet(test *testing.T) {
	test.Parallel()

	module := test.TempDir()
	if writeError := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module scratch\n\ngo 1.24\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	started := time.Now()
	host := loadObjects(test, module, objectEnvironment(runtime.GOOS, ""), "std", false)
	test.Logf("host %s: %d packages loaded in %s", runtime.GOOS, len(host.packages),
		time.Since(started).Round(time.Millisecond))
	notWorkspace := func(*packages.Package) bool { return false }
	judged := map[string]bool{runtime.GOOS: true}
	for _, target := range release.Targets {
		if judged[target.GoOperatingSystem] {
			continue
		}
		judged[target.GoOperatingSystem] = true
		// Not parallel: one build's std at a time holds one load's memory beside the host's, and each stays its own
		// subtest so a red names its build.
		test.Run(target.GoOperatingSystem, func(test *testing.T) {
			started := time.Now()
			built := loadObjects(test, module, objectEnvironment(target.GoOperatingSystem, ""), "std", false)
			if len(built.broken) > 0 {
				test.Logf("%d packages with errors left out: %q", len(built.broken), built.broken)
			}
			tally := objectDifferences(host, built, objectDependence(host, built, notWorkspace).ObjectBits)
			reportObjectDifferences(test, target.GoOperatingSystem, tally, started)
			if tally.compared == 0 {
				test.Error("compared no object, so the load found nothing")
			}
		})
	}
}

// objectTreeBuilds is the workspace rooted at root and the builds the engine checks of it: the host's build as go env
// reads it, then every extra build its census needs beside it, the targets its settings file names among them
// (project.BuildsFor, asked as the engine's readBuilds asks it). A target go tool dist list does not list is an error,
// as the engine refuses its run, since a target named and never checked would read as checked.
func objectTreeBuilds(root string) (project.Workspace, []project.Build, error) {
	workspace, workspaceError := project.ReadWorkspace(root)
	if workspaceError != nil {
		return project.Workspace{}, nil, workspaceError
	}
	census, censusError := project.TakeCensus(workspace)
	if censusError != nil {
		return project.Workspace{}, nil, censusError
	}
	goEnvironment, goEnvironmentError := load.ReadGoEnvironment(workspace.Root, workspace.Environment(), nil, nil)
	if goEnvironmentError != nil {
		return project.Workspace{}, nil, goEnvironmentError
	}
	ports, portsError := project.ReadPorts(workspace.Root, workspace.Environment())
	if portsError != nil {
		return project.Workspace{}, nil, portsError
	}
	// The settings file the engine reads with no `--lint-config` named, whose rules this package cannot judge, as it
	// cannot import the engine that knows every rule, so only the targets are read, as settings.Load reads them.
	targets, targetsError := settings.ReadTargets(root, "")
	if targetsError != nil {
		return project.Workspace{}, nil, targetsError
	}
	// Each target as a port, mirroring readBuilds, which this package cannot call, as the engine imports it.
	var extraTargets []project.Port
	for _, target := range targets {
		operatingSystem, architecture, _ := strings.Cut(target, "/")
		index := slices.IndexFunc(ports, func(port project.Port) bool {
			return port.OperatingSystem == operatingSystem && port.Architecture == architecture
		})
		if index < 0 {
			return project.Workspace{}, nil, fmt.Errorf("the go block's targets name %s, which go tool dist list "+
				"does not target, so the engine refuses the run", target)
		}
		extraTargets = append(extraTargets, ports[index])
	}
	buildContext, goFlags := goEnvironment.Target()
	builds, _, buildsError := project.BuildsFor(census, project.HostBuild(buildContext, goFlags), ports, extraTargets,
		os.ReadFile)
	if buildsError != nil {
		return project.Workspace{}, nil, buildsError
	}
	return workspace, builds, nil
}

// The object differential checks the targets a tree's settings file names, as the engine does (objectTreeBuilds): a
// fixture tree with one file every platform compiles, whose settings name freebsd/arm64, is checked under that build
// beside the host's, its targets read as settings.Load reads them, and one naming a target go tool dist list does not
// list is refused, naming it.
func TestTheObjectDifferentialChecksTheTargetsASettingsFileNames(test *testing.T) {
	test.Parallel()

	tree := func(test *testing.T, targets string) string {
		test.Helper()
		root := test.TempDir()
		files := map[string]string{
			"go.mod":              "module scratch\n\ngo 1.24\n",
			"scratch.go":          "package scratch\n\nconst Answer = 42\n",
			"CohereSettings.json": `{"go": {"rules": {}, "targets": ` + targets + `}}`,
		}
		for name, contents := range files {
			if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
				test.Fatal(writeError)
			}
		}
		return root
	}

	test.Run("named", func(test *testing.T) {
		test.Parallel()

		root := tree(test, `["freebsd/arm64"]`)
		loaded, loadError := settings.Load(root, "", nil, nil)
		if loadError != nil {
			test.Fatal(loadError)
		}
		read, readError := settings.ReadTargets(root, "")
		if readError != nil {
			test.Fatal(readError)
		}
		if !slices.Equal(read, loaded.Targets) {
			test.Fatalf("read the targets %q, and settings.Load reads %q", read, loaded.Targets)
		}
		_, builds, buildsError := objectTreeBuilds(root)
		if buildsError != nil {
			test.Fatal(buildsError)
		}
		if !slices.ContainsFunc(builds[1:], func(build project.Build) bool {
			return build.OperatingSystem == "freebsd" && build.Architecture == "arm64"
		}) {
			test.Errorf("the builds are %v, and none is freebsd/arm64, the target the settings file names", builds)
		}
	})

	test.Run("unlisted", func(test *testing.T) {
		test.Parallel()

		_, builds, buildsError := objectTreeBuilds(tree(test, `["nowhere/none"]`))
		if buildsError == nil {
			test.Fatalf("read the builds %v for a target go tool dist list does not list, which the engine refuses",
				builds)
		}
		if !strings.Contains(buildsError.Error(), "nowhere/none") {
			test.Errorf("refused for %q, which names no target, so another failure stood in for the refusal",
				buildsError)
		}
	})
}

// The same rule over the tree verdictDifferentialRootVariable names, skipped when it is unset: the builds the engine
// checks of it (objectTreeBuilds), the host's and every extra build, the targets its settings file names among them,
// each loaded with tests under the workspace's environment and the build's own, the packages of the tree's modules
// the workspace. It logs each build's counts and fails on any object whose bit the rule wants set and finds unset.
func TestEveryObjectOfARealTreeWhoseLinesDifferHasItsBitSet(test *testing.T) {
	test.Parallel()

	root := os.Getenv(verdictDifferentialRootVariable)
	if root == "" {
		test.Skipf("%s names no tree", verdictDifferentialRootVariable)
	}
	workspace, builds, buildsError := objectTreeBuilds(root)
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if len(builds) < 2 {
		test.Skipf("the census of %s needs no extra build", root)
	}
	environmentOf := func(build project.Build) []string {
		return slices.Concat(os.Environ(), workspace.Environment(), build.Environment())
	}
	inWorkspace := func(loaded *packages.Package) bool { return loaded.Module != nil && loaded.Module.Main }
	started := time.Now()
	host := loadObjects(test, workspace.Root, environmentOf(builds[0]), "./...", true)
	test.Logf("host %s: %d packages loaded, %d with errors left out, in %s", builds[0], len(host.packages),
		len(host.broken), time.Since(started).Round(time.Second))
	for _, build := range builds[1:] {
		started := time.Now()
		built := loadObjects(test, workspace.Root, environmentOf(build), "./...", true)
		test.Logf("%s: %d packages loaded, %d with errors left out", build, len(built.packages), len(built.broken))
		tally := objectDifferences(host, built, objectDependence(host, built, inWorkspace).ObjectBits)
		reportObjectDifferences(test, build.String(), tally, started)
	}
}
