package build_dependence

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// fixtureBuild is one build a fixture loads under, by its environment.
type fixtureBuild struct {
	name        string
	environment []string
}

// buildEnvironment is the process's environment with the build's own `GOOS` and tags, cgo off and no workspace file,
// so a fixture module loads alone and the same on every machine.
func buildEnvironment(goos string, tags string) []string {
	var environment []string
	for _, pair := range os.Environ() {
		key, _, _ := strings.Cut(pair, "=")
		switch key {
		case "GOOS", "CGO_ENABLED", "GOFLAGS", "GOWORK":
			continue
		}
		environment = append(environment, pair)
	}
	goFlags := ""
	if tags != "" {
		goFlags = "-tags=" + tags
	}
	return append(environment, "GOOS="+goos, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS="+goFlags)
}

// The host every fixture is judged against, and the two extra builds: a platform build and a tag build.
var (
	hostBuild   = fixtureBuild{name: "darwin", environment: buildEnvironment("darwin", "")}
	linuxBuild  = fixtureBuild{name: "linux", environment: buildEnvironment("linux", "")}
	taggedBuild = fixtureBuild{name: "darwin tags dependence_tag",
		environment: buildEnvironment("darwin", "dependence_tag")}
)

// loadedBuild is every package a fixture's load reaches, by package ID.
type loadedBuild map[string]*packages.Package

// loadKey is one fixture loaded under one build.
type loadKey struct {
	fixture string
	build   string
}

// fixtureLoads are the fixtures' loads, each run once for the package's tests however many ask for it.
var (
	fixtureLoadsLock sync.Mutex
	fixtureLoads     = map[loadKey]func() (loadedBuild, error){}
)

// loadFixture is testdata/fixture loaded whole under build, syntax and types for every package, its dependencies
// included, and its tests, as the unit-level check needs both builds loaded.
func loadFixture(test *testing.T, fixture string, build fixtureBuild) loadedBuild {
	test.Helper()
	fixtureLoadsLock.Lock()
	load, isLoading := fixtureLoads[loadKey{fixture: fixture, build: build.name}]
	if !isLoading {
		load = sync.OnceValues(func() (loadedBuild, error) {
			roots, loadError := packages.Load(&packages.Config{
				Mode:  packages.LoadAllSyntax | packages.NeedTypesInfo,
				Tests: true,
				Dir:   filepath.Join("testdata", fixture),
				Env:   build.environment,
			}, "./...")
			if loadError != nil {
				return nil, loadError
			}
			loaded := loadedBuild{}
			packages.Visit(roots, nil, func(visited *packages.Package) { loaded[visited.ID] = visited })
			for _, visited := range loaded {
				if len(visited.Errors) > 0 {
					return nil, fmt.Errorf("%s under %s: %v", visited.ID, build.name, visited.Errors)
				}
			}
			return loaded, nil
		})
		fixtureLoads[loadKey{fixture: fixture, build: build.name}] = load
	}
	fixtureLoadsLock.Unlock()
	loaded, loadError := load()
	if loadError != nil {
		test.Fatal(loadError)
	}
	return loaded
}

// fixtureWorkspace reports whether a fixture's package belongs to its workspace: a package named dependency stands
// for a dependency.
func fixtureWorkspace(loaded *packages.Package) bool {
	return !strings.HasSuffix(loaded.PkgPath, "/dependency")
}

// graphOf is the graph of host's packages, each added from a goroutine of its own, as the engine adds them while each
// package's rules run, except those excluded names.
func graphOf(host loadedBuild, excluded func(*packages.Package) bool) *Graph {
	graph := NewGraph()
	var group sync.WaitGroup
	for _, loaded := range host {
		if excluded != nil && excluded(loaded) {
			continue
		}
		group.Go(func() { graph.AddPackage(loaded, fixtureWorkspace(loaded)) })
	}
	group.Wait()
	return graph
}

// differenceOf is what build changes against host, from the two loads' file sets: each file matched by its path as
// written (GoFiles), as the engine matches them, and each build-only file taken from the build's syntax, which for
// these fixtures, with no cgo, is the file as written.
func differenceOf(host loadedBuild, build loadedBuild) Difference {
	difference := Difference{
		Packages:          map[string]PackageDifference{},
		HostOnlyPackages:  map[string]bool{},
		BuildOnlyPackages: map[string]bool{},
	}
	for identifier := range host {
		if _, isShared := build[identifier]; !isShared {
			difference.HostOnlyPackages[identifier] = true
		}
	}
	for identifier, built := range build {
		hosted, isShared := host[identifier]
		if !isShared {
			difference.BuildOnlyPackages[identifier] = true
			difference.Packages[identifier] = PackageDifference{BuildOnlyFiles: built.Syntax,
				Workspace: fixtureWorkspace(built)}
			continue
		}
		var differing PackageDifference
		for _, file := range hosted.GoFiles {
			if !slices.Contains(built.GoFiles, file) {
				differing.HostOnlyFiles = append(differing.HostOnlyFiles, file)
			}
		}
		for _, file := range built.Syntax {
			if !slices.Contains(hosted.GoFiles, built.Fset.File(file.FileStart).Name()) {
				differing.BuildOnlyFiles = append(differing.BuildOnlyFiles, file)
			}
		}
		if len(differing.HostOnlyFiles) > 0 || len(differing.BuildOnlyFiles) > 0 {
			difference.Packages[identifier] = differing
		}
	}
	return difference
}

// qualifierByPackagePath writes another package by its path, which is the same in both builds.
func qualifierByPackagePath(other *types.Package) string {
	return other.Path()
}

// fingerprintEntry is one fact the type checker recorded inside a unit, the object it names, if any, and for a
// selection the selection itself, whose path says which fields the unit selects.
type fingerprintEntry struct {
	text      string
	object    types.Object
	selection *types.Selection
}

// fingerprint is what the type checker recorded inside node, in source order: each expression's offset in its file,
// type and constant value; each identifier's use and definition; each selection's kind, object and index path.
func fingerprint(typesInformation *types.Info, fileSet *token.FileSet, node ast.Node) []fingerprintEntry {
	tokenFile := fileSet.File(node.Pos())
	var entries []fingerprintEntry
	ast.Inspect(node, func(visited ast.Node) bool {
		if visited == nil {
			return false
		}
		offset := tokenFile.Offset(visited.Pos())
		if expression, isExpression := visited.(ast.Expr); isExpression {
			if typeAndValue, isRecorded := typesInformation.Types[expression]; isRecorded {
				value := ""
				if typeAndValue.Value != nil {
					value = " = " + typeAndValue.Value.ExactString()
				}
				entries = append(entries, fingerprintEntry{text: fmt.Sprintf("%d type %s%s", offset,
					types.TypeString(typeAndValue.Type, qualifierByPackagePath), value)})
			}
		}
		switch visited := visited.(type) {
		case *ast.Ident:
			if object := typesInformation.Uses[visited]; object != nil {
				entries = append(entries, fingerprintEntry{text: fmt.Sprintf("%d use %s", offset,
					types.ObjectString(object, qualifierByPackagePath)), object: object})
			}
			if object := typesInformation.Defs[visited]; object != nil {
				entries = append(entries, fingerprintEntry{text: fmt.Sprintf("%d def %s", offset,
					types.ObjectString(object, qualifierByPackagePath)), object: object})
			}
		case *ast.SelectorExpr:
			if selection := typesInformation.Selections[visited]; selection != nil {
				entries = append(entries, fingerprintEntry{text: fmt.Sprintf("%d selection %d %s %v", offset,
					selection.Kind(), types.ObjectString(selection.Obj(), qualifierByPackagePath), selection.Index()),
					object: selection.Obj(), selection: selection})
			}
		}
		return true
	})
	return entries
}

// compareFingerprints are the ways two units' fingerprints differ, the first few of them, empty when they agree, and
// whether every difference is only a type name whose struct's unexported fields differ, none of which the unit
// selects (unexportedUntouched).
func compareFingerprints(hosted []fingerprintEntry, built []fingerprintEntry) ([]string, bool) {
	var differences []string
	onlyUnexported := true
	if len(hosted) != len(built) {
		onlyUnexported = false
		differences = append(differences, fmt.Sprintf("%d entries in the host, %d in the build", len(hosted),
			len(built)))
	}
	selected := selectedFields(hosted)
	for index := range min(len(hosted), len(built)) {
		if hosted[index].text == built[index].text {
			continue
		}
		if !unexportedUntouched(hosted[index], built[index], selected) {
			onlyUnexported = false
		}
		if len(differences) < 3 {
			differences = append(differences, "host "+hosted[index].text+" | build "+built[index].text)
		}
	}
	return differences, onlyUnexported && len(differences) > 0
}

// selectedFields are the names of the fields a unit's entries reach: every field an identifier uses or defines, a
// composite literal's keys among them, and every field on each selection's path, the embedded fields a promoted
// selection passes through included. A difference in any of them is never exempt.
func selectedFields(entries []fingerprintEntry) map[string]bool {
	selected := map[string]bool{}
	for _, entry := range entries {
		if variable, isVariable := entry.object.(*types.Var); isVariable && variable.IsField() {
			selected[variable.Name()] = true
		}
		if entry.selection == nil || entry.selection.Kind() != types.FieldVal {
			continue
		}
		current := entry.selection.Recv()
		for _, index := range entry.selection.Index() {
			if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
				current = pointer.Elem()
			}
			structure, isStruct := current.Underlying().(*types.Struct)
			if !isStruct || index >= structure.NumFields() {
				break
			}
			selected[structure.Field(index).Name()] = true
			current = structure.Field(index).Type()
		}
	}
	return selected
}

// unexportedUntouched is whether two entries differ only in a type name whose struct's unexported fields differ, none
// of which the unit selects: the check's one exception class, kept apart and counted, never hidden. A type name's
// types.ObjectString writes its underlying type, the whole struct, while what a unit that only names the type reads of
// it is its name, so such a unit records differently where nothing it reads differs. The counter met five, runtime
// units on windows at deeafbf4 naming mcache and mheap, whose unexported array lengths hang on _NumStackOrders. Whether
// any rule reads more of a named type than its kind of use says is block 3's rule input inventory to answer, not this
// check's (#ewgh523, section 4). A difference in an exported field, or in one the unit selects, is never exempt.
func unexportedUntouched(hosted fingerprintEntry, built fingerprintEntry, selected map[string]bool) bool {
	hostedName, isHostedTypeName := hosted.object.(*types.TypeName)
	builtName, isBuiltTypeName := built.object.(*types.TypeName)
	if !isHostedTypeName || !isBuiltTypeName || hostedName.Name() != builtName.Name() {
		return false
	}
	hostedStruct, isHostedStruct := hostedName.Type().Underlying().(*types.Struct)
	builtStruct, isBuiltStruct := builtName.Type().Underlying().(*types.Struct)
	if !isHostedStruct || !isBuiltStruct {
		return false
	}
	fields := func(structure *types.Struct) map[string]string {
		written := map[string]string{}
		for field := range structure.Fields() {
			written[field.Name()] = types.TypeString(field.Type(), qualifierByPackagePath)
		}
		return written
	}
	hostedFields, builtFields := fields(hostedStruct), fields(builtStruct)
	differing := map[string]bool{}
	for name, written := range hostedFields {
		if builtFields[name] != written {
			differing[name] = true
		}
	}
	for name, written := range builtFields {
		if hostedFields[name] != written {
			differing[name] = true
		}
	}
	for name := range differing {
		if token.IsExported(name) || selected[name] {
			return false
		}
	}
	return len(differing) > 0
}

// unitDeclarations are a file's units, in source order.
func unitDeclarations(file *ast.File) []ast.Node {
	var units []ast.Node
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			units = append(units, declaration)
		case *ast.GenDecl:
			if declaration.Tok == token.IMPORT {
				continue
			}
			for _, spec := range declaration.Specs {
				units = append(units, spec)
			}
		}
	}
	return units
}

// unitCheck is the unit-level check's outcome: each miss, naming its unit; each independent unit whose only difference
// is unexported fields it never selects (unexportedUntouched), by its name, kept apart and never hidden; the
// independent units it compared, and by name those that recorded exactly alike; and, as its calibration, the
// dependent units in shared files whose fingerprints really differ, which shows the comparison can see a difference.
type unitCheck struct {
	misses              []string
	unexportedUntouched []string
	identical           []string
	compared            int
	dependentDiffer     int
}

// checkUnits compares, for every unit in a file both builds compile, what the type checker recorded inside it in host
// and in build, and counts every unit dependence calls independent whose records differ as a miss, unless its only
// difference is unexported fields it never selects.
func checkUnits(host loadedBuild, build loadedBuild, dependence *Dependence) unitCheck {
	var checked unitCheck
	for _, identifier := range slices.Sorted(maps.Keys(host)) {
		hosted, built := host[identifier], build[identifier]
		if built == nil || hosted.TypesInfo == nil || built.TypesInfo == nil {
			continue
		}
		builtUnits := map[string]ast.Node{}
		for _, file := range built.Syntax {
			tokenFile := built.Fset.File(file.FileStart)
			for _, unit := range unitDeclarations(file) {
				builtUnits[fmt.Sprintf("%s:%d", tokenFile.Name(), tokenFile.Offset(unit.Pos()))] = unit
			}
		}
		for _, file := range hosted.Syntax {
			tokenFile := hosted.Fset.File(file.FileStart)
			for _, unit := range unitDeclarations(file) {
				offset := tokenFile.Offset(unit.Pos())
				counterpart, isShared := builtUnits[fmt.Sprintf("%s:%d", tokenFile.Name(), offset)]
				if !isShared {
					continue
				}
				differences, onlyUnexported := compareFingerprints(fingerprint(hosted.TypesInfo, hosted.Fset, unit),
					fingerprint(built.TypesInfo, built.Fset, counterpart))
				if dependence.UnitDependent(identifier, tokenFile.Position(unit.Pos()).Filename, offset) {
					if len(differences) > 0 {
						checked.dependentDiffer++
					}
					continue
				}
				checked.compared++
				switch {
				case len(differences) == 0:
					checked.identical = append(checked.identical, unitName(unit))
				case onlyUnexported:
					checked.unexportedUntouched = append(checked.unexportedUntouched, unitName(unit))
				case len(differences) > 0:
					checked.misses = append(checked.misses, fmt.Sprintf("%s %s %s: %s", identifier, unitName(unit),
						hosted.Fset.Position(unit.Pos()), strings.Join(differences, "; ")))
				}
			}
		}
	}
	return checked
}

// unitName is how a test names a unit: `func Name`, `method Type.Name`, `type Name`, or `value Name` for a var or
// const spec, by its first name.
func unitName(unit ast.Node) string {
	switch unit := unit.(type) {
	case *ast.FuncDecl:
		if unit.Recv != nil && len(unit.Recv.List) > 0 {
			return "method " + receiverBaseName(unit.Recv.List[0].Type) + "." + unit.Name.Name
		}
		return "func " + unit.Name.Name
	case *ast.TypeSpec:
		return "type " + unit.Name.Name
	case *ast.ValueSpec:
		return "value " + unit.Names[0].Name
	}
	return fmt.Sprintf("%T", unit)
}
