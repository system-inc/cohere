package check

import (
	"fmt"
	"go/types"
	"strings"

	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// verdict is one package's reads judged in another build: the host's memo and the build's, the packages the judged
// package reaches in the build by path, its closure, those of them the build restored from the host's run, at check
// time the build's facts as its action inherited them, nil at the load's, which asks no fact, and the closures its
// Interface and Method lookups ask of, each gathered once for the verdict.
type verdict struct {
	host     *ReadsBuild
	build    *ReadsBuild
	closure  map[string]*types.Package
	restored map[*types.Package]bool
	fact     FactLookup
	methods  *verdictClosures
}

// verdictClosures are a verdict's import closures as its Interface and Method lookups ask them: the host's, by the
// package whose import closure it is, which is the subject's, and the build's, from the judged package's imports
// there, nil until a lookup first asks.
type verdictClosures struct {
	host  map[*types.Package]*gatheredClosure
	build *gatheredClosure
}

// gatheredClosure is one import closure, the union of its roots' (ReadsBuild.closureOf): its packages, and each by its
// path, one package of a path in any closure.
type gatheredClosure struct {
	reached map[*types.Package]bool
	byPath  map[string]*types.Package
}

// gatherClosure is the import closure of roots in memo, every package of it indexed by its methods' names there.
func gatherClosure(memo *ReadsBuild, roots []*types.Package) *gatheredClosure {
	gathered := &gatheredClosure{reached: map[*types.Package]bool{}, byPath: map[string]*types.Package{}}
	for _, root := range roots {
		for _, reached := range memo.closureOf(root) {
			gathered.reached[reached] = true
			gathered.byPath[reached.Path()] = reached
		}
	}
	return gathered
}

// hostClosure is the host's import closure of subjectPackage, gathered once for the verdict.
func (judged verdict) hostClosure(subjectPackage *types.Package) *gatheredClosure {
	if gathered, known := judged.methods.host[subjectPackage]; known {
		return gathered
	}
	gathered := gatherClosure(judged.host, subjectPackage.Imports())
	judged.methods.host[subjectPackage] = gathered
	return gathered
}

// buildClosure is the build's import closure of roots, the judged package's imports there, gathered once for the
// verdict.
func (judged verdict) buildClosure(roots []*types.Package) *gatheredClosure {
	if judged.methods.build == nil {
		judged.methods.build = gatherClosure(judged.build, roots)
	}
	return judged.methods.build
}

// find is the build's object at path in the package of ownerPath, and why not when there is none.
func (judged verdict) find(ownerPath string, path objectpath.Path) (types.Object, string) {
	owner := judged.closure[ownerPath]
	if owner == nil {
		return nil, "the build has no package " + ownerPath
	}
	found, findError := objectpath.Object(owner, path)
	if findError != nil {
		return nil, "the build's " + ownerPath + " has no " + string(path)
	}
	return found, ""
}

// counterpart is the build's object at the place of hostObject, the host's, by its package's path and objectpath, and
// why not when there is none. unsafe's objects are the type checker's own, the same in every build.
func (judged verdict) counterpart(hostObject types.Object) (types.Object, string) {
	if hostObject.Pkg() == types.Unsafe {
		return hostObject, ""
	}
	path, hasPath := judged.host.pathOf(hostObject)
	if !hasPath {
		return nil, "object " + objectName(hostObject) + " has no objectpath"
	}
	return judged.find(hostObject.Pkg().Path(), path)
}

// objectHolds reports whether hostObject, the host's, is in the build at the same place and renders alike there.
func (judged verdict) objectHolds(hostObject types.Object) (bool, string) {
	found, why := judged.counterpart(hostObject)
	if found == nil {
		return false, "object " + objectName(hostObject) + ": " + why
	}
	if judged.host.closureDigest(hostObject) != judged.build.closureDigest(found) {
		return false, "object " + objectName(hostObject) + " renders differently"
	}
	return true, ""
}

// factHolds reports whether a fact read reads the same in the build: there both times or neither, and when there, its
// value equal by what gob carries of it (factValueDigest).
func (judged verdict) factHolds(fact build_reads.Fact) (bool, string) {
	var path objectpath.Path
	named := fact.Package.Path()
	if fact.Object != nil {
		named = objectName(fact.Object)
		objectPath, hasPath := judged.host.pathOf(fact.Object)
		if !hasPath {
			return false, "fact " + fact.FactType.String() + " of " + named + ": the object has no objectpath"
		}
		path = objectPath
	}
	name := "fact " + fact.FactType.String() + " of " + named
	if judged.fact == nil {
		return false, name + ": the build has no facts to ask"
	}
	value, found := judged.fact(fact.Package.Path(), path, fact.FactType)
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

// lookupHolds reports whether a rule's lookup of the types gets the same answer asked again of the build: the same
// object, rendered alike, or none both times.
//
//   - Scope asks the build's package at the lookup's path for the name.
//   - Closure asks the build's import closure, from the package's imports, as the rule asked (ClosureInterface).
//   - PackageName asks whether a package of that closure has the name, as the rule's walk does.
//   - Interface and Method answer by the subject, the package's own method, which is the host's in the build, and by
//     the closure's type names with a method of the name: so they hold when those type names are the same in both
//     builds, rendered alike. That errs wide, failing when a type name the rule would not have matched changes. A
//     package the build restored from the host's run gives the host's own type names: its types there are read from
//     export data, which holds only what its exported declarations reach, so its unexported types would go missing,
//     while its source and its reads are the host's, so checked from source it declares what the host's does.
//
// A deprecation is a fact, asked at check time (deprecationHolds).
func (judged verdict) lookupHolds(lookup build_reads.Lookup, restoring Restoring) (bool, string) {
	name := "lookup " + lookup.Query + " " + strings.Trim(lookup.Path+"."+lookup.Name, ".")
	roots := make([]*types.Package, 0, len(restoring.Imports))
	for _, imported := range restoring.Imports {
		if judged.closure[imported] == nil {
			return false, name + ": the build has no package " + imported
		}
		roots = append(roots, judged.closure[imported])
	}
	switch lookup.Query {
	case build_reads.QueryScope:
		declaring := judged.closure[lookup.Path]
		if declaring == nil {
			return false, name + ": the build has no package " + lookup.Path
		}
		return judged.answerHolds(name, lookup, declaring.Scope().Lookup(lookup.Name))
	case build_reads.QueryClosure:
		_, declared := build_reads.ClosureInterface(roots, lookup.Path, lookup.Name)
		var answer types.Object
		if declared != nil {
			answer = declared
		}
		return judged.answerHolds(name, lookup, answer)
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
		hostClosure, buildClosure := judged.hostClosure(lookup.Subject.Pkg()), judged.buildClosure(roots)
		// Every type name of either closure with a method of the name, read from each side's index, sorted by
		// package path and name. A package the build restored from the host gives the host's type names of its path.
		var hostCandidates []*types.TypeName
		var buildCandidates []*types.TypeName
		for _, indexed := range judged.host.methodsNamed(lookup.Name) {
			if !hostClosure.reached[indexed.owner] {
				continue
			}
			hostCandidates = append(hostCandidates, indexed.typeNames...)
			if judged.restored[buildClosure.byPath[indexed.owner.Path()]] {
				buildCandidates = append(buildCandidates, indexed.typeNames...)
			}
		}
		for _, indexed := range judged.build.methodsNamed(lookup.Name) {
			if !buildClosure.reached[indexed.owner] ||
				judged.restored[indexed.owner] && hostClosure.byPath[indexed.owner.Path()] != nil {
				continue
			}
			buildCandidates = append(buildCandidates, indexed.typeNames...)
		}
		sortTypeNames(hostCandidates)
		sortTypeNames(buildCandidates)
		if len(hostCandidates) != len(buildCandidates) {
			return false, name + fmt.Sprintf(": %d type names have the method on the host, %d in the build",
				len(hostCandidates), len(buildCandidates))
		}
		for index, hostCandidate := range hostCandidates {
			if !judged.sameObject(hostCandidate, buildCandidates[index]) {
				return false, name + ": " + objectName(hostCandidate) + " differs in the build"
			}
		}
		return true, ""
	default:
		return false, name + ": a lookup of this kind cannot be asked again"
	}
}

// answerHolds reports whether answer, the build's, is the answer lookup recorded on the host.
func (judged verdict) answerHolds(name string, lookup build_reads.Lookup, answer types.Object) (bool, string) {
	switch {
	case lookup.Found != (answer != nil):
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", lookup.Found, answer != nil)
	case answer == nil:
		return true, ""
	case !judged.sameObject(lookup.Answer, answer):
		return false, name + ": the answer differs"
	}
	return true, ""
}

// sameObject reports whether hostObject, the host's, and buildObject, the build's, are one object in each: of one
// package path at one objectpath, rendered alike. The host's own object, as a package restored from the host answers
// a method lookup with, is the same object, which renders alike wherever it is asked.
func (judged verdict) sameObject(hostObject types.Object, buildObject types.Object) bool {
	if hostObject == buildObject {
		return true
	}
	if hostObject.Pkg() == nil || buildObject.Pkg() == nil {
		return false
	}
	if hostObject.Pkg().Path() != buildObject.Pkg().Path() {
		return false
	}
	hostPath, hostHasPath := judged.host.pathOf(hostObject)
	buildPath, buildHasPath := judged.build.pathOf(buildObject)
	if !hostHasPath || !buildHasPath || hostPath != buildPath {
		return false
	}
	return judged.host.closureDigest(hostObject) == judged.build.closureDigest(buildObject)
}

// deprecationHolds reports whether a Deprecated lookup gets the same answer from the build's deprecated facts: the
// subject, or its package, deprecated both times with the same message, or neither time.
func (judged verdict) deprecationHolds(lookup build_reads.Lookup) (bool, string) {
	name := "lookup " + lookup.Query + " " + lookup.Path
	var path objectpath.Path
	if lookup.Subject != nil {
		subjectPath, hasPath := judged.host.pathOf(lookup.Subject)
		// A field of an instance, as a key of `Box[int]{Value: 1}` names, has no objectpath, and no fact ever attaches
		// to it in any build: the deprecated facts' result holds the objects their packages declare, the generic's
		// field and not its instances'. So the rule's answer for it is no deprecation everywhere, and holds when the
		// host's was too.
		if variable, isVariable := lookup.Subject.(*types.Var); !hasPath && isVariable &&
			variable.Origin() != variable && !lookup.Found {
			return true, ""
		}
		if !hasPath {
			return false, name + ": the subject has no objectpath"
		}
		path = subjectPath
		name += " of " + objectName(lookup.Subject)
	}
	if judged.fact == nil {
		return false, name + ": the build has no facts to ask"
	}
	value, found := judged.fact(lookup.Path, path, deprecatedFactType)
	if found != lookup.Found {
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", lookup.Found, found)
	}
	if !found {
		return true, ""
	}
	deprecation, isDeprecation := value.(*deprecated.IsDeprecated)
	if !isDeprecation || deprecation.Msg != lookup.Text {
		return false, name + ": its message differs"
	}
	return true, ""
}
