package go_engine

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_dependence"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// The reach at body grain, stage 2 of #9paaen9 (#w72zxtq): for each extra build, which of the host's function and
// value bodies a difference reaches, from stage 1's record of what each declaration uses (build_reads.FunctionUses)
// joined with the build's judgment of each object (build_dependence.Dependence.ObjectBits). Nothing is skipped by it
// yet: it is a census, counted beside today's verdict under `COHERE_BODY_REACH_CENSUS=<file>` with
// `--build-dependence --other-builds`, and written to that file at the end of the builds, changing no finding and no
// record. Every body it leaves unreached that the dependence graph's own unit verdict calls dependent
// (Dependence.UnitDependent) is listed, a lead to read before anything is skipped by it.
//
// A use reaches a body when a bit its kind reads is set in the build (the Square's accepted mapping):
//   - Named, a function, var, constant or type name: the object's name bit, which a constant's value is under.
//   - Named, a universe object or an import's name: no bit. A universe name a build-only file of the package declares
//     at package level means another object there, and so does a dot-imported one, which shadowed reaches apart.
//   - Selected: the selected member's name bit, a method's carrying its receiver's, and the underlying bit of the
//     named type the selection starts from, its fields and its embedding being its layout. Each embedded field on the
//     path is its own selection in the record, so reads its own name bit the same way.
//   - ValueType and TypeArgument: both bits, since a value's copy, comparison or conversion and a constraint's check
//     read its layout and its method set alike.
// A body is reached by the first of: its package unrecorded, a part of it unknown, its signature reaching (its body
// type-checks against the signature), its body reaching, by the first use in the record's order that does, and a
// shadowed name in either part.

// bodyReachCensusVariable names the file the census is written to, unset for a run that writes none.
const bodyReachCensusVariable = "COHERE_BODY_REACH_CENSUS"

// bodyDifferentialVariable, set to 1, turns on the body differential (build_body_differential.go).
const bodyDifferentialVariable = "COHERE_BODY_DIFFERENTIAL"

// bodyReachReason is why the census calls a body reached in a build.
type bodyReachReason string

const (
	reachedUnrecorded   bodyReachReason = "Unrecorded"
	reachedUnknown      bodyReachReason = "Unknown"
	reachedSignature    bodyReachReason = "Signature"
	reachedNamed        bodyReachReason = "Named"
	reachedConstant     bodyReachReason = "Constant"
	reachedSelected     bodyReachReason = "Selected"
	reachedValueType    bodyReachReason = "ValueType"
	reachedTypeArgument bodyReachReason = "TypeArgument"
	reachedShadowed     bodyReachReason = "Shadowed"
	reachedLoadMode     bodyReachReason = "LoadMode"
)

// bodyReachReasons are the reasons in the order the census writes them, the order a body is judged by.
var bodyReachReasons = []bodyReachReason{reachedUnrecorded, reachedUnknown, reachedSignature, reachedNamed,
	reachedConstant, reachedLoadMode, reachedSelected, reachedValueType, reachedTypeArgument, reachedShadowed}

// bodyReachHost is the host's side of the census, shared by every build and read only: each package the host checked,
// by its ID, with its reads, whose function uses stage 1 recorded and, under the body differential, its bodies as its
// walk rendered them; the host's graph, with the types of the packages it took from the findings store's records
// rather than from their syntax (hostDependence.decoded); and the host's findings by file, before the merge, which the
// differential compares each skipped body's with.
type bodyReachHost struct {
	packages   map[string]*packages.Package
	reads      map[*packages.Package]*build_reads.Reads
	dependence *hostDependence
	findings   map[string][]check.Finding
	// identifiers are the IDs of the host's packages by their types, by which the reach finds how a build loaded the
	// package of an object the host's record names.
	identifiers map[*types.Package]string
}

// newBodyReachHost is the census's host side from the host's whole reads (wholeReads), its graph and its findings.
func newBodyReachHost(
	dependence *hostDependence,
	reads map[*packages.Package]*build_reads.Reads,
	findings []check.Finding,
) *bodyReachHost {
	host := &bodyReachHost{packages: make(map[string]*packages.Package, len(reads)), reads: reads,
		dependence: dependence, findings: findingsByFile(findings),
		identifiers: make(map[*types.Package]string, len(reads))}
	for each := range reads {
		host.packages[each.ID] = each
		if each.Types != nil {
			host.identifiers[each.Types] = each.ID
		}
	}
	return host
}

// reachBuiltPackage is what the census reads of one package a build checked from source, from the build's own syntax,
// taken before its rules let the syntax go: the files it compiles as written, the bodies in the files the host's
// package of its ID compiles too and in those only the build compiles, every file's when the host has no such package,
// the names its build-only files declare at package level, and the paths each shared file imports by a dot.
type reachBuiltPackage struct {
	identifier      string
	files           map[string]bool
	sharedBodies    int
	buildOnlyBodies int
	buildOnlyNames  map[string]bool
	dotImports      map[string][]string
}

// reachBuiltPackages are the packages of loaded's whole graph that it checked from source, as the census reads them
// (reachBuiltPackage), each against hostFiles, the host's files of each package by its ID.
func reachBuiltPackages(loaded *load.Loaded, hostFiles map[string][]string) []*reachBuiltPackage {
	var built []*reachBuiltPackage
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.TypesInfo == nil {
			return
		}
		hosted, isHosted := hostFiles[each.ID]
		read := &reachBuiltPackage{identifier: each.ID, files: make(map[string]bool, len(each.GoFiles)),
			buildOnlyNames: map[string]bool{}, dotImports: map[string][]string{}}
		for _, file := range each.GoFiles {
			read.files[file] = true
		}
		for _, file := range each.Syntax {
			// The file a reader sees, past a cgo output's //line directives, as the host's record and the graph name
			// it.
			name := each.Fset.PositionFor(file.Package, true).Filename
			bodies := syntaxBodies(file)
			if isHosted && slices.Contains(hosted, name) {
				read.sharedBodies += bodies
				for _, imported := range file.Imports {
					if imported.Name != nil && imported.Name.Name == "." {
						read.dotImports[name] = append(read.dotImports[name], strings.Trim(imported.Path.Value, "\"`"))
					}
				}
				continue
			}
			read.buildOnlyBodies += bodies
			if isHosted {
				for _, declared := range build_dependence.PackageLevelNames(file) {
					read.buildOnlyNames[declared] = true
				}
			}
		}
		built = append(built, read)
	})
	return built
}

// syntaxBodies counts file's bodies as stage 1's record counts them (build_reads.DeclarationUses.HasBody): a function
// with a body, and a var or const spec with values, a const spec repeating the one before it taking that one's.
func syntaxBodies(file *ast.File) int {
	bodies := 0
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			if declaration.Body != nil {
				bodies++
			}
		case *ast.GenDecl:
			if declaration.Tok != token.VAR && declaration.Tok != token.CONST {
				continue
			}
			repeatedHasBody := false
			for _, spec := range declaration.Specs {
				valueSpec, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				if declaration.Tok == token.CONST && valueSpec.Type == nil && len(valueSpec.Values) == 0 {
					if repeatedHasBody {
						bodies++
					}
					continue
				}
				repeatedHasBody = len(valueSpec.Values) > 0
				if repeatedHasBody {
					bodies++
				}
			}
		}
	}
	return bodies
}

// buildBodyReach is one build's census: the packages it checked from source, those of them the host recorded and
// those only the build has; their bodies, reached by reason, absent (in the host's files the build does not compile)
// and new (in files only the build compiles, always checked); the recorded packages none of whose bodies is reached
// and none new, which need their declarations alone; and every body of a recorded package, in the order the packages'
// IDs and their records give, which the disagreements with the graph's unit verdict are read from.
type buildBodyReach struct {
	checkedFromSource int
	recorded          int
	buildOnlyPackages int
	total             int
	reached           int
	absent            int
	new               int
	byReason          map[bodyReachReason]int
	declarationsOnly  []string
	bodies            []reachedBody
}

// reach is the census of one build judged as dependence, over built, packages its final load checked from source,
// every such package's ID in fromSource, which the load mode rule reads whether built is all of them or a subset.
func (host *bodyReachHost) reach(
	dependence *build_dependence.Dependence,
	built []*reachBuiltPackage,
	fromSource map[string]bool,
) *buildBodyReach {
	census := &buildBodyReach{checkedFromSource: len(built), byReason: map[bodyReachReason]int{}}
	reacher := &bodyReacher{host: host, dependence: dependence, fromSource: fromSource}
	for _, read := range slices.SortedFunc(slices.Values(built), func(first, second *reachBuiltPackage) int {
		return cmp.Compare(first.identifier, second.identifier)
	}) {
		census.new += read.buildOnlyBodies
		hosted, isHosted := host.packages[read.identifier]
		if !isHosted {
			census.buildOnlyPackages++
			continue
		}
		var record *build_reads.FunctionUses
		isRecorded := false
		if reads := host.reads[hosted]; reads != nil {
			record, isRecorded = reads.FunctionUses()
		}
		if !isRecorded {
			census.total += read.sharedBodies
			census.reached += read.sharedBodies
			census.byReason[reachedUnrecorded] += read.sharedBodies
			continue
		}
		census.recorded++
		bodies, absent := reacher.reachPackage(hosted, record, read)
		census.absent += absent
		census.total += len(bodies)
		packageReached := 0
		for _, body := range bodies {
			if body.reason != "" {
				packageReached++
				census.byReason[body.reason]++
			}
		}
		census.reached += packageReached
		census.bodies = append(census.bodies, bodies...)
		if packageReached == 0 && read.buildOnlyBodies == 0 {
			census.declarationsOnly = append(census.declarationsOnly, read.identifier)
		}
	}
	return census
}
