package check

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"

	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// useDiffers is what a reason says of a use of each kind but the whole that does not hold.
var useDiffers = map[build_reads.UseKind]string{
	build_reads.UseName:       "its name or kind differs",
	build_reads.UseMember:     "its own line differs",
	build_reads.UseUnderlying: "its underlying type differs",
	build_reads.UseMethodSet:  "its method set differs",
}

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
// there, nil until a lookup first asks, with buildUnindexed, the packages of the build's not yet indexed for this
// verdict (indexBuildCandidates).
type verdictClosures struct {
	host           map[*types.Package]*gatheredClosure
	build          *gatheredClosure
	buildUnindexed []*types.Package
}

// gatheredClosure is one import closure, the union of its roots' (ReadsBuild.closureOf): its packages, in the order
// the walks reached them and as a set, and each by its path, one package of a path in any closure.
type gatheredClosure struct {
	ordered []*types.Package
	reached map[*types.Package]bool
	byPath  map[string]*types.Package
}

// gatherClosure is the import closure of roots, each root's as closureOf gives it: the host's indexed whole
// (ReadsBuild.indexedClosureOf), the build's walked alone (ReadsBuild.closureOf).
func gatherClosure(closureOf func(*types.Package) []*types.Package, roots []*types.Package) *gatheredClosure {
	gathered := &gatheredClosure{reached: map[*types.Package]bool{}, byPath: map[string]*types.Package{}}
	for _, root := range roots {
		for _, reached := range closureOf(root) {
			if gathered.reached[reached] {
				continue
			}
			gathered.ordered = append(gathered.ordered, reached)
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
	gathered := gatherClosure(judged.host.indexedClosureOf, subjectPackage.Imports())
	judged.methods.host[subjectPackage] = gathered
	return gathered
}

// buildClosure is the build's import closure of roots, the judged package's imports there, gathered once for the
// verdict, none of it indexed until a lookup reads it (indexBuildCandidates).
func (judged verdict) buildClosure(roots []*types.Package) *gatheredClosure {
	if judged.methods.build == nil {
		judged.methods.build = gatherClosure(judged.build.closureOf, roots)
		judged.methods.buildUnindexed = slices.Clone(judged.methods.build.ordered)
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
// why not when there is none. unsafe's objects are the type checker's own, and the universe's, of no package, the same
// in every build: a selection of Error through another package's interface that embeds error selects the universe's,
// which a package judged by closure alone reads whole.
func (judged verdict) counterpart(hostObject types.Object) (types.Object, string) {
	if hostObject.Pkg() == nil || hostObject.Pkg() == types.Unsafe {
		return hostObject, ""
	}
	path, hasPath := judged.host.pathOf(hostObject)
	if !hasPath {
		return nil, "object " + objectName(hostObject) + " has no objectpath"
	}
	return judged.find(hostObject.Pkg().Path(), path)
}

// useHolds reports whether use, a read of the host's object, reads the same in the build, judged by its kind of use
// (#mjdf7ah): a whole use by the object's closure, as every read was before kinds (objectHolds), and every other kind
// by what of the object that kind read (ReadsBuild.useDigest). closureOnly judges every use as a whole one.
func (judged verdict) useHolds(use build_reads.ObjectUse, closureOnly bool) (bool, string) {
	if closureOnly || use.Kind == build_reads.UseWhole {
		return judged.objectHolds(use.Object)
	}
	found, why := judged.counterpart(use.Object)
	if found == nil {
		return false, "object " + objectName(use.Object) + ": " + why
	}
	if judged.host.useDigest(use.Object, use.Kind) != judged.build.useDigest(found, use.Kind) {
		return false, "object " + objectName(use.Object) + ": " + useDiffers[use.Kind]
	}
	return true, ""
}

// selectionHolds reports whether selection, one the host's check made through another package's named type, selects
// the same in the build: the build's lookup of its name from the receiver finds a member at the same index path, so
// nothing shallower shadows it and no twin at its depth makes it ambiguous, whose own line is the host's member's.
// closureOnly judges it by the receiver's and the member's closures instead.
func (judged verdict) selectionHolds(selection build_reads.Selection, closureOnly bool) (bool, string) {
	if closureOnly {
		if holds, why := judged.objectHolds(selection.Receiver); !holds {
			return false, why
		}
		return judged.objectHolds(selection.Selected)
	}
	name := "selection " + objectName(selection.Receiver) + "." + selection.Name
	found, why := judged.counterpart(selection.Receiver)
	if found == nil {
		return false, name + ": " + why
	}
	// A lookup of an unexported name finds it only in its own package, the build's of the member's path.
	var namedIn *types.Package
	if selection.Selected.Pkg() != nil {
		namedIn = judged.closure[selection.Selected.Pkg().Path()]
	}
	selected, index, _ := types.LookupFieldOrMethod(found.Type(), true, namedIn, selection.Name)
	switch {
	case selected == nil:
		return false, name + ": the build finds no one member of the name"
	case !slices.Equal(index, selection.Index):
		return false, name + fmt.Sprintf(": the build finds it at %v, the host at %v", index, selection.Index)
	case judged.host.useDigest(selection.Selected, build_reads.UseMember) !=
		judged.build.useDigest(declaredOrigin(selected), build_reads.UseMember):
		return false, name + ": the member it selects differs"
	}
	return true, ""
}

// declaredOrigin is object as its package declares it, the generic one for a field or method of an instance.
func declaredOrigin(object types.Object) types.Object {
	switch instantiated := object.(type) {
	case *types.Func:
		return instantiated.Origin()
	case *types.Var:
		return instantiated.Origin()
	}
	return object
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
	hostValue := func() (cache.Key, error) { return factValueDigest(fact.Value) }
	return judged.buildFactHolds("fact "+fact.FactType.String()+" of "+named, fact.Package.Path(), path,
		fact.FactType, fact.Found, hostValue, func(digest cache.Key) cache.Key { return digest })
}

// buildFactHolds is a fact read's build half, which a read of this run and a persisted read share: the build's fact of
// factType on the object at path in the package at owner, or on that package itself when path is empty, found when
// the host's was, hostFound, and when found, of a value whose digest, mixed as the host's was (mixed), is hostValue's.
// name is how a reason names the read.
func (judged verdict) buildFactHolds(
	name string,
	owner string,
	path objectpath.Path,
	factType reflect.Type,
	hostFound bool,
	hostValue func() (cache.Key, error),
	mixed func(cache.Key) cache.Key,
) (bool, string) {
	if judged.fact == nil {
		return false, name + ": the build has no facts to ask"
	}
	value, found := judged.fact(owner, path, factType)
	switch {
	case found != hostFound:
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", hostFound, found)
	case !found:
		return true, ""
	}
	hostDigest, hostError := hostValue()
	buildDigest, buildError := factValueDigest(value)
	if hostError != nil || buildError != nil {
		return false, name + ": its value does not encode"
	}
	if hostDigest != mixed(buildDigest) {
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
//     the closure's type names with a method of the name that a rule could let fix the subject's, as the rules index
//     them (countedCandidates), judged by the one bit their rules read of them (methodLookupHolds), or, where that bit
//     can flip with nothing it reads changing, by every type name whose whole method set has a method of the name,
//     promoted ones included, being the same in both builds, rendered alike (#swv3jbw). A package the build restored
//     from the host's run gives the host's own type names: its types there are read from export data, which holds
//     only what its exported declarations reach, so its unexported types would go missing, while its source and its
//     reads are the host's, so checked from source it declares what the host's does.
//
// A deprecation is a fact, asked at check time (deprecationHolds).
func (judged verdict) lookupHolds(lookup build_reads.Lookup, restoring Restoring) (bool, string) {
	name := "lookup " + lookup.Query + " " + strings.Trim(lookup.Path+"."+lookup.Name, ".")
	roots, why := judged.importRoots(restoring.Imports)
	if why != "" {
		return false, name + ": " + why
	}
	switch lookup.Query {
	case build_reads.QueryScope, build_reads.QueryClosure:
		answer, why := judged.buildAnswer(lookup.Query, lookup.Path, lookup.Name, roots)
		if why != "" {
			return false, name + ": " + why
		}
		return answerHolds(name, lookup.Found, answer, func(answer types.Object) bool {
			return judged.sameObject(lookup.Answer, answer)
		})
	case build_reads.QueryPackageName:
		return packageNameHolds(name, lookup.Found, roots, lookup.Name)
	case build_reads.QueryInterface, build_reads.QueryMethod:
		if lookup.Subject == nil || lookup.Subject.Pkg() == nil {
			return false, name + ": it has no subject to find the host's closure by"
		}
		subjectConcrete := concreteSubject(lookup.Subject)
		// The type names of either closure with a method of the name that a rule could let fix the subject's, as the
		// rules index them (countedCandidates), sorted by package path and name, which the one-bit judgment reads.
		hostCandidates, buildCandidates := judged.closureCandidates(lookup, roots, ruleTypeNames,
			func(typeNames []*types.TypeName) []*types.TypeName {
				return countedCandidates(typeNames, subjectConcrete)
			})
		judgment := judged.methodJudgmentOf(lookup, hostCandidates, buildCandidates)
		if holds, why, decided := methodLookupHolds(name, judgment); decided {
			return holds, why
		}
		// Where that bit can flip with nothing it reads changing, every type name whose whole method set has a method
		// of the name, promoted ones included: satisfying a constraint reads a type argument's whole method set.
		hostCandidates, buildCandidates = judged.closureCandidates(lookup, roots, methodSetTypeNames,
			func(typeNames []*types.TypeName) []*types.TypeName { return typeNames })
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

// closureCandidates are the type names of the host's import closure of lookup's subject's package and of the build's
// from roots with a method of lookup's name, each side's from its index's list that list picks (ruleTypeNames or
// methodSetTypeNames), each package's passed through kept, sorted by package path and name. A package the build
// restored from the host gives the host's type names of its path.
func (judged verdict) closureCandidates(
	lookup build_reads.Lookup,
	roots []*types.Package,
	list func(packageMethods) []*types.TypeName,
	kept func([]*types.TypeName) []*types.TypeName,
) (hostCandidates []*types.TypeName, buildCandidates []*types.TypeName) {
	hostClosure, buildClosure := judged.hostClosure(lookup.Subject.Pkg()), judged.buildClosure(roots)
	for _, indexed := range judged.host.methodsNamed(lookup.Name) {
		if !hostClosure.reached[indexed.owner] {
			continue
		}
		counted := kept(list(indexed))
		hostCandidates = append(hostCandidates, counted...)
		if judged.restored[buildClosure.byPath[indexed.owner.Path()]] {
			buildCandidates = append(buildCandidates, counted...)
		}
	}
	buildCandidates = append(buildCandidates, kept(judged.buildCandidates(lookup.Name, roots,
		func(owner *types.Package) bool { return hostClosure.byPath[owner.Path()] != nil }, list))...)
	sortTypeNames(hostCandidates)
	sortTypeNames(buildCandidates)
	return hostCandidates, buildCandidates
}

// importRoots are the build's packages at imports, the judged package's imports, the roots of the import closure its
// lookups ask of, and why not when the build has no package at one of them.
func (judged verdict) importRoots(imports []string) ([]*types.Package, string) {
	roots := make([]*types.Package, 0, len(imports))
	for _, imported := range imports {
		if judged.closure[imported] == nil {
			return nil, "the build has no package " + imported
		}
		roots = append(roots, judged.closure[imported])
	}
	return roots, ""
}

// buildAnswer is the build's answer to a Scope or Closure lookup of name in the package at path, nil for none, and why
// the build cannot be asked: Scope asks the build's package at path, and Closure the build's import closure from
// roots, as the rule asked (ClosureInterface).
func (judged verdict) buildAnswer(
	query string,
	path string,
	name string,
	roots []*types.Package,
) (types.Object, string) {
	if query == build_reads.QueryScope {
		declaring := judged.closure[path]
		if declaring == nil {
			return nil, "the build has no package " + path
		}
		return declaring.Scope().Lookup(name), ""
	}
	_, declared := build_reads.ClosureInterface(roots, path, name)
	if declared == nil {
		// A nil *types.TypeName in the interface would read as an answer.
		return nil, ""
	}
	return declared, ""
}

// packageNameHolds reports whether a PackageName lookup of name, which found a package so named on the host when
// hostFound, finds one in the build's import closure from roots as the rule's walk does, named in a reason as name.
func packageNameHolds(name string, hostFound bool, roots []*types.Package, packageName string) (bool, string) {
	taken := false
	for reached := range build_reads.ClosurePackages(roots) {
		if reached.Name() == packageName {
			taken = true
			break
		}
	}
	if taken != hostFound {
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", hostFound, taken)
	}
	return true, ""
}

// buildCandidates are the build's type names with a method named name in its import closure from roots, read from
// the list of its index that list picks (ruleTypeNames or methodSetTypeNames), unsorted, but for those of a package
// the build restored from the host's run that the host's side stands for (hostHas), which the caller gives as the
// host's own type names.
func (judged verdict) buildCandidates(
	name string,
	roots []*types.Package,
	hostHas func(owner *types.Package) bool,
	list func(packageMethods) []*types.TypeName,
) []*types.TypeName {
	buildClosure := judged.buildClosure(roots)
	judged.indexBuildCandidates(hostHas)
	var candidates []*types.TypeName
	for _, indexed := range judged.build.methodsNamed(name) {
		if !buildClosure.reached[indexed.owner] || judged.restored[indexed.owner] && hostHas(indexed.owner) {
			continue
		}
		candidates = append(candidates, list(indexed)...)
	}
	return candidates
}

// indexBuildCandidates indexes, in the build's memo, every package of the verdict's build closure that buildCandidates
// takes candidates from under hostHas: all but a package the build restored from the host's run that the host's side
// stands for, which the walk left unindexed (ReadsBuild.closureOf). It is exact: every package buildCandidates returns
// candidates of passes that filter, and every such package is indexed before it reads the index, so it returns what
// an index of the whole closure gives. The filter after methodsNamed stays, since another verdict may have indexed a
// package this one skips. hostHas differs by caller, the host's closure for a lookup of this run and the host's
// candidates' owners for a persisted one, which is why the indexing is decided here by the same hostHas, never ahead
// of it.
//
// A package once indexed for the verdict leaves buildUnindexed, so after the first lookup each later one passes over
// only the restored packages the host stood for so far, whichever lookup asks.
func (judged verdict) indexBuildCandidates(hostHas func(owner *types.Package) bool) {
	skipped := judged.methods.buildUnindexed[:0]
	for _, owner := range judged.methods.buildUnindexed {
		if judged.restored[owner] && hostHas(owner) {
			skipped = append(skipped, owner)
			continue
		}
		judged.build.indexPackage(owner)
	}
	judged.methods.buildUnindexed = skipped
}

// answerHolds reports whether answer, the build's, is the answer a lookup named name recorded on the host: found when
// the host's was, hostFound, and then the same object, as same judges it.
func answerHolds(name string, hostFound bool, answer types.Object, same func(types.Object) bool) (bool, string) {
	switch {
	case hostFound != (answer != nil):
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", hostFound, answer != nil)
	case answer == nil:
		return true, ""
	case !same(answer):
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
	return judged.buildDeprecationHolds(name, lookup.Path, path, lookup.Found, lookup.Text)
}

// buildDeprecationHolds is a Deprecated lookup's build half, which a read of this run and a persisted read share: the
// build's deprecated fact of the object at path in the package at owner, or of that package itself when path is empty,
// found when the host's was, hostFound, and when found, with the host's message, hostText. name is how a reason names
// the lookup.
func (judged verdict) buildDeprecationHolds(
	name string,
	owner string,
	path objectpath.Path,
	hostFound bool,
	hostText string,
) (bool, string) {
	if judged.fact == nil {
		return false, name + ": the build has no facts to ask"
	}
	value, found := judged.fact(owner, path, deprecatedFactType)
	if found != hostFound {
		return false, name + fmt.Sprintf(": found %t on the host, %t in the build", hostFound, found)
	}
	if !found {
		return true, ""
	}
	deprecation, isDeprecation := value.(*deprecated.IsDeprecated)
	if !isDeprecation || deprecation.Msg != hostText {
		return false, name + ": its message differs"
	}
	return true, ""
}
