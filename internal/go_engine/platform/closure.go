package platform

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// BuildLoad is one build's load as the closure reads it: the build, its workspace packages as load gives them
// (Loaded.Packages, test variants included), and the digests of how the build reads each object outside the
// workspace and each workspace package's output.
type BuildLoad struct {
	Build    project.Build
	Packages []*packages.Package

	// ObjectDigests are, for every object of every package outside the workspace the load reaches, a digest of how
	// the build reads it: its export rendering and every fact attached to it.
	ObjectDigests map[ObjectKey]cache.Key

	// PackageDigests are, for every workspace package by load ID, its export and its exported facts, as check's
	// output digest combines them.
	PackageDigests map[string]cache.Key
}

// ReasonKind is why a package is in the closure.
type ReasonKind string

const (
	// ReasonKindSeed is a package whose compiled files differ between the builds, or that only the extra build has.
	ReasonKindSeed ReasonKind = "Seed"

	// ReasonKindReference is a package that reaches an object outside the workspace the builds read differently.
	ReasonKindReference ReasonKind = "Reference"

	// ReasonKindImporter is a package that directly imports a closure package whose output differs.
	ReasonKindImporter ReasonKind = "Importer"
)

// Reason is one reason a package is in the closure.
type Reason struct {
	Kind ReasonKind

	// Object is the differing object a Reference reaches.
	Object ObjectKey

	// Imported is the load ID of the closure package an Importer imports.
	Imported string
}

// Member is one package the extra build must analyze, by load ID, with every reason it must, sorted.
type Member struct {
	Id      string
	Reasons []Reason
}

// Closure is the packages of the extra build that can find something the host's run cannot, sorted by load ID,
// so it analyzes those and takes every other package's findings from the host's run:
//
//   - Seeds: each workspace package, test variants included, whose compiled file set differs between the builds,
//     or that only the extra build has.
//   - References: each workspace package whose TypesInfo.Uses, Implicits or Selections, in either build's load of
//     it, reach an object outside the workspace whose ObjectDigests entry differs. Either build's, so a reference
//     only one build makes counts. A selection reaches every embedded field its path passes through, which no
//     identifier names, and an implicit, a type switch's binding, reaches the named types of its type, which no
//     clause need name. So does every expression of TypesInfo.Types and every object of Defs: a value of a
//     dependency's type read through a function whose own rendering never moves, as `h := dependency.Value()`
//     then `h + 1` reads Handle, reaches the type by no name at all.
//   - Importers: each workspace package that directly imports a closure package whose PackageDigests entry
//     differs, and in turn each importer of those whose own entry differs. A closure package whose output is the
//     same gives its importers the same types and facts, so they read nothing new through it; one whose output
//     differs is, to its importers, a seed, whatever put it in the closure. So the importers of a package that is in
//     only by a reference are followed exactly when its own output differs, and the seeds measurement found that
//     following them otherwise adds nothing.
//
// A digest one build lacks differs, and one neither build has counts as differing too: a key neither holds is
// unknown, not alike, as a member reached only through a generic instance's type arguments is, which check.ReadBuild
// gives no digest. Unknown must widen the closure, never narrow it, so a gap in the digests costs packages analyzed
// again, never a finding.
//
// Workspace packages the load does not analyze, as the plain p beside "p [p.test]", are read as the rest are, since
// what reaches the analyzed ones through them counts, but are never members: the extra build's roots are the
// analyzed packages. A package only the host's build has is no member either, since the extra build has nothing
// of it to analyze.
//
// The blind spot is layout read without a name: `unsafe.Sizeof` of our type embedding a dependency's struct, as
// SA1027 and SA9006 read sizes and alignment, reaches no object whose digest moved, while the sizes behind it move
// with the architecture. It is sound only because every build is on the host's `GOARCH` (project.BuildsFor builds
// no other), so Closure refuses two builds on different architectures rather than miss it.
func Closure(host BuildLoad, extra BuildLoad) ([]Member, error) {
	if host.Build.Architecture != extra.Build.Architecture {
		return nil, fmt.Errorf("the extra build %s is on another `GOARCH` than the host's %s: layout read without a "+
			"name (`unsafe.Sizeof` of our type embedding a dependency's struct; SA1027, SA9006) reaches no object "+
			"whose digest moves, and would go unchecked", extra.Build, host.Build)
	}
	if host.Build.String() == extra.Build.String() {
		return nil, fmt.Errorf("the extra build %s is the host's build, so nothing can differ between them",
			extra.Build)
	}

	workspacePaths := map[string]bool{}
	for _, loaded := range slices.Concat(host.Packages, extra.Packages) {
		workspacePaths[loaded.PkgPath] = true
	}
	hostWorkspace := workspaceOf(host.Packages, workspacePaths)
	extraWorkspace := workspaceOf(extra.Packages, workspacePaths)

	reasons := map[string][]Reason{}
	for _, id := range slices.Sorted(maps.Keys(extraWorkspace)) {
		extraPackage := extraWorkspace[id]
		hostPackage, hostHas := hostWorkspace[id]
		if !hostHas || !slices.Equal(sortedFiles(hostPackage), sortedFiles(extraPackage)) {
			reasons[id] = append(reasons[id], Reason{Kind: ReasonKindSeed})
		}

		reached := map[ObjectKey]bool{}
		for _, each := range []*packages.Package{hostPackage, extraPackage} {
			if each == nil {
				continue
			}
			if each.TypesInfo == nil {
				return nil, fmt.Errorf("%s has no type information, so what it references cannot be read", each.ID)
			}
			addReached(reached, each, workspacePaths)
		}
		for _, key := range slices.SortedFunc(maps.Keys(reached), compareObjectKeys) {
			if digestsDiffer(host.ObjectDigests, extra.ObjectDigests, key) {
				reasons[id] = append(reasons[id], Reason{Kind: ReasonKindReference, Object: key})
			}
		}
	}

	importers := map[string][]string{}
	for _, id := range slices.Sorted(maps.Keys(extraWorkspace)) {
		for _, imported := range extraWorkspace[id].Imports {
			if _, inWorkspace := extraWorkspace[imported.ID]; inWorkspace {
				importers[imported.ID] = append(importers[imported.ID], id)
			}
		}
	}
	// Each member is followed once, whatever order it joined in, so every importer collects one reason per closure
	// package it imports whose output differs.
	pending := slices.Sorted(maps.Keys(reasons))
	for len(pending) > 0 {
		imported := pending[0]
		pending = pending[1:]
		if !digestsDiffer(host.PackageDigests, extra.PackageDigests, imported) {
			continue
		}
		for _, importer := range importers[imported] {
			if _, member := reasons[importer]; !member {
				pending = append(pending, importer)
			}
			reasons[importer] = append(reasons[importer], Reason{Kind: ReasonKindImporter, Imported: imported})
		}
	}

	analyzed := map[string]bool{}
	for _, loaded := range extra.Packages {
		analyzed[loaded.ID] = true
	}
	members := []Member{}
	for _, id := range slices.Sorted(maps.Keys(reasons)) {
		if analyzed[id] {
			members = append(members, Member{Id: id, Reasons: slices.SortedFunc(slices.Values(reasons[id]),
				compareReasons)})
		}
	}
	return members, nil
}

// workspaceOf is every workspace package of one build by load ID: given, and every package with a workspace path
// they import, directly or through one another, as the plain p "q" imports beside the analyzed "p [p.test]". No
// package outside the workspace imports one inside it, so the walk never leaves the workspace.
func workspaceOf(given []*packages.Package, workspacePaths map[string]bool) map[string]*packages.Package {
	workspace := map[string]*packages.Package{}
	var visit func(loaded *packages.Package)
	visit = func(loaded *packages.Package) {
		if _, seen := workspace[loaded.ID]; seen || !workspacePaths[loaded.PkgPath] {
			return
		}
		workspace[loaded.ID] = loaded
		for _, imported := range loaded.Imports {
			visit(imported)
		}
	}
	for _, loaded := range given {
		visit(loaded)
	}
	return workspace
}

// sortedFiles is the package's source files, sorted: GoFiles, the files the build compiles as written, its cgo
// files among them, rather than CompiledGoFiles, whose cgo outputs are named per run.
func sortedFiles(loaded *packages.Package) []string {
	return slices.Sorted(slices.Values(loaded.GoFiles))
}

// addReached adds the key of every object outside the workspace the package's Uses, Implicits and Selections reach,
// every named type outside it in the type of an expression of TypesInfo.Types or of an object of TypesInfo.Defs,
// and the package-facts key, an empty Path, of every package outside the workspace it imports, since an analyzer
// reads an imported package's facts with no identifier naming them.
//
// Mutates: reached gains each key
func addReached(reached map[ObjectKey]bool, loaded *packages.Package, workspacePaths map[string]bool) {
	for _, imported := range loaded.Imports {
		if !workspacePaths[imported.PkgPath] {
			reached[ObjectKey{PackagePath: imported.PkgPath}] = true
		}
	}
	add := func(object types.Object) {
		origin := originOf(object)
		if origin == nil || origin.Pkg() == nil || workspacePaths[origin.Pkg().Path()] {
			return
		}
		if key, keyed := KeyOf(origin); keyed {
			reached[key] = true
		}
	}
	for _, object := range loaded.TypesInfo.Uses {
		add(object)
	}
	for _, object := range loaded.TypesInfo.Implicits {
		// An implicit is a local, a type switch's binding or an unnamed parameter, or an import's package name, none
		// of which has a key: what it reaches of a dependency is its type.
		add(object)
		addNamedTypes(object.Type(), add)
	}
	for _, selection := range loaded.TypesInfo.Selections {
		add(selection.Obj())
		for _, field := range embeddedFieldsOf(selection) {
			add(field)
		}
	}
	// What no identifier names: h in `h := dependency.Value(); _ = h + 1` reads dependency.Handle, whose
	// underlying type the builds may read apart while Value's rendering stays the same, and only the types of the
	// expressions and of the objects declared say so.
	for _, typeAndValue := range loaded.TypesInfo.Types {
		addNamedTypes(typeAndValue.Type, add)
	}
	for _, object := range loaded.TypesInfo.Defs {
		if object != nil {
			addNamedTypes(object.Type(), add)
		}
	}
}

// originOf is the generic object an instantiated function, method or field was made from, which is what a key
// names, and any other object itself.
func originOf(object types.Object) types.Object {
	switch typed := object.(type) {
	case *types.Func:
		return typed.Origin()
	case *types.Var:
		return typed.Origin()
	}
	return object
}

// addNamedTypes passes add the type name of every named type and alias reached spells, through pointers, slices,
// arrays, maps, channels, signatures, tuples, unnamed structs, interfaces, unions and type arguments, but not
// through a named type's underlying type, which the named type's own digest covers.
func addNamedTypes(reached types.Type, add func(object types.Object)) {
	switch typed := reached.(type) {
	case *types.Named:
		add(typed.Obj())
		for typeArgument := range typed.TypeArgs().Types() {
			addNamedTypes(typeArgument, add)
		}
	case *types.Alias:
		add(typed.Obj())
		for typeArgument := range typed.TypeArgs().Types() {
			addNamedTypes(typeArgument, add)
		}
		addNamedTypes(types.Unalias(typed), add)
	case *types.Pointer:
		addNamedTypes(typed.Elem(), add)
	case *types.Slice:
		addNamedTypes(typed.Elem(), add)
	case *types.Array:
		addNamedTypes(typed.Elem(), add)
	case *types.Chan:
		addNamedTypes(typed.Elem(), add)
	case *types.Map:
		addNamedTypes(typed.Key(), add)
		addNamedTypes(typed.Elem(), add)
	case *types.Signature:
		addNamedTypes(typed.Params(), add)
		addNamedTypes(typed.Results(), add)
	case *types.Tuple:
		for variable := range typed.Variables() {
			addNamedTypes(variable.Type(), add)
		}
	case *types.Struct:
		for field := range typed.Fields() {
			addNamedTypes(field.Type(), add)
		}
	case *types.Interface:
		for method := range typed.Methods() {
			addNamedTypes(method.Type(), add)
		}
		for embedded := range typed.EmbeddedTypes() {
			addNamedTypes(embedded, add)
		}
	case *types.Union:
		for index := range typed.Len() {
			addNamedTypes(typed.Term(index).Type(), add)
		}
	}
}

// embeddedFieldsOf is every embedded field a selection's path passes through before the field or method it
// selects, as Selection.Index walks them from its receiver: for ours.Field promoted through Outer's Inner, the
// fields Outer and Inner.
func embeddedFieldsOf(selection *types.Selection) []*types.Var {
	var fields []*types.Var
	current := selection.Recv()
	path := selection.Index()
	for _, index := range path[:len(path)-1] {
		current = types.Unalias(current)
		if pointer, isPointer := current.(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStruct := current.Underlying().(*types.Struct)
		if !isStruct || index >= structure.NumFields() {
			// Invariant: every index before the last is a field of a struct, so this is a selection the type checker
			// did not make; the fields read so far are what can be said of it.
			break
		}
		field := structure.Field(index)
		fields = append(fields, field)
		current = field.Type()
	}
	return fields
}

// digestsDiffer reports whether two builds' digests of key differ: either lacks it, both do, or they are not equal.
func digestsDiffer[Key comparable](host map[Key]cache.Key, extra map[Key]cache.Key, key Key) bool {
	hostDigest, hostHas := host[key]
	extraDigest, extraHas := extra[key]
	return !hostHas || !extraHas || hostDigest != extraDigest
}

// compareObjectKeys orders keys by package path, then by path inside the package.
func compareObjectKeys(left ObjectKey, right ObjectKey) int {
	return cmp.Or(strings.Compare(left.PackagePath, right.PackagePath),
		strings.Compare(string(left.Path), string(right.Path)))
}

// reasonKindOrder is the order a member's reasons are listed in: what it is, then what it reaches, then what it
// imports.
var reasonKindOrder = []ReasonKind{ReasonKindSeed, ReasonKindReference, ReasonKindImporter}

// compareReasons orders reasons by kind (reasonKindOrder), then by object, then by the package imported.
func compareReasons(left Reason, right Reason) int {
	return cmp.Or(cmp.Compare(slices.Index(reasonKindOrder, left.Kind), slices.Index(reasonKindOrder, right.Kind)),
		compareObjectKeys(left.Object, right.Object), strings.Compare(left.Imported, right.Imported))
}
