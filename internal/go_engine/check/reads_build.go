package check

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// FactLookup is one build's facts as a verdict asks them: the fact of type factType on the object at path in the
// package at owner, or on that package itself when path is empty, and whether there is one.
type FactLookup func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool)

// ReadsBuild is one build's types as a verdict on a package's reads reads them, the host's or an extra build's (linux,
// windows, freebsd), with what the verdicts compute of them memoized for the build, so every package's verdict
// renders each object once (#b2t9xmf). Its methods take its lock, so verdicts may run on several goroutines.
type ReadsBuild struct {
	// packages are the build's packages by path, as the package a verdict judges sees them, its imports and what they
	// reach among them. The host's needs none: its reads hold its objects.
	packages map[string]*types.Package

	// fact is the build's fact lookup, nil for the host's, whose reads hold their facts.
	fact FactLookup

	lock sync.Mutex

	// renderings are each object's closureRendering, as a digest.
	renderings map[types.Object]cache.Key

	// paths are each object's objectpath, and encoder the encoder that found them, which keeps what it learns of each
	// package for the next object of it.
	paths   map[types.Object]objectPathResult
	encoder objectpath.Encoder

	// methodNames are each package's type names by the name of each method in their method sets (methodNamesOf).
	methodNames map[*types.Package]map[string][]*types.TypeName
}

// objectPathResult is an object's objectpath, and whether it has one.
type objectPathResult struct {
	path  objectpath.Path
	found bool
}

// NewReadsBuild is a build of packages, by path as the package a verdict judges sees them, whose facts fact looks up.
// The host's is NewReadsBuild(nil, nil).
func NewReadsBuild(packages map[string]*types.Package, fact FactLookup) *ReadsBuild {
	return &ReadsBuild{
		packages:    packages,
		fact:        fact,
		renderings:  map[types.Object]cache.Key{},
		paths:       map[types.Object]objectPathResult{},
		methodNames: map[*types.Package]map[string][]*types.TypeName{},
	}
}

// rendering is object's closureRendering as a digest, rendered the first time it is asked. Two goroutines asking at
// once may both render it, which costs time and never a wrong answer.
func (build *ReadsBuild) rendering(object types.Object) cache.Key {
	build.lock.Lock()
	rendered, known := build.renderings[object]
	build.lock.Unlock()
	if known {
		return rendered
	}
	lines := closureRendering(object)
	parts := make([][]byte, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, []byte(line))
	}
	rendered = cache.DigestOf(parts...)
	build.lock.Lock()
	build.renderings[object] = rendered
	build.lock.Unlock()
	return rendered
}

// pathOf is object's objectpath in its package, and whether it has one.
func (build *ReadsBuild) pathOf(object types.Object) (objectpath.Path, bool) {
	build.lock.Lock()
	defer build.lock.Unlock()
	if known, isKnown := build.paths[object]; isKnown {
		return known.path, known.found
	}
	path, pathError := build.encoder.For(object)
	build.paths[object] = objectPathResult{path: path, found: pathError == nil}
	return path, pathError == nil
}

// find is the object of the build at path in the package of ownerPath, and why not when there is none.
func (build *ReadsBuild) find(ownerPath string, path objectpath.Path) (types.Object, string) {
	owner := build.packages[ownerPath]
	if owner == nil {
		return nil, "the build has no package " + ownerPath
	}
	found, findError := objectpath.Object(owner, path)
	if findError != nil {
		return nil, "the build's " + ownerPath + " has no " + string(path)
	}
	return found, ""
}

// methodNamesOf are the type names declared package declares, by the name of each method in their method sets,
// promoted ones included, and a pointer's for a type that is not an interface, memoized for the build.
func (build *ReadsBuild) methodNamesOf(declaring *types.Package) map[string][]*types.TypeName {
	build.lock.Lock()
	known, isKnown := build.methodNames[declaring]
	build.lock.Unlock()
	if isKnown {
		return known
	}
	byName := map[string][]*types.TypeName{}
	for _, name := range declaring.Scope().Names() {
		typeName, isTypeName := declaring.Scope().Lookup(name).(*types.TypeName)
		if !isTypeName {
			continue
		}
		setOf := typeName.Type()
		if !types.IsInterface(setOf) {
			setOf = types.NewPointer(setOf)
		}
		methods := types.NewMethodSet(setOf)
		for index := range methods.Len() {
			methodName := methods.At(index).Obj().Name()
			byName[methodName] = append(byName[methodName], typeName)
		}
	}
	build.lock.Lock()
	build.methodNames[declaring] = byName
	build.lock.Unlock()
	return byName
}

// Restoring is one package of the host's run whose reads a verdict judges in another build.
type Restoring struct {
	// Path is the package's path, whose own objects a read never asks of the other build: its own inputs are the
	// host's there, which the portable digest compares (cache.Keyed.Portable).
	Path string

	// Imports are the paths of the packages it imports, the roots of its import closure in the other build, the same
	// as the host's while its own inputs are.
	Imports []string

	// Reads are its reads on the host's run.
	Reads *build_reads.Reads
}

// Holds reports whether everything restoring read on the host's run, in host, reads the same in build, and when it
// does not, the first read that differs, named by its row: T for an object or instance its type check reached, F for
// a fact, L for a rule's lookup, D for a deprecation, and never or unknown for reads that cannot hold at all. A
// package whose own inputs are the host's and whose reads hold may take the host's results in build (#b2t9xmf).
//
// Every read fails closed: an object with no objectpath, a package the build lacks, a fact that does not encode, and a
// lookup of a kind it does not know each fail it.
func Holds(restoring Restoring, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	if restorable, why := restoring.Reads.Restorable(); !restorable {
		if restoring.Reads.Never() != "" {
			return false, "never: " + why
		}
		return false, "unknown: " + why
	}
	for _, object := range restoring.Reads.Objects() {
		if holds, why := objectHolds(object, host, build); !holds {
			return false, "T " + why
		}
	}
	for _, instance := range restoring.Reads.Instances() {
		if holds, why := instanceHolds(instance, restoring.Path, host, build); !holds {
			return false, "T " + why
		}
	}
	for _, fact := range restoring.Reads.Facts() {
		if holds, why := factHolds(fact, host, build); !holds {
			return false, "F " + why
		}
	}
	for _, lookup := range restoring.Reads.Lookups() {
		row := "L "
		if lookup.Query == build_reads.QueryDeprecated {
			row = "D "
		}
		if holds, why := lookupHolds(lookup, restoring, host, build); !holds {
			return false, row + why
		}
	}
	return true, ""
}

// objectName is object as a reason names it, its package's path and its name.
func objectName(object types.Object) string {
	if object.Pkg() == nil {
		return object.Name()
	}
	return object.Pkg().Path() + "." + object.Name()
}

// counterpart is the build's object at the place of hostObject, the host's, by its package's path and objectpath, and
// why not when there is none. unsafe's objects are the type checker's own, the same in every build.
func counterpart(hostObject types.Object, host *ReadsBuild, build *ReadsBuild) (types.Object, string) {
	if hostObject.Pkg() == types.Unsafe {
		return hostObject, ""
	}
	path, hasPath := host.pathOf(hostObject)
	if !hasPath {
		return nil, "object " + objectName(hostObject) + " has no objectpath"
	}
	return build.find(hostObject.Pkg().Path(), path)
}

// objectHolds reports whether hostObject, the host's, is in build at the same place and renders alike there.
func objectHolds(hostObject types.Object, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	found, why := counterpart(hostObject, host, build)
	if found == nil {
		return false, "object " + objectName(hostObject) + ": " + why
	}
	if host.rendering(hostObject) != build.rendering(found) {
		return false, "object " + objectName(hostObject) + " renders differently"
	}
	return true, ""
}

// instanceHolds reports whether an instance the package made of another package's generic reads the same in build:
// the generic, and every named type of another package its type arguments reach, each in build and rendered alike.
// The arguments' shape is the package's own source and what it inferred from the objects it read, each a read of its
// own; a named type of the package itself is an own input.
func instanceHolds(instance build_reads.Instance, ownPath string, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	if holds, why := objectHolds(instance.Generic, host, build); !holds {
		return false, "instance of " + objectName(instance.Generic) + ": " + why
	}
	for _, typeName := range closureTypeNames(slices.Collect(instance.TypeArguments.Types())) {
		if typeName.Pkg().Path() == ownPath {
			continue
		}
		if holds, why := objectHolds(typeName, host, build); !holds {
			return false, "instance of " + objectName(instance.Generic) + ": " + why
		}
	}
	return true, ""
}

// factHolds reports whether a fact read reads the same in build: there both times or neither, and when there, its
// value equal by what gob carries of it (factValueDigest).
func factHolds(fact build_reads.Fact, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	var path objectpath.Path
	named := fact.Package.Path()
	if fact.Object != nil {
		named = objectName(fact.Object)
		objectPath, hasPath := host.pathOf(fact.Object)
		if !hasPath {
			return false, "fact " + fact.FactType.String() + " of " + named + ": the object has no objectpath"
		}
		path = objectPath
	}
	name := "fact " + fact.FactType.String() + " of " + named
	if build.fact == nil {
		return false, name + ": the build has no facts to ask"
	}
	value, found := build.fact(fact.Package.Path(), path, fact.FactType)
	switch {
	case found != fact.Found:
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", fact.Found, found)
	case !found:
		return true, ""
	}
	hostDigest, hostError := factValueDigest(fact.Value)
	buildDigest, buildError := factValueDigest(value)
	if hostError != nil || buildError != nil {
		return false, name + ": its value does not encode"
	}
	if hostDigest != buildDigest {
		return false, name + ": its value differs"
	}
	return true, ""
}

// lookupHolds reports whether a rule's lookup gets the same answer asked again of build: the same object, rendered
// alike, or none both times, or for a deprecation the same message.
//
//   - Scope asks the build's package at the lookup's path for the name.
//   - Closure asks the build's import closure, from the package's imports, as the rule asked (ClosureInterface).
//   - PackageName asks whether a package of that closure has the name, as the rule's walk does.
//   - Interface and Method answer by the subject, the package's own method, which is the host's in the build, and by
//     the closure's type names with a method of the name: so they hold when those type names are the same in both
//     builds, rendered alike. That errs wide, failing when a type name the rule would not have matched changes.
//   - Deprecated asks the build's deprecated fact of the subject, or of its package.
func lookupHolds(lookup build_reads.Lookup, restoring Restoring, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	name := "lookup " + lookup.Query + " " + strings.Trim(lookup.Path+"."+lookup.Name, ".")
	roots := make([]*types.Package, 0, len(restoring.Imports))
	for _, imported := range restoring.Imports {
		if build.packages[imported] == nil {
			return false, name + ": the build has no package " + imported
		}
		roots = append(roots, build.packages[imported])
	}
	switch lookup.Query {
	case build_reads.QueryScope:
		declaring := build.packages[lookup.Path]
		if declaring == nil {
			return false, name + ": the build has no package " + lookup.Path
		}
		return answerHolds(name, lookup, declaring.Scope().Lookup(lookup.Name), host, build)
	case build_reads.QueryClosure:
		_, declared := build_reads.ClosureInterface(roots, lookup.Path, lookup.Name)
		var answer types.Object
		if declared != nil {
			answer = declared
		}
		return answerHolds(name, lookup, answer, host, build)
	case build_reads.QueryPackageName:
		taken := false
		for reached := range build_reads.ClosurePackages(roots) {
			if reached.Name() == lookup.Name {
				taken = true
				break
			}
		}
		if taken != lookup.Found {
			return false, name + fmt.Sprintf(": found %t on the host, %t in the build", lookup.Found, taken)
		}
		return true, ""
	case build_reads.QueryInterface, build_reads.QueryMethod:
		if lookup.Subject == nil || lookup.Subject.Pkg() == nil {
			return false, name + ": it has no subject to find the host's closure by"
		}
		hostCandidates := methodCandidates(host, lookup.Subject.Pkg().Imports(), lookup.Name)
		buildCandidates := methodCandidates(build, roots, lookup.Name)
		if len(hostCandidates) != len(buildCandidates) {
			return false, name + fmt.Sprintf(": %d type names have the method on the host, %d in the build",
				len(hostCandidates), len(buildCandidates))
		}
		for index, hostCandidate := range hostCandidates {
			if !sameObject(hostCandidate, buildCandidates[index], host, build) {
				return false, name + ": " + objectName(hostCandidate) + " differs in the build"
			}
		}
		return true, ""
	case build_reads.QueryDeprecated:
		return deprecationHolds(name, lookup, host, build)
	default:
		return false, name + ": a lookup of this kind cannot be asked again"
	}
}

// answerHolds reports whether answer, the build's, is the answer lookup recorded on the host.
func answerHolds(
	name string,
	lookup build_reads.Lookup,
	answer types.Object,
	host *ReadsBuild,
	build *ReadsBuild,
) (bool, string) {
	switch {
	case lookup.Found != (answer != nil):
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", lookup.Found, answer != nil)
	case answer == nil:
		return true, ""
	case !sameObject(lookup.Answer, answer, host, build):
		return false, name + ": the answer differs"
	}
	return true, ""
}

// sameObject reports whether hostObject, the host's, and buildObject, the build's, are one object in each: of one
// package path at one objectpath, rendered alike.
func sameObject(hostObject types.Object, buildObject types.Object, host *ReadsBuild, build *ReadsBuild) bool {
	if hostObject.Pkg() == nil || buildObject.Pkg() == nil {
		return hostObject == buildObject
	}
	if hostObject.Pkg().Path() != buildObject.Pkg().Path() {
		return false
	}
	hostPath, hostHasPath := host.pathOf(hostObject)
	buildPath, buildHasPath := build.pathOf(buildObject)
	if !hostHasPath || !buildHasPath || hostPath != buildPath {
		return false
	}
	return host.rendering(hostObject) == build.rendering(buildObject)
}

// methodCandidates are the type names of the import closure of roots with a method named name in their method sets,
// sorted by package path and name: every type whose method an Interface or Method lookup of the name may match, by
// whichever measure the asking rule takes.
func methodCandidates(build *ReadsBuild, roots []*types.Package, name string) []*types.TypeName {
	var candidates []*types.TypeName
	for reached := range build_reads.ClosurePackages(roots) {
		candidates = append(candidates, build.methodNamesOf(reached)[name]...)
	}
	slices.SortFunc(candidates, func(left, right *types.TypeName) int {
		if order := strings.Compare(left.Pkg().Path(), right.Pkg().Path()); order != 0 {
			return order
		}
		return strings.Compare(left.Name(), right.Name())
	})
	return candidates
}

// deprecatedFactType is the deprecated analyzer's fact, which its result gathers for SA1019 from every fact of the
// import closure, so a Deprecated lookup is that fact asked of the build.
var deprecatedFactType = reflect.TypeFor[*deprecated.IsDeprecated]()

// deprecationHolds reports whether a Deprecated lookup gets the same answer from the build's deprecated facts: the
// subject, or its package, deprecated both times with the same message, or neither time.
func deprecationHolds(name string, lookup build_reads.Lookup, host *ReadsBuild, build *ReadsBuild) (bool, string) {
	var path objectpath.Path
	asked := name
	if lookup.Subject != nil {
		subjectPath, hasPath := host.pathOf(lookup.Subject)
		if !hasPath {
			return false, name + ": the subject has no objectpath"
		}
		path = subjectPath
		asked = name + " of " + objectName(lookup.Subject)
	}
	if build.fact == nil {
		return false, asked + ": the build has no facts to ask"
	}
	value, found := build.fact(lookup.Path, path, deprecatedFactType)
	if found != lookup.Found {
		return false, asked + fmt.Sprintf(": found %t on the host, %t in the build", lookup.Found, found)
	}
	if !found {
		return true, ""
	}
	deprecation, isDeprecation := value.(*deprecated.IsDeprecated)
	if !isDeprecation || deprecation.Msg != lookup.Text {
		return false, asked + ": its message differs"
	}
	return true, ""
}
