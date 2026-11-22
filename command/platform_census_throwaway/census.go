package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// sampleCapacity is how many sampled forks each group keeps across every build, fifty in all.
var sampleCapacity = map[string]int{groupOurs: 20, groupDependencies: 10, groupStandardLibrary: 20}

// topPackagesKept is how many packages a build's differing declarations are listed by.
const topPackagesKept = 20

// initializersSuffix ends the identity of a package's variable initializers, counted as one function.
const initializersSuffix = "#initializers"

// expressionKey names an expression across builds: its file, its byte offsets, and its syntax node's kind, since two
// nodes could in principle span the same bytes.
type expressionKey struct {
	file  string
	start int
	end   int
	kind  reflect.Type
}

// functionRange is a function's body, or a package's variable initializer, in one file, by byte offsets.
type functionRange struct {
	start int
	end   int
	// identity names the function across builds and test variants; display is what a reader sees.
	identity string
	display  string
}

// variantSet is the distinct answers one forked declaration or expression has across the host and every build.
type variantSet struct {
	group   string
	answers map[string]bool
}

// census holds what accumulates across every build.
type census struct {
	host     *loadedBuild
	report   *censusReport
	progress io.Writer
	// hostFunctions is each host file's functions, read once.
	hostFunctions map[string][]functionRange
	// fileOwner is the package each host file is counted under: the first by id that compiles it.
	fileOwner           map[string]string
	declarationVariants map[declarationKey]*variantSet
	expressionVariants  map[expressionKey]*variantSet
	// forkedFunctions maps each function forked in any build, by identity, to its group.
	forkedFunctions map[string]string
	// samples are each group's reservoir of forks, and forksSeen how many it has drawn from.
	samples   map[string][]forkRecord
	forksSeen map[string]int
	random    *rand.Rand
	// lineCounts and sources cache each file's line count and bytes.
	lineCounts map[string]int
	sources    map[string][]byte
}

// takeCensus loads the host and each build of asked, and counts each build against the host.
func takeCensus(asked commandLine, progress io.Writer) (*censusReport, error) {
	root, absoluteError := filepath.Abs(asked.root)
	if absoluteError != nil {
		return nil, absoluteError
	}
	side, sideError := sideWorkFile(root)
	if sideError != nil {
		return nil, fmt.Errorf("copying the go.work aside: %w", sideError)
	}
	defer side.remove()
	report := &censusReport{
		Root:                root,
		Patterns:            asked.patterns,
		Tests:               asked.tests,
		Workspace:           "GOWORK=off, the tree holds no go.work",
		StandardLibraryRule: standardLibraryRule,
		OverAll:             map[string]*overAllCounts{},
	}
	if side.directory != "" {
		report.Workspace = "a side copy of the tree's go.work, every directory made absolute"
	}
	say(progress, "platform-census: loading the host %s\n", asked.host.target(asked.host.cgo == "1"))
	host, hostError := loadBuild(root, asked.patterns, asked.tests, asked.host, side.workFile)
	if hostError != nil {
		return nil, hostError
	}
	taken := &census{
		host:                host,
		report:              report,
		progress:            progress,
		hostFunctions:       map[string][]functionRange{},
		fileOwner:           map[string]string{},
		declarationVariants: map[declarationKey]*variantSet{},
		expressionVariants:  map[expressionKey]*variantSet{},
		forkedFunctions:     map[string]string{},
		samples:             map[string][]forkRecord{},
		forksSeen:           map[string]int{},
		// A fixed seed draws the same samples from the same forks in the same order.
		random:     rand.New(rand.NewPCG(7, 95)),
		lineCounts: map[string]int{},
		sources:    map[string][]byte{},
	}
	report.Host = configurationReportOf(host)
	for _, name := range groupNames {
		report.OverAll[name] = &overAllCounts{DeclarationVariants: map[int]int{}, ExpressionVariants: map[int]int{}}
	}
	taken.countHost()
	for _, asked := range asked.builds {
		say(progress, "platform-census: loading %s\n", asked.target(asked.cgo == "1"))
		build, buildError := loadBuild(root, report.Patterns, report.Tests, asked, side.workFile)
		if buildError != nil {
			return nil, buildError
		}
		started := time.Now()
		counted := taken.compare(build)
		counted.Configuration.CompareSeconds = time.Since(started).Seconds()
		report.Builds = append(report.Builds, counted)
		say(progress, "platform-census: %s loaded in %.1fs and compared in %.1fs\n",
			counted.Configuration.Target, counted.Configuration.LoadSeconds, counted.Configuration.CompareSeconds)
		// The build's whole graph is garbage now, and the next build's load needs the room.
		build = nil
		runtime.GC()
		debug.FreeOSMemory()
	}
	taken.finish()
	return report, nil
}

// configurationReportOf is how loaded was loaded.
func configurationReportOf(loaded *loadedBuild) configurationReport {
	return configurationReport{
		Target:        loaded.configuration.target(loaded.cgoEnabled),
		CgoEnabled:    loaded.cgoEnabled,
		CgoChoice:     loaded.configuration.cgoChoice(),
		Packages:      len(loaded.packagesByIdentifier),
		PackageErrors: loaded.packageErrors,
		FirstErrors:   loaded.firstErrors,
		LoadSeconds:   loaded.loadDuration.Seconds(),
	}
}

// countHost counts the host's own expressions and functions, each file once under the first package by id that
// compiles it, which over every build is what a fork is a share of.
func (taken *census) countHost() {
	initializersCounted := map[string]bool{}
	for _, hostPackage := range taken.host.sortedPackages {
		for _, file := range hostPackage.CompiledGoFiles {
			if _, owned := taken.fileOwner[file]; !owned {
				taken.fileOwner[file] = hostPackage.ID
			}
		}
		if hostPackage.TypesInfo == nil {
			continue
		}
		overAll := taken.report.OverAll[taken.host.group(hostPackage)]
		for expression := range hostPackage.TypesInfo.Types {
			if taken.fileOwner[taken.host.fileName(expression.Pos())] == hostPackage.ID {
				overAll.HostExpressions++
			}
		}
		for _, syntax := range hostPackage.Syntax {
			file := taken.host.fileName(syntax.Pos())
			if taken.fileOwner[file] != hostPackage.ID {
				continue
			}
			for _, function := range taken.functionsOf(hostPackage, syntax) {
				if strings.HasSuffix(function.identity, initializersSuffix) {
					// A package's initializers are one function however many files hold them.
					if initializersCounted[function.identity] {
						continue
					}
					initializersCounted[function.identity] = true
				}
				overAll.HostFunctions++
			}
		}
	}
}

// functionsOf is syntax's functions as the host compiles it: each function declaration's body, and, as one more, each
// package-level variable's initializer, read once per file.
func (taken *census) functionsOf(hostPackage *packages.Package, syntax *ast.File) []functionRange {
	tokenFile := taken.host.fileSet.File(syntax.Pos())
	if cached, read := taken.hostFunctions[tokenFile.Name()]; read {
		return cached
	}
	var ranges []functionRange
	for _, declaration := range syntax.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if typed.Body == nil {
				continue
			}
			start := tokenFile.Offset(typed.Body.Pos())
			ranges = append(ranges, functionRange{
				start:    start,
				end:      tokenFile.Offset(typed.Body.End()),
				identity: tokenFile.Name() + ":" + strconv.Itoa(start),
				display:  hostPackage.PkgPath + "." + functionName(typed),
			})
		case *ast.GenDecl:
			if typed.Tok != token.VAR {
				continue
			}
			for _, specification := range typed.Specs {
				// A var declaration's specifications are value specifications; anything else is not an initializer.
				valueSpecification, isValue := specification.(*ast.ValueSpec)
				if !isValue || len(valueSpecification.Values) == 0 {
					continue
				}
				values := valueSpecification.Values
				ranges = append(ranges, functionRange{
					start:    tokenFile.Offset(values[0].Pos()),
					end:      tokenFile.Offset(values[len(values)-1].End()),
					identity: hostPackage.PkgPath + initializersSuffix,
					display:  hostPackage.PkgPath + initializersSuffix,
				})
			}
		}
	}
	taken.hostFunctions[tokenFile.Name()] = ranges
	return ranges
}

// functionName is a function declaration's name, `T.m` for a method.
func functionName(declaration *ast.FuncDecl) string {
	if declaration.Recv == nil || len(declaration.Recv.List) == 0 {
		return declaration.Name.Name
	}
	receiver := declaration.Recv.List[0].Type
	// A pointer receiver, then a generic type's parameters, wrap the type's name.
	if star, isStar := receiver.(*ast.StarExpr); isStar {
		receiver = star.X
	}
	switch typed := receiver.(type) {
	case *ast.IndexExpr:
		receiver = typed.X
	case *ast.IndexListExpr:
		receiver = typed.X
	}
	if identifier, isIdentifier := receiver.(*ast.Ident); isIdentifier {
		return identifier.Name + "." + declaration.Name.Name
	}
	return declaration.Name.Name
}

// pairing is one build's packages set against the host's: those both hold by id, in the host's order, and each
// side's own.
type pairing struct {
	paired      [][2]*packages.Package
	onlyOnHost  []*packages.Package
	onlyInBuild []*packages.Package
}

// pair sets build's packages against the host's by id, test variants included.
func (taken *census) pair(build *loadedBuild) pairing {
	var paired pairing
	for _, hostPackage := range taken.host.sortedPackages {
		buildPackage, held := build.packagesByIdentifier[hostPackage.ID]
		if !held {
			paired.onlyOnHost = append(paired.onlyOnHost, hostPackage)
			continue
		}
		paired.paired = append(paired.paired, [2]*packages.Package{hostPackage, buildPackage})
	}
	for _, buildPackage := range build.sortedPackages {
		if _, held := taken.host.packagesByIdentifier[buildPackage.ID]; !held {
			paired.onlyInBuild = append(paired.onlyInBuild, buildPackage)
		}
	}
	return paired
}

// compare counts build against the host.
func (taken *census) compare(build *loadedBuild) buildReport {
	counted := buildReport{Configuration: configurationReportOf(build), Groups: map[string]*groupCounts{}}
	for _, name := range groupNames {
		counted.Groups[name] = &groupCounts{
			PackagesOnlyOnHost:  []string{},
			PackagesOnlyInBuild: []string{},
			differingByPackage:  map[string]int{},
			functions:           map[string]bool{},
			forkedFunctions:     map[string]string{},
			packagesWithFork:    map[string]bool{},
			packagePaths:        map[string]bool{},
		}
	}
	paired := taken.pair(build)
	for _, hostPackage := range paired.onlyOnHost {
		group := counted.Groups[taken.host.group(hostPackage)]
		group.PackagesOnlyOnHost = append(group.PackagesOnlyOnHost, hostPackage.ID)
	}
	for _, buildPackage := range paired.onlyInBuild {
		group := counted.Groups[build.group(buildPackage)]
		group.PackagesOnlyInBuild = append(group.PackagesOnlyInBuild, buildPackage.ID)
	}
	differing := taken.compareDeclarations(build, paired, counted)
	assigned := taken.compareFiles(paired, counted)
	for _, both := range paired.paired {
		taken.compareExpressions(build, both[0], both[1], assigned[both[0].ID], differing, counted)
	}
	for _, group := range counted.Groups {
		group.DifferingDeclarationShare = share(group.DifferingDeclarations, group.Declarations)
		group.ForkedExpressionShare = share(group.ForkedExpressions, group.AlignedExpressions)
		group.Functions = len(group.functions)
		group.ForkedFunctions = len(group.forkedFunctions)
		group.ForkedFunctionShare = share(group.ForkedFunctions, group.Functions)
		group.PackagesWithFork = len(group.packagesWithFork)
		group.PackagePaths = len(group.packagePaths)
		displays := map[string]bool{}
		for _, display := range group.forkedFunctions {
			displays[display] = true
		}
		group.ForkedFunctionNames = sortedKeys(displays)
		sort.Strings(group.DifferingDeclarationNames)
		group.DifferingByPackage = topPackages(group.differingByPackage)
	}
	return counted
}

// compareDeclarations counts the paired packages' declarations, each once by package path and name, and returns those
// that differ: present on one side only, or with a different answer.
//
// Mutates: counted gets each declaration counted in its groups.
func (taken *census) compareDeclarations(
	build *loadedBuild,
	paired pairing,
	counted buildReport,
) map[declarationKey]bool {
	differing := map[declarationKey]bool{}
	seen := map[declarationKey]bool{}
	for _, both := range paired.paired {
		groupName := taken.host.group(both[0])
		group := counted.Groups[groupName]
		group.PairedPackages++
		group.packagePaths[both[0].PkgPath] = true
		// Both sides are read whole, so every member of every named type is owned before any expression is classified.
		hostAnswers := taken.host.declarationsOf(both[0])
		buildAnswers := build.declarationsOf(both[1])
		names := map[string]bool{}
		for name := range hostAnswers {
			names[name] = true
		}
		for name := range buildAnswers {
			names[name] = true
		}
		for name := range names {
			key := declarationKey{packagePath: both[0].PkgPath, name: name}
			if seen[key] {
				continue
			}
			seen[key] = true
			group.Declarations++
			hostAnswer, onHost := hostAnswers[name]
			buildAnswer, inBuild := buildAnswers[name]
			if onHost && inBuild && hostAnswer == buildAnswer {
				continue
			}
			if !onHost {
				hostAnswer = absentAnswer
			}
			if !inBuild {
				buildAnswer = absentAnswer
			}
			differing[key] = true
			group.DifferingDeclarations++
			group.differingByPackage[key.packagePath]++
			group.DifferingDeclarationNames = append(group.DifferingDeclarationNames, key.packagePath+"."+name)
			variants := taken.declarationVariants[key]
			if variants == nil {
				variants = &variantSet{group: groupName, answers: map[string]bool{}}
				taken.declarationVariants[key] = variants
			}
			variants.answers[hostAnswer] = true
			variants.answers[buildAnswer] = true
		}
	}
	return differing
}

// compareFiles counts the files one side of a paired package compiles and the other does not, each file once, and
// returns, by host package id, the files both sides compile that the package is the first to hold, whose expressions
// it aligns.
//
// Mutates: counted gets each file counted in its groups.
func (taken *census) compareFiles(paired pairing, counted buildReport) map[string][]string {
	assigned := map[string][]string{}
	seen := map[string]bool{}
	for _, both := range paired.paired {
		group := counted.Groups[taken.host.group(both[0])]
		hostFiles := setOf(both[0].CompiledGoFiles)
		buildFiles := setOf(both[1].CompiledGoFiles)
		for _, file := range both[0].CompiledGoFiles {
			if seen[file] {
				continue
			}
			seen[file] = true
			if buildFiles[file] {
				group.FilesShared++
				assigned[both[0].ID] = append(assigned[both[0].ID], file)
				continue
			}
			taken.countFile(&group.FilesOnlyOnHost, file, both[0])
		}
		for _, file := range both[1].CompiledGoFiles {
			if seen[file] || hostFiles[file] {
				continue
			}
			seen[file] = true
			taken.countFile(&group.FilesOnlyInBuild, file, both[1])
		}
	}
	return assigned
}

// countFile adds file, which owner compiles, to counted.
//
// Mutates: counted gets the file and its lines.
func (taken *census) countFile(counted *fileCount, file string, owner *packages.Package) {
	counted.Files++
	counted.Lines += taken.lineCount(file)
	if !slices.Contains(owner.GoFiles, file) {
		counted.OutsidePackageDirectory++
	}
}

// compareExpressions aligns the expressions of files, which both hostPackage and buildPackage compile, by file and
// offsets, and counts those whose answer differs, classing each by whether it uses a declaration in differing.
//
// Mutates: counted gets each expression counted in its groups.
func (taken *census) compareExpressions(
	build *loadedBuild,
	hostPackage *packages.Package,
	buildPackage *packages.Package,
	files []string,
	differing map[declarationKey]bool,
	counted buildReport,
) {
	groupName := taken.host.group(hostPackage)
	group := counted.Groups[groupName]
	if len(files) == 0 {
		return
	}
	if hostPackage.TypesInfo == nil || buildPackage.TypesInfo == nil {
		group.PackagesNotCompared = append(group.PackagesNotCompared, hostPackage.ID)
		return
	}
	compared := setOf(files)
	for _, syntax := range hostPackage.Syntax {
		if !compared[taken.host.fileName(syntax.Pos())] {
			continue
		}
		for _, function := range taken.functionsOf(hostPackage, syntax) {
			// By identity, so a package's initializers are one function however many files and variants hold them.
			group.functions[function.identity] = true
		}
	}
	buildIndex := map[expressionKey]ast.Expr{}
	for expression := range buildPackage.TypesInfo.Types {
		if key, inFiles := spanOf(build, expression, compared); inFiles {
			buildIndex[key] = expression
		}
	}
	for hostExpression, hostTypeAndValue := range hostPackage.TypesInfo.Types {
		key, inFiles := spanOf(taken.host, hostExpression, compared)
		if !inFiles {
			continue
		}
		buildExpression, aligned := buildIndex[key]
		if !aligned {
			group.ExpressionsOnlyOnHost++
			continue
		}
		delete(buildIndex, key)
		group.AlignedExpressions++
		hostAnswer := taken.host.answerOf(hostTypeAndValue)
		buildAnswer := build.answerOf(buildPackage.TypesInfo.Types[buildExpression])
		if hostAnswer == buildAnswer {
			continue
		}
		fork := forkRecord{
			Build:       counted.Configuration.Target,
			Group:       groupName,
			File:        key.file,
			Line:        taken.host.fileSet.Position(hostExpression.Pos()).Line,
			Start:       key.start,
			End:         key.end,
			HostAnswer:  hostAnswer,
			BuildAnswer: buildAnswer,
			Class:       classLocal,
		}
		group.ForkedExpressions++
		byReference := usesDiffering(taken.host, hostPackage, hostExpression, differing) ||
			usesDiffering(build, buildPackage, buildExpression, differing)
		if !byReference && typedByContext(hostTypeAndValue, buildPackage.TypesInfo.Types[buildExpression]) {
			byReference = contextUsesDiffering(taken.host, hostPackage, hostExpression, differing) ||
				contextUsesDiffering(build, buildPackage, buildExpression, differing)
		}
		if byReference {
			fork.Class = classByReference
			group.ForkedByReference++
		} else {
			group.ForkedLocal++
		}
		group.packagesWithFork[hostPackage.PkgPath] = true
		if function, inFunction := taken.enclosingFunction(key); inFunction {
			fork.Function = function.display
			group.forkedFunctions[function.identity] = function.display
			taken.forkedFunctions[function.identity] = groupName
		} else {
			group.ForkPointsOutsideFunctions++
		}
		variants := taken.expressionVariants[key]
		if variants == nil {
			variants = &variantSet{group: groupName, answers: map[string]bool{}}
			taken.expressionVariants[key] = variants
		}
		variants.answers[hostAnswer] = true
		variants.answers[buildAnswer] = true
		if groupName == groupOurs {
			group.Forks = append(group.Forks, fork)
		}
		taken.sample(fork)
	}
	group.ExpressionsOnlyInBuild += len(buildIndex)
}

// spanOf is expression's key when its file is among files.
func spanOf(loaded *loadedBuild, expression ast.Expr, files map[string]bool) (expressionKey, bool) {
	tokenFile := loaded.fileSet.File(expression.Pos())
	if tokenFile == nil || !files[tokenFile.Name()] {
		return expressionKey{}, false
	}
	return expressionKey{
		file:  tokenFile.Name(),
		start: tokenFile.Offset(expression.Pos()),
		end:   tokenFile.Offset(expression.End()),
		kind:  reflect.TypeOf(expression),
	}, true
}

// usesDiffering says whether expression's subtree holds an identifier or a selector whose used object is one of
// differing.
func usesDiffering(
	loaded *loadedBuild,
	owner *packages.Package,
	expression ast.Expr,
	differing map[declarationKey]bool,
) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		var used types.Object
		switch typed := node.(type) {
		case *ast.Ident:
			used = owner.TypesInfo.Uses[typed]
		case *ast.SelectorExpr:
			if selection := owner.TypesInfo.Selections[typed]; selection != nil {
				used = selection.Obj()
			}
		}
		if key, isDeclaration := loaded.declarationOf(used); isDeclaration && differing[key] {
			found = true
		}
		return !found
	})
	return found
}

// typedByContext says whether two answers of one expression differ only in a type its context gave it: an untyped
// constant of one value on both sides, or nil, each of which go/types records with the type it converts to. Such a fork
// holds nothing of its own that differs, as `syscall.S_IFMT` in `status.Mode&syscall.S_IFMT` takes Mode's type.
func typedByContext(host types.TypeAndValue, build types.TypeAndValue) bool {
	if host.Value != nil && build.Value != nil {
		return host.Value.ExactString() == build.Value.ExactString()
	}
	return host.IsNil() && build.IsNil()
}

// contextUsesDiffering says whether what gives expression its type uses one of differing: an expression around it, or,
// where those end at a statement, the other side of an assignment, a variable's declared type, a channel sent on, or
// the results of the function a return statement leaves.
func contextUsesDiffering(
	loaded *loadedBuild,
	owner *packages.Package,
	expression ast.Expr,
	differing map[declarationKey]bool,
) bool {
	var file *ast.File
	for _, syntax := range owner.Syntax {
		if syntax.FileStart <= expression.Pos() && expression.End() <= syntax.FileEnd {
			file = syntax
			break
		}
	}
	if file == nil {
		return false
	}
	path, _ := astutil.PathEnclosingInterval(file, expression.Pos(), expression.End())
	for index, node := range path {
		switch typed := node.(type) {
		case ast.Expr:
			if usesDiffering(loaded, owner, typed, differing) {
				return true
			}
			continue
		case *ast.AssignStmt:
			for _, left := range typed.Lhs {
				if usesDiffering(loaded, owner, left, differing) {
					return true
				}
			}
		case *ast.ValueSpec:
			return typed.Type != nil && usesDiffering(loaded, owner, typed.Type, differing)
		case *ast.SendStmt:
			return usesDiffering(loaded, owner, typed.Chan, differing)
		case *ast.ReturnStmt:
			return resultsUseDiffering(loaded, owner, path[index+1:], differing)
		}
		return false
	}
	return false
}

// resultsUseDiffering says whether the results of the innermost function in outer, a return statement's enclosing
// nodes, use one of differing.
func resultsUseDiffering(
	loaded *loadedBuild,
	owner *packages.Package,
	outer []ast.Node,
	differing map[declarationKey]bool,
) bool {
	for _, node := range outer {
		switch typed := node.(type) {
		case *ast.FuncLit:
			return typed.Type.Results != nil && usesDiffering(loaded, owner, typed.Type, differing)
		case *ast.FuncDecl:
			return typed.Type.Results != nil && usesDiffering(loaded, owner, typed.Type, differing)
		}
	}
	return false
}

// enclosingFunction is the host function whose body or initializer holds key.
func (taken *census) enclosingFunction(key expressionKey) (functionRange, bool) {
	for _, function := range taken.hostFunctions[key.file] {
		if key.start >= function.start && key.end <= function.end {
			return function, true
		}
	}
	return functionRange{}, false
}

// sample keeps fork in its group's reservoir, so every fork of a group across every build is as likely to be kept.
func (taken *census) sample(fork forkRecord) {
	taken.forksSeen[fork.Group]++
	kept := taken.samples[fork.Group]
	if len(kept) < sampleCapacity[fork.Group] {
		taken.samples[fork.Group] = append(kept, fork)
		return
	}
	if replaced := taken.random.IntN(taken.forksSeen[fork.Group]); replaced < len(kept) {
		kept[replaced] = fork
	}
}

// finish totals every build into the report's over all counts, and spells each kept fork's expression from its file.
func (taken *census) finish() {
	report := taken.report
	for key, variants := range taken.declarationVariants {
		overAll := report.OverAll[variants.group]
		overAll.DifferingDeclarations++
		overAll.DeclarationVariants[len(variants.answers)]++
		if variants.group == groupOurs {
			overAll.Variants = append(overAll.Variants, variantRecord{
				Kind:    "declaration",
				Key:     key.packagePath + "." + key.name,
				Answers: sortedKeys(variants.answers),
			})
		}
	}
	for key, variants := range taken.expressionVariants {
		overAll := report.OverAll[variants.group]
		overAll.ForkedExpressions++
		overAll.ExpressionVariants[len(variants.answers)]++
		if variants.group == groupOurs {
			spelled := fmt.Sprintf("%s:%d-%d %s", key.file, key.start, key.end,
				taken.expressionText(key.file, key.start, key.end))
			overAll.Variants = append(overAll.Variants,
				variantRecord{Kind: "expression", Key: spelled, Answers: sortedKeys(variants.answers)})
		}
	}
	for _, group := range taken.forkedFunctions {
		report.OverAll[group].ForkedFunctions++
	}
	for _, overAll := range report.OverAll {
		overAll.ForkedExpressionShare = share(overAll.ForkedExpressions, overAll.HostExpressions)
		overAll.ForkedFunctionShare = share(overAll.ForkedFunctions, overAll.HostFunctions)
		sort.Slice(overAll.Variants, func(left int, right int) bool {
			return overAll.Variants[left].Key < overAll.Variants[right].Key
		})
	}
	for _, build := range report.Builds {
		// By index, since a range would spell a copy of each.
		forks := build.Groups[groupOurs].Forks
		for index := range forks {
			forks[index].Expression = taken.expressionText(forks[index].File, forks[index].Start, forks[index].End)
		}
	}
	for _, name := range groupNames {
		for _, fork := range taken.samples[name] {
			fork.Expression = taken.expressionText(fork.File, fork.Start, fork.End)
			report.Samples = append(report.Samples, fork)
		}
	}
	report.Notes = censusNotes
}

// expressionTextLimit is how much of an expression's source a record spells.
const expressionTextLimit = 160

// expressionText is file's source from start to end, its whitespace folded, cut at expressionTextLimit.
func (taken *census) expressionText(file string, start int, end int) string {
	source := taken.source(file)
	if end > len(source) || start > end {
		return ""
	}
	folded := strings.Join(strings.Fields(string(source[start:end])), " ")
	if len(folded) > expressionTextLimit {
		folded = folded[:expressionTextLimit] + "..."
	}
	return folded
}

// source is file's bytes, read once, or none when it cannot be read.
func (taken *census) source(file string) []byte {
	if cached, read := taken.sources[file]; read {
		return cached
	}
	contents, readError := os.ReadFile(file)
	if readError != nil {
		contents = nil
	}
	taken.sources[file] = contents
	return contents
}

// lineCount is how many lines file holds, a last line with no newline counted.
func (taken *census) lineCount(file string) int {
	if cached, counted := taken.lineCounts[file]; counted {
		return cached
	}
	contents, readError := os.ReadFile(file)
	lines := 0
	if readError == nil {
		lines = strings.Count(string(contents), "\n")
		if len(contents) != 0 && contents[len(contents)-1] != '\n' {
			lines++
		}
	}
	taken.lineCounts[file] = lines
	return lines
}

// share is part over whole, 0 when whole is.
func share(part int, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// setOf is values as a set.
func setOf(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

// sortedKeys is set's members, sorted.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// topPackages is counts' packages by count, most first, the first topPackagesKept.
func topPackages(counts map[string]int) []packageCount {
	listed := make([]packageCount, 0, len(counts))
	for name, count := range counts {
		listed = append(listed, packageCount{Package: name, Count: count})
	}
	sort.Slice(listed, func(left int, right int) bool {
		if listed[left].Count != listed[right].Count {
			return listed[left].Count > listed[right].Count
		}
		return listed[left].Package < listed[right].Package
	})
	if len(listed) > topPackagesKept {
		listed = listed[:topPackagesKept]
	}
	return listed
}

// say writes one progress message to progress.
func say(progress io.Writer, format string, arguments ...any) {
	// Ignored: the census is in its table, its json and its exit code, which a closed terminal does not change.
	_, _ = fmt.Fprintf(progress, format, arguments...)
}
