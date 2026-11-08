package check

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/platform"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// ReadBuild is how one build reads what its workspace imports and what its workspace packages give their importers,
// so an extra build can be compared with the host's (#72n9mq0): the fact analyzers among rules and below them, as
// newCacheAnalyzers finds them, run over every package loaded's analyzed packages reach, and nothing else. Every
// package outside the workspace, the packages loaded's patterns matched and their test variants, has each object
// keyed (platform.KeyOf's key) by the digest of its rendering and its facts (see objectDigest), and its own facts
// under its key with an empty Path. Every workspace package has its output digest, by load ID, as a cache entry of it
// would hold.
//
// A fact analyzer that did not finish on a package is part of how the build reads that package: every digest of the
// package holds the action's error, so a build that fails there reads it unlike one that does not. It returns an
// error only when the rules cannot be run, or a fact cannot be digested, so the read cannot say what the build holds.
//
// Every package reached is given to the checker, so none comes back without its syntax and type information, and
// the same load can be run or read again.
func ReadBuild(loaded *load.Loaded, rules []rule.Rule) (platform.ObjectDigests, platform.PackageDigests, error) {
	ruleOf := make(map[*analysis.Analyzer]rule.Rule, len(rules))
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		if enabled.Analyzer == nil {
			return nil, nil, fmt.Errorf("reading the build: rule %s has no analyzer", enabled.Name)
		}
		ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	if wrapError := wrapRuns(analyzers); wrapError != nil {
		return nil, nil, wrapError
	}
	known, knownError := newCacheAnalyzers(ruleOf, analyzers)
	if knownError != nil {
		return nil, nil, fmt.Errorf("reading the build: %w", knownError)
	}
	var reached []*packages.Package
	packages.Visit(loaded.Packages, nil, func(visited *packages.Package) { reached = append(reached, visited) })
	graph, analyzeError := checker.Analyze(known.facts, reached, &checker.Options{})
	if analyzeError != nil {
		return nil, nil, fmt.Errorf("reading the build: analyzing %d packages: %w", len(reached), analyzeError)
	}
	actionsOf := map[*packages.Package][]*checker.Action{}
	for _, action := range graph.Roots {
		actionsOf[action.Package] = append(actionsOf[action.Package], action)
	}
	workspace := map[string]bool{}
	for _, root := range loaded.Roots {
		workspace[root.PkgPath] = true
	}
	objects := platform.ObjectDigests{}
	ours := platform.PackageDigests{}
	for _, reachedPackage := range reached {
		read, readError := readPackage(known, reachedPackage, actionsOf[reachedPackage])
		if readError != nil {
			return nil, nil, fmt.Errorf("reading the build: %w", readError)
		}
		if workspace[reachedPackage.PkgPath] {
			ours[reachedPackage.ID] = workspaceOutput(known, read, reachedPackage.Types)
			continue
		}
		maps.Copy(objects, objectDigestsOf(read, reachedPackage))
	}
	return objects, ours, nil
}

// packageRead is what the fact analyzers concluded on one package: each fact it exported by slot, by object and
// for the package, and each analyzer that did not finish on it.
type packageRead struct {
	analyses     map[string]analysisFacts
	objectFacts  map[objectpath.Path][]slottedFact
	packageFacts []slottedFact

	// failures are each unfinished action's slot and error, sorted.
	failures []string
}

// slottedFact is one fact as a digest reads it: the slot of the analyzer that exported it, and the fact by value.
type slottedFact struct {
	slot string
	fact factDigest
}

// readPackage is the facts the fact analyzers' actions exported on read, by value, as encodeEntry reads them: an
// object fact by the object's objectpath, left out when the object has none, as no other package can name it. A fact
// gob or factValueDigest refuses is an error.
func readPackage(known *cacheAnalyzers, read *packages.Package, actions []*checker.Action) (packageRead, error) {
	concluded := packageRead{analyses: map[string]analysisFacts{}, objectFacts: map[objectpath.Path][]slottedFact{}}
	var encoder objectpath.Encoder
	for _, action := range actions {
		slot := known.slots[action.Analyzer]
		if action.Err != nil {
			concluded.failures = append(concluded.failures, slot+": "+action.Err.Error())
			continue
		}
		facts := analysisFacts{}
		objectFacts, packageFacts := action.ExportedFacts()
		for _, objectFact := range objectFacts {
			path, pathError := encoder.For(objectFact.Object)
			if pathError != nil {
				continue
			}
			_, value, factError := encodeFactAndValue(objectFact.Fact)
			if factError != nil {
				return packageRead{}, fmt.Errorf("digesting %s's fact on %s: %w", action.Analyzer.Name,
					objectFact.Object, factError)
			}
			facts.objectFacts = append(facts.objectFacts, objectFactDigest{path: path, fact: value})
			concluded.objectFacts[path] = append(concluded.objectFacts[path], slottedFact{slot: slot, fact: value})
		}
		for _, packageFact := range packageFacts {
			_, value, factError := encodeFactAndValue(packageFact.Fact)
			if factError != nil {
				return packageRead{}, fmt.Errorf("digesting %s's package fact on %s: %w", action.Analyzer.Name,
					read.ID, factError)
			}
			facts.packageFacts = append(facts.packageFacts, value)
			concluded.packageFacts = append(concluded.packageFacts, slottedFact{slot: slot, fact: value})
		}
		concluded.analyses[slot] = facts
	}
	slices.Sort(concluded.failures)
	return concluded, nil
}

// workspaceOutput is a workspace package's output digest, as encodeEntry gives one of a package the run analyzed: every
// rule's slot is held, a rule's that exports no fact with none. A package a fact analyzer did not finish on has no
// entry, so its digest is the output with every failure after it.
func workspaceOutput(known *cacheAnalyzers, concluded packageRead, checked *types.Package) cache.Key {
	analyses := maps.Clone(concluded.analyses)
	for _, ruleAnalyzer := range known.rules {
		if _, held := analyses[known.slots[ruleAnalyzer]]; !held && len(known.factTypes[ruleAnalyzer]) == 0 {
			analyses[known.slots[ruleAnalyzer]] = analysisFacts{}
		}
	}
	output := outputDigest(exportDigest(checked), analyses)
	if len(concluded.failures) == 0 {
		return output
	}
	parts := [][]byte{[]byte("unfinished output"), output[:]}
	for _, failure := range concluded.failures {
		parts = append(parts, []byte(failure))
	}
	return cache.DigestOf(parts...)
}

// objectDigestsOf is the digest of every object of read, concluded being its facts, that objectpath gives a path,
// and of the package itself. A package-level object reads as its lines of exportRendering. A member, a method
// declared on a defined type, a field or an interface method inside a declaration, reads as the one line that writes
// it: a declared method's own line, and for a member inside a declaration's type, the declaration's line that writes
// that type, a defined type's first one. A member two declarations reach, as the fields of a struct one variable
// declares and another's type is inferred from, is read from the first by name. An object only a fact names, which no
// declaration's type reaches, reads as its facts alone.
func objectDigestsOf(concluded packageRead, read *packages.Package) platform.ObjectDigests {
	checked := read.Types
	if checked == nil {
		return platform.ObjectDigests{
			{PackagePath: read.PkgPath}: readDigest(concluded, []string{"no types"}, concluded.packageFacts),
		}
	}
	var encoder objectpath.Encoder
	rendered := map[objectpath.Path][]string{}
	add := func(object types.Object, lines []string) {
		if object.Pkg() != checked {
			return
		}
		path, pathError := encoder.For(object)
		if pathError != nil {
			return
		}
		if _, seen := rendered[path]; !seen {
			rendered[path] = lines
		}
	}
	for _, name := range checked.Scope().Names() {
		object := checked.Scope().Lookup(name)
		lines := renderedObject(object)
		add(object, lines)
		renderedMembers(object, lines[0], func(member types.Object, line string) { add(member, []string{line}) })
	}
	for path := range concluded.objectFacts {
		if _, seen := rendered[path]; !seen {
			rendered[path] = nil
		}
	}
	packageLine := "package " + checked.Path() + " " + checked.Name() + " " + checked.GoVersion()
	objects := platform.ObjectDigests{
		{PackagePath: checked.Path()}: readDigest(concluded, []string{packageLine}, concluded.packageFacts),
	}
	for path, lines := range rendered {
		objects[platform.ObjectKey{PackagePath: checked.Path(), Path: path}] = readDigest(concluded, lines,
			concluded.objectFacts[path])
	}
	return objects
}

// readDigest is the digest of one object of the package concluded read, or of the package itself: its lines, then
// its facts sorted by slot, type and value, then every analyzer that did not finish on the package, since its facts
// are missing from every object of it.
func readDigest(concluded packageRead, lines []string, facts []slottedFact) cache.Key {
	parts := [][]byte{[]byte("object"), []byte(strconv.Itoa(len(lines)))}
	for _, line := range lines {
		parts = append(parts, []byte(line))
	}
	sorted := slices.Clone(facts)
	slices.SortFunc(sorted, func(left, right slottedFact) int {
		return cmp.Or(cmp.Compare(left.slot, right.slot), compareFactDigests(left.fact, right.fact))
	})
	parts = append(parts, []byte(strconv.Itoa(len(sorted))))
	for _, fact := range sorted {
		parts = append(parts, []byte(fact.slot), []byte(fact.fact.typeName), fact.fact.value[:])
	}
	parts = append(parts, []byte(strconv.Itoa(len(concluded.failures))))
	for _, failure := range concluded.failures {
		parts = append(parts, []byte(failure))
	}
	return cache.DigestOf(parts...)
}

// renderedMembers calls visit with every member of a package-level object and the line of exportRendering that
// writes it: a defined type's declared methods each with its own line, and every field and interface method inside
// the types its first line writes, its type parameters' constraints and its underlying type, or inside a declared
// method's signature, with that method's line; an alias's inside the type its declaration names; any other object's
// inside its type, with its one line, firstLine.
func renderedMembers(object types.Object, firstLine string, visit func(member types.Object, line string)) {
	typeName, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		walkMembers(object.Type(), firstLine, visit)
		return
	}
	switch declared := typeName.Type().(type) {
	case *types.Alias:
		for parameter := range declared.TypeParams().TypeParams() {
			walkMembers(parameter.Constraint(), firstLine, visit)
		}
		walkMembers(declared.Rhs(), firstLine, visit)
	case *types.Named:
		for parameter := range declared.TypeParams().TypeParams() {
			walkMembers(parameter.Constraint(), firstLine, visit)
		}
		walkMembers(declared.Underlying(), firstLine, visit)
		for method := range declared.Methods() {
			methodLine := renderedMethod(typeName, method)
			visit(method, methodLine)
			walkMembers(method.Type(), methodLine, visit)
		}
	}
}

// walkMembers calls visit with every field and interface method inside walked, with line, the walk stopping where
// unexportedOrigins' does, at a named type, an alias and a type parameter, each of which is a declaration of its own.
func walkMembers(walked types.Type, line string, visit func(member types.Object, line string)) {
	switch composite := walked.(type) {
	case *types.Pointer:
		walkMembers(composite.Elem(), line, visit)
	case *types.Slice:
		walkMembers(composite.Elem(), line, visit)
	case *types.Array:
		walkMembers(composite.Elem(), line, visit)
	case *types.Chan:
		walkMembers(composite.Elem(), line, visit)
	case *types.Map:
		walkMembers(composite.Key(), line, visit)
		walkMembers(composite.Elem(), line, visit)
	case *types.Signature:
		for parameter := range composite.TypeParams().TypeParams() {
			walkMembers(parameter.Constraint(), line, visit)
		}
		walkMembers(composite.Params(), line, visit)
		walkMembers(composite.Results(), line, visit)
	case *types.Tuple:
		for variable := range composite.Variables() {
			walkMembers(variable.Type(), line, visit)
		}
	case *types.Struct:
		for field := range composite.Fields() {
			visit(field, line)
			walkMembers(field.Type(), line, visit)
		}
	case *types.Interface:
		for method := range composite.ExplicitMethods() {
			visit(method, line)
			walkMembers(method.Type(), line, visit)
		}
		for embedded := range composite.EmbeddedTypes() {
			walkMembers(embedded, line, visit)
		}
	case *types.Union:
		for index := range composite.Len() {
			walkMembers(composite.Term(index).Type(), line, visit)
		}
	}
}
