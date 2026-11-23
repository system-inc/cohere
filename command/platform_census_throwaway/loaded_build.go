package main

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

// loadMode type-checks every package of the graph from source, standard library included, since a fork in std is as
// much a fork as one in ours.
const loadMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports |
	packages.NeedDeps | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedModule

// firstErrorsKept is how many of a load's package errors the report spells out.
const firstErrorsKept = 10

// The groups every package is counted in.
const (
	groupOurs            = "ours"
	groupDependencies    = "dependencies"
	groupStandardLibrary = "standardLibrary"
)

// groupNames is every group in the order the table prints them.
var groupNames = []string{groupOurs, groupDependencies, groupStandardLibrary}

// standardLibraryRule says how a package is told to be std, for the report.
const standardLibraryRule = "std when go/packages gives it no module and the first element of its path has no dot; " +
	"ours when its path is one the patterns matched (a test variant's path included); every other package is a " +
	"dependency"

// loadedBuild is one configuration's whole graph, every package type-checked from source.
type loadedBuild struct {
	configuration configuration
	cgoEnabled    bool
	fileSet       *token.FileSet
	// packagesByIdentifier is every package of the graph by its id, test variants included.
	packagesByIdentifier map[string]*packages.Package
	// rootPaths are the paths of the packages the patterns matched, which are ours.
	rootPaths map[string]bool
	// typeStrings caches each type's string, qualified by package path, since a census asks for one many times.
	typeStrings map[types.Type]string
	// memberOwners maps each field of a package-level struct type and each method of a package-level interface type,
	// as its origin, to the type's declaration, so a use of the member reaches the declaration that holds it.
	memberOwners   map[types.Object]declarationKey
	packageErrors  int
	firstErrors    []string
	loadDuration   time.Duration
	sortedPackages []*packages.Package
}

// loadBuild loads patterns in root under build, with tests when asked, reading the workspace workFile names.
func loadBuild(root string, patterns []string, tests bool, build configuration, workFile string) (*loadedBuild, error) {
	environment := build.environment(workFile)
	cgoOn, cgoError := cgoEnabled(root, environment)
	if cgoError != nil {
		return nil, cgoError
	}
	loaded := &loadedBuild{
		configuration:        build,
		cgoEnabled:           cgoOn,
		fileSet:              token.NewFileSet(),
		packagesByIdentifier: map[string]*packages.Package{},
		rootPaths:            map[string]bool{},
		typeStrings:          map[types.Type]string{},
		memberOwners:         map[types.Object]declarationKey{},
	}
	var buildFlags []string
	if len(build.tags) != 0 {
		buildFlags = []string{"-tags=" + strings.Join(build.tags, ",")}
	}
	started := time.Now()
	roots, loadError := packages.Load(&packages.Config{
		Mode:       loadMode,
		Dir:        root,
		Env:        environment,
		BuildFlags: buildFlags,
		Tests:      tests,
		Fset:       loaded.fileSet,
	}, patterns...)
	loaded.loadDuration = time.Since(started)
	if loadError != nil {
		return nil, fmt.Errorf("loading %s: %w", build.target(cgoOn), loadError)
	}
	for _, root := range roots {
		loaded.rootPaths[root.PkgPath] = true
	}
	packages.Visit(roots, nil, func(visited *packages.Package) {
		loaded.packagesByIdentifier[visited.ID] = visited
		for _, packageError := range visited.Errors {
			loaded.packageErrors++
			if len(loaded.firstErrors) < firstErrorsKept {
				loaded.firstErrors = append(loaded.firstErrors, visited.ID+": "+packageError.Error())
			}
		}
	})
	for _, each := range loaded.packagesByIdentifier {
		loaded.sortedPackages = append(loaded.sortedPackages, each)
	}
	// Sorted by id, a package comes before its test variant ("p" before "p [p.test]"), so a file both compile is
	// counted under the package itself.
	sort.Slice(loaded.sortedPackages, func(left int, right int) bool {
		return loaded.sortedPackages[left].ID < loaded.sortedPackages[right].ID
	})
	return loaded, nil
}

// group is the group a package of this build is counted in, by standardLibraryRule.
func (loaded *loadedBuild) group(counted *packages.Package) string {
	if loaded.rootPaths[counted.PkgPath] {
		return groupOurs
	}
	first, _, _ := strings.Cut(counted.PkgPath, "/")
	if counted.Module == nil && !strings.Contains(first, ".") {
		return groupStandardLibrary
	}
	return groupDependencies
}

// typeString is typed's canonical string, or `<nil>` for no type: two types of two builds have one string when
// types.Identical would call them identical, which it can't be asked across two loads. So a signature drops its
// parameters' and results' names, an alias is its target, byte is uint8, and every package is spelled by its path.
func (loaded *loadedBuild) typeString(typed types.Type) string {
	if typed == nil {
		return "<nil>"
	}
	if spelled, cached := loaded.typeStrings[typed]; cached {
		return spelled
	}
	var builder strings.Builder
	writeCanonicalType(&builder, typed)
	spelled := builder.String()
	loaded.typeStrings[typed] = spelled
	return spelled
}

// channelOpenings begins a channel type's spelling by its direction, its element in parentheses so `chan (<-chan int)`
// and `chan<- chan int` never spell alike.
var channelOpenings = [...]string{types.SendRecv: "chan(", types.SendOnly: "chan<-(", types.RecvOnly: "<-chan("}

// writeCanonicalType writes typed as typeString spells it. A named type is its path, name and type arguments, never its
// underlying type, so the walk ends however a type refers to itself.
//
// Mutates: builder gets the spelling.
func writeCanonicalType(builder *strings.Builder, typed types.Type) {
	switch canonical := types.Unalias(typed).(type) {
	case nil:
		builder.WriteString("<nil>")
	case *types.Basic:
		// byte and rune are their own Basics in go/types, identical to uint8 and int32.
		builder.WriteString(types.Typ[canonical.Kind()].Name())
	case *types.Pointer:
		builder.WriteString("*")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Slice:
		builder.WriteString("[]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Array:
		builder.WriteString("[" + strconv.FormatInt(canonical.Len(), 10) + "]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Map:
		builder.WriteString("map[")
		writeCanonicalType(builder, canonical.Key())
		builder.WriteString("]")
		writeCanonicalType(builder, canonical.Elem())
	case *types.Chan:
		builder.WriteString(channelOpenings[canonical.Dir()])
		writeCanonicalType(builder, canonical.Elem())
		builder.WriteString(")")
	case *types.Struct:
		builder.WriteString("struct{")
		for index := range canonical.NumFields() {
			field := canonical.Field(index)
			if index > 0 {
				builder.WriteString("; ")
			}
			if field.Embedded() {
				builder.WriteString("embedded ")
			}
			// An unexported field is identical only to one of the same package, as types.Identical has it.
			if !field.Exported() && field.Pkg() != nil {
				builder.WriteString(field.Pkg().Path() + ".")
			}
			builder.WriteString(field.Name() + " ")
			writeCanonicalType(builder, field.Type())
			if tag := canonical.Tag(index); tag != "" {
				builder.WriteString(" " + strconv.Quote(tag))
			}
		}
		builder.WriteString("}")
	case *types.Signature:
		writeCanonicalSignature(builder, canonical)
	case *types.Interface:
		builder.WriteString("interface{")
		// A complete interface's methods are its whole method set, sorted, embedded interfaces' included.
		for index := range canonical.NumMethods() {
			method := canonical.Method(index)
			if index > 0 {
				builder.WriteString("; ")
			}
			if !method.Exported() && method.Pkg() != nil {
				builder.WriteString(method.Pkg().Path() + ".")
			}
			builder.WriteString(method.Name())
			writeCanonicalSignature(builder, method.Signature())
		}
		for index := range canonical.NumEmbeddeds() {
			// An embedded interface's methods are above; anything else, a union or a type, is a term of the type set.
			embedded := canonical.EmbeddedType(index)
			if _, isInterface := embedded.Underlying().(*types.Interface); isInterface {
				continue
			}
			builder.WriteString("; ")
			writeCanonicalType(builder, embedded)
		}
		builder.WriteString("}")
	case *types.Union:
		for index := range canonical.Len() {
			term := canonical.Term(index)
			if index > 0 {
				builder.WriteString("|")
			}
			if term.Tilde() {
				builder.WriteString("~")
			}
			writeCanonicalType(builder, term.Type())
		}
	case *types.Named:
		name := canonical.Obj()
		if name.Pkg() != nil {
			builder.WriteString(name.Pkg().Path() + ".")
		}
		builder.WriteString(name.Name())
		if arguments := canonical.TypeArgs(); arguments.Len() > 0 {
			builder.WriteString("[")
			for index := range arguments.Len() {
				if index > 0 {
					builder.WriteString(", ")
				}
				writeCanonicalType(builder, arguments.At(index))
			}
			builder.WriteString("]")
		}
	case *types.TypeParam:
		builder.WriteString(canonical.Obj().Name())
	case *types.Tuple:
		writeCanonicalTuple(builder, canonical, false)
	default:
		builder.WriteString(types.TypeString(canonical, func(named *types.Package) string { return named.Path() }))
	}
}

// writeCanonicalSignature writes signature's type parameters' constraints, its parameters' types and its results'
// types, never its receiver or any name, which types.Identical ignores.
//
// Mutates: builder gets the spelling.
func writeCanonicalSignature(builder *strings.Builder, signature *types.Signature) {
	builder.WriteString("func")
	if parameters := signature.TypeParams(); parameters.Len() > 0 {
		builder.WriteString("[")
		for index := range parameters.Len() {
			if index > 0 {
				builder.WriteString(", ")
			}
			writeCanonicalType(builder, parameters.At(index).Constraint())
		}
		builder.WriteString("]")
	}
	writeCanonicalTuple(builder, signature.Params(), signature.Variadic())
	writeCanonicalTuple(builder, signature.Results(), false)
}

// writeCanonicalTuple writes tuple's types in parentheses, the last as `...` and its element when variadic.
//
// Mutates: builder gets the spelling.
func writeCanonicalTuple(builder *strings.Builder, tuple *types.Tuple, variadic bool) {
	builder.WriteString("(")
	for index := range tuple.Len() {
		if index > 0 {
			builder.WriteString(", ")
		}
		typed := tuple.At(index).Type()
		if variadic && index == tuple.Len()-1 {
			// A variadic parameter's type is a slice of its element.
			if slice, isSlice := typed.(*types.Slice); isSlice {
				builder.WriteString("...")
				typed = slice.Elem()
			}
		}
		writeCanonicalType(builder, typed)
	}
	builder.WriteString(")")
}

// answerOf is what the census compares of an expression: its type, and its constant value when it has one.
func (loaded *loadedBuild) answerOf(typeAndValue types.TypeAndValue) string {
	answer := loaded.typeString(typeAndValue.Type)
	if typeAndValue.Value != nil {
		answer += " = " + typeAndValue.Value.ExactString()
	}
	return answer
}

// fileName is the name of the file position is in, empty when the file set holds none.
func (loaded *loadedBuild) fileName(position token.Pos) string {
	file := loaded.fileSet.File(position)
	if file == nil {
		return ""
	}
	return file.Name()
}

// declarationsOf is every package-level object of counted and every method of its package-level named types, each with
// its answer, and records each member of its named types in loaded's memberOwners. A package that was not
// type-checked declares nothing.
func (loaded *loadedBuild) declarationsOf(counted *packages.Package) map[string]string {
	answers := map[string]string{}
	if counted.Types == nil {
		return answers
	}
	scope := counted.Types.Scope()
	for _, name := range scope.Names() {
		object := scope.Lookup(name)
		answers[name] = loaded.objectAnswer(object)
		typeName, isTypeName := object.(*types.TypeName)
		if !isTypeName || typeName.IsAlias() {
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if !isNamed {
			continue
		}
		owner := declarationKey{packagePath: counted.PkgPath, name: name}
		for index := range named.NumMethods() {
			method := named.Method(index)
			answers[name+"."+method.Name()] = "method " + loaded.typeString(method.Type())
		}
		switch underlying := named.Underlying().(type) {
		case *types.Struct:
			for index := range underlying.NumFields() {
				loaded.memberOwners[underlying.Field(index)] = owner
			}
		case *types.Interface:
			for index := range underlying.NumExplicitMethods() {
				loaded.memberOwners[underlying.ExplicitMethod(index)] = owner
			}
		}
	}
	return answers
}

// objectAnswer is what the census compares of a package-level object: its kind, its type with packages spelled by
// path, a constant's value, and a named type's underlying type, since a named type's own string is its name alone and
// would hide a `type Handle uintptr` that is an int elsewhere.
func (loaded *loadedBuild) objectAnswer(object types.Object) string {
	switch typed := object.(type) {
	case *types.Const:
		return "const " + loaded.typeString(typed.Type()) + " = " + typed.Val().ExactString()
	case *types.TypeName:
		if typed.IsAlias() {
			return "alias " + loaded.typeString(typed.Type())
		}
		return "type " + loaded.typeString(typed.Type()) + " " + loaded.typeString(typed.Type().Underlying())
	case *types.Var:
		return "var " + loaded.typeString(typed.Type())
	case *types.Func:
		return "func " + loaded.typeString(typed.Type())
	default:
		return "other " + loaded.typeString(object.Type())
	}
}

// declarationOf is the declaration object names when it is a package-level object, a method of a package-level named
// type, or a member of one, and false for anything else: a local, a parameter, a builtin, a package name.
func (loaded *loadedBuild) declarationOf(object types.Object) (declarationKey, bool) {
	if object == nil || object.Pkg() == nil {
		return declarationKey{}, false
	}
	// A member of a generic type is reached through an instance, whose members are copies of the origin's.
	origin := object
	switch typed := object.(type) {
	case *types.Var:
		origin = typed.Origin()
	case *types.Func:
		origin = typed.Origin()
	}
	if owner, owned := loaded.memberOwners[origin]; owned {
		return owner, true
	}
	if function, isFunction := origin.(*types.Func); isFunction && function.Signature().Recv() != nil {
		receiver := function.Signature().Recv().Type()
		if pointer, isPointer := receiver.(*types.Pointer); isPointer {
			receiver = pointer.Elem()
		}
		named, isNamed := receiver.(*types.Named)
		if !isNamed {
			return declarationKey{}, false
		}
		methodName := named.Origin().Obj().Name() + "." + origin.Name()
		return declarationKey{packagePath: origin.Pkg().Path(), name: methodName}, true
	}
	if origin.Parent() != origin.Pkg().Scope() {
		return declarationKey{}, false
	}
	return declarationKey{packagePath: origin.Pkg().Path(), name: origin.Name()}, true
}
