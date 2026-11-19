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
// `--build-dependence`, and written to that file at the end of the builds, changing no finding and no
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
// A body is reached by the first of: the build's sizes differing from the host's, its package unrecorded, its package's
// bodies judged not the bodies the build counts in the files both compile (Miscounted), a part of it unknown, its
// signature reaching (its body type-checks against the signature), its body reaching, by the first use in the record's
// order that does, and a shadowed name in either part. A package's variable initializers are one body (#bed0mnk): Go
// lowers them into one synthetic function, in the order `types.Info.InitOrder` gives, dependencies first, declaration
// order among the independent and files in the load's order, so an initializer that never returns leaves every one
// after it unreachable, and a rule reading the representation reports nothing there. Where the package holds a body or
// a name one side alone does, or any body of it is reached, every initializer of the package is reached, Initializers,
// since the order and the cut hang on every body of the package an initializer may run (bodyReacher.reachInitializers).
// A package whose files import other paths in the build than on the host, blank imports among them, is reached whole,
// Imports (#bed0mnk): a rule may judge a body by what its package imports anywhere, as unusedwrite stands down where
// any file imports `unsafe`, a gate no use of the body reads. A body whose positions a //line directive moves onto a
// file the builds do not share is reached, LineClaim (#d2mqgfj): a rule may judge a finding by the file its position
// claims, as staticcheck's generated-file filter does (unsharedLineClaims). A build whose word size or maximum
// alignment differs from the host's (buildSizesDiffer) reaches every body of every package, Sizes: `unsafe.Sizeof`, a
// shift past a word or `math.MaxInt` type-check to other constants there with no file differing, which no bit says, as
// the restore's own sizes gate refuses such a build every package (check's hostSizesDiffer, #61qj12v).
//
// What the reach does not stand for, which the census says at its head: a finding that turns on a fact about a
// function a body calls, its purity, that it never returns or that it wraps printf, is stage 4's (#h0k644s), since the
// callee's fact moves with the callee's own body while the callee's bit, its signature, stays unset; and a
// package-wide rule's finding, U1000's or vet's tests', which turns on what the rest of the package holds rather than
// on what the body uses, is stage 5's (#xeafx79). The body differential classes a findings failure that only such
// rules make apart, with its own count and lines (findingStage), rather than failing on it, where the build shows the
// stage's cause for the body (findingCauses): a dependent unit in its package for stage 5's, and for stage 4's nothing
// yet, so each such difference is a failure until the build can show a callee's facts differ (#5vbs1np).

// bodyReachCensusVariable names the file the census is written to, unset for a run that writes none.
const bodyReachCensusVariable = "COHERE_BODY_REACH_CENSUS"

// bodyDifferentialVariable, set to 1, turns on the body differential (build_body_differential.go).
const bodyDifferentialVariable = "COHERE_BODY_DIFFERENTIAL"

// bodyReachReason is why the census calls a body reached in a build.
type bodyReachReason string

const (
	reachedSizes        bodyReachReason = "Sizes"
	reachedUnrecorded   bodyReachReason = "Unrecorded"
	reachedMiscounted   bodyReachReason = "Miscounted"
	reachedUnknown      bodyReachReason = "Unknown"
	reachedSignature    bodyReachReason = "Signature"
	reachedNamed        bodyReachReason = "Named"
	reachedConstant     bodyReachReason = "Constant"
	reachedSelected     bodyReachReason = "Selected"
	reachedValueType    bodyReachReason = "ValueType"
	reachedTypeArgument bodyReachReason = "TypeArgument"
	reachedShadowed     bodyReachReason = "Shadowed"
	reachedLoadMode     bodyReachReason = "LoadMode"
	reachedInitializers bodyReachReason = "Initializers"
	reachedImports      bodyReachReason = "Imports"
	reachedLineClaim    bodyReachReason = "LineClaim"
)

// bodyReachReasons are the reasons in the order the census writes them, the order a body is judged by.
var bodyReachReasons = []bodyReachReason{reachedSizes, reachedUnrecorded, reachedMiscounted, reachedUnknown,
	reachedSignature, reachedNamed, reachedConstant, reachedLoadMode, reachedSelected, reachedValueType,
	reachedTypeArgument, reachedShadowed, reachedLineClaim, reachedImports, reachedInitializers}

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
// the names its build-only files declare at package level, the paths each shared file imports by a dot, by the file
// as parsed, whether a build-only file declares a variable initializer, which reaches every one of the package's
// (bodyReacher.reachInitializers), the paths its files import, by its load's import graph (differingImports), and in
// each shared file, by the file as parsed, the declarations a //line directive moves onto a file the builds do not
// share, each by its offset with the file claimed (unsharedLineClaims).
type reachBuiltPackage struct {
	identifier            string
	files                 map[string]bool
	sharedBodies          int
	buildOnlyBodies       int
	buildOnlyNames        map[string]bool
	dotImports            map[string][]string
	buildOnlyInitializers bool
	imports               map[string]*packages.Package
	lineClaims            map[string]map[int]string
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
			buildOnlyNames: map[string]bool{}, dotImports: map[string][]string{}, imports: each.Imports,
			lineClaims: map[string]map[int]string{}}
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
				parsed := each.Fset.File(file.FileStart).Name()
				if claims := unsharedLineClaims(each.Fset, file, hosted, read.files); len(claims) > 0 {
					read.lineClaims[parsed] = claims
				}
				for _, imported := range file.Imports {
					if imported.Name != nil && imported.Name.Name == "." {
						read.dotImports[parsed] = append(read.dotImports[parsed],
							strings.Trim(imported.Path.Value, "\"`"))
					}
				}
				continue
			}
			read.buildOnlyBodies += bodies
			read.buildOnlyInitializers = read.buildOnlyInitializers || declaresVariableInitializer(file)
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

// unsharedLineClaims are the declarations of file, a file both builds compile, by the offset the record places each at,
// a function's at its `func` and a value spec's at its first name, whose positions a //line directive moves onto a file
// the builds do not share, one of hostFiles and builtFiles holding it and the other not, each with that file
// (#d2mqgfj). A rule reads the file a position claims: staticcheck's generated-file filter asks whether that file is
// one the package compiles as generated, so a body claiming a platform sibling is judged by a file only one build
// holds, though nothing it uses differs. That filter asks the pass's own package, and it is the one reader of a claimed
// file: a finding itself is placed by the file as parsed (check.newFinding), so the readers that key a finding's file
// across the run, check's withholding of house findings in generated and copied files and the suppression's directives,
// never read a claim, which is why this package's own files are the whole question. A claim is read from the position
// after its directive, as the scanner made it, and holds to the next directive or the file's end; a declaration any
// claim it overlaps names such a file is claimed, a whole var or const group when any of it is, a const spec repeating
// the expressions before it. The file's bytes are the same in both builds, so its claims are too, and a claim of a file
// neither build compiles is judged alike in both: no compiled file answers for it.
func unsharedLineClaims(
	fileSet *token.FileSet,
	file *ast.File,
	hostFiles []string,
	builtFiles map[string]bool,
) map[int]string {
	tokenFile := fileSet.File(file.FileStart)
	// spans are each claim's start, in the file's offsets, and the file it claims, in the file's order.
	type span struct {
		start   int
		claimed string
	}
	var spans []span
	for _, group := range file.Comments {
		for _, comment := range group.List {
			start := comment.End()
			switch {
			case strings.HasPrefix(comment.Text, "/*line "):
			case strings.HasPrefix(comment.Text, "//line "):
				// A line comment's directive moves the line after it, found by the directive's own line as written:
				// File.Line reads it as a directive before it moved it, and LineStart takes none but the written.
				line := tokenFile.PositionFor(comment.End(), false).Line
				if line >= tokenFile.LineCount() {
					continue
				}
				start = tokenFile.LineStart(line + 1)
			default:
				continue
			}
			spans = append(spans, span{start: tokenFile.Offset(start),
				claimed: fileSet.PositionFor(start, true).Filename})
		}
	}
	claims := map[int]string{}
	for index, each := range spans {
		if each.claimed == tokenFile.Name() || slices.Contains(hostFiles, each.claimed) == builtFiles[each.claimed] {
			continue
		}
		end := tokenFile.Size()
		if index+1 < len(spans) {
			end = spans[index+1].start
		}
		for _, declaration := range file.Decls {
			if tokenFile.Offset(declaration.End()) <= each.start || tokenFile.Offset(declaration.Pos()) >= end {
				continue
			}
			switch declaration := declaration.(type) {
			case *ast.FuncDecl:
				claims[tokenFile.Offset(declaration.Pos())] = each.claimed
			case *ast.GenDecl:
				for _, spec := range declaration.Specs {
					if valueSpec, isValue := spec.(*ast.ValueSpec); isValue {
						claims[tokenFile.Offset(valueSpec.Pos())] = each.claimed
					}
				}
			}
		}
	}
	return claims
}

// declaresVariableInitializer reports whether file declares a package-level variable with a value, which the
// package's one synthetic initializing function runs.
func declaresVariableInitializer(file *ast.File) bool {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			if valueSpec, isValue := spec.(*ast.ValueSpec); isValue && len(valueSpec.Values) > 0 {
				return true
			}
		}
	}
	return false
}

// buildBodyReach is one build's census: the packages it checked from source, those of them the host recorded and
// those only the build has; their bodies, reached by reason, absent (in the host's files the build does not compile)
// and new (in files only the build compiles, always checked); the recorded packages none of whose bodies is reached
// and none new, which need their declarations alone; the recorded packages whose judged bodies are not the bodies the
// build counts in the files both compile, each reached whole; and every body of a recorded package, in the order the
// packages' IDs and their records give, which the disagreements with the graph's unit verdict are read from.
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
	miscounted        []string
	bodies            []reachedBody
}

// reach is the census of one build judged as dependence, over built, packages its final load checked from source,
// every such package's ID in fromSource, which the load mode rule reads whether built is all of them or a subset, and
// with sizesDiffer every body reached by the build's sizes (buildSizesDiffer).
func (host *bodyReachHost) reach(
	dependence *build_dependence.Dependence,
	built []*reachBuiltPackage,
	fromSource map[string]bool,
	sizesDiffer bool,
) *buildBodyReach {
	reacher := &bodyReacher{host: host, dependence: dependence, fromSource: fromSource, sizesDiffer: sizesDiffer,
		carried: map[types.Object]bool{}}
	sorted := slices.SortedFunc(slices.Values(built), func(first, second *reachBuiltPackage) int {
		return cmp.Compare(first.identifier, second.identifier)
	})
	// A value spec the load mode reaches carries it to its objects' users (bodyReacher.carryLoadMode), and so does
	// every value of an unrecorded package (bodyReacher.carryUnrecorded), whose users may be judged before it, in its
	// package or another, so the build is judged again until a judgment carries nothing new.
	for {
		reacher.carriedGrew = false
		census := host.judge(reacher, sorted)
		if !reacher.carriedGrew {
			return census
		}
	}
}

// judge is the census of reacher's build over built, packages its final load checked from source, sorted by ID.
func (host *bodyReachHost) judge(reacher *bodyReacher, built []*reachBuiltPackage) *buildBodyReach {
	census := &buildBodyReach{checkedFromSource: len(built), byReason: map[bodyReachReason]int{}}
	for _, read := range built {
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
			reacher.carryUnrecorded(hosted.Types)
			census.total += read.sharedBodies
			census.reached += read.sharedBodies
			census.byReason[cmp.Or(reacher.sizesReason(), reachedUnrecorded)] += read.sharedBodies
			continue
		}
		census.recorded++
		bodies, absent := reacher.reachPackage(hosted, record, read)
		census.absent += absent
		// The bodies judged must be every body the build counts in the files both compile, or one of them was
		// counted absent, or judged in a file the build does not compile, so none is left unreached: the package is
		// reached whole, its count the build's.
		if len(bodies) != read.sharedBodies {
			census.miscounted = append(census.miscounted, read.identifier)
			reason := cmp.Or(reacher.sizesReason(), reachedMiscounted)
			for index := range bodies {
				bodies[index].reason, bodies[index].constantsOnlyReason = reason, reason
				bodies[index].driver = "the package's bodies miscounted"
			}
			census.total += read.sharedBodies
			census.reached += read.sharedBodies
			census.byReason[reason] += read.sharedBodies
			census.bodies = append(census.bodies, bodies...)
			continue
		}
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

// buildSizesDiffer reports whether built, a build's load, checked its packages under other sizes than host, the host's
// load, by the measure the restore's sizes gate compares (check.MeasureOf, #61qj12v): a word size or a maximum
// alignment apart, or either load checked under no sizes, which nothing says are alike.
func buildSizesDiffer(host *load.Loaded, built *load.Loaded) bool {
	hostSizes, builtSizes := loadSizes(host), loadSizes(built)
	if hostSizes == nil || builtSizes == nil {
		return true
	}
	hostWordSize, hostMaximumAlignment := check.MeasureOf(hostSizes)
	wordSize, maximumAlignment := check.MeasureOf(builtSizes)
	return wordSize != hostWordSize || maximumAlignment != hostMaximumAlignment
}

// loadSizes are the sizes loaded's packages were checked by (check.LoadedSizes), nil when it checked none.
func loadSizes(loaded *load.Loaded) types.Sizes {
	var every []*packages.Package
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) { every = append(every, each) })
	return check.LoadedSizes(every)
}
