package check

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
)

// closureRendering is the closure an object's digest stands for, written out whole, the oracle the digests are held
// to: its own line, then every named type and alias reachable from its type, each once, sorted by package path, name
// and lines, each as its path and name and its lines (renderedTypeName), an unexported method kept only where an
// importer sees it (oracleSees), and, with sizes, another package's unexported field by its properties alone
// (directTypeNames). It walks every closure from scratch, as the verdicts once did, so it is the test's alone.
func closureRendering(object types.Object, sizes types.Sizes) []string {
	lines := append([]string{ownRendering(object)}, directTypeNames(rootTypes(object), sizes).observed...)
	type renderedName struct {
		path  string
		name  string
		lines []string
	}
	var rendered []renderedName
	for _, typeName := range closureTypeNames(rootTypes(object), sizes) {
		rendered = append(rendered, renderedName{path: typeName.Pkg().Path(), name: typeName.Name(),
			lines: append(renderedTypeName(typeName, oracleSees),
				directTypeNames(declarationTypes(typeName, oracleSees), sizes).observed...)})
	}
	slices.SortFunc(rendered, func(left, right renderedName) int {
		return cmp.Or(cmp.Compare(left.path, right.path), cmp.Compare(left.name, right.name),
			slices.Compare(left.lines, right.lines))
	})
	for _, typeName := range rendered {
		lines = append(lines, "named "+typeName.path+"."+typeName.name)
		lines = append(lines, typeName.lines...)
	}
	return lines
}

// oracleSees is whether the oracle keeps a method of a type name, as the graph does, asked of its package from scratch.
func oracleSees(method *types.Func) bool {
	return importerSeesMethod(method, declaresUnexportedInterface)
}

// closureTypeNames are the named types and aliases roots reach, walked from scratch: those roots name, and those their
// declarations name, each once.
func closureTypeNames(roots []types.Type, sizes types.Sizes) []*types.TypeName {
	seen := map[*types.TypeName]bool{}
	var reached []*types.TypeName
	pending := directTypeNames(roots, sizes).typeNames
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		reached = append(reached, current)
		pending = append(pending, directTypeNames(declarationTypes(current, oracleSees), sizes).typeNames...)
	}
	return reached
}

// comparedObjects are the objects of checked a closure digest is asked of, by a name that finds the same object in
// another check: every package-level object, and every method and struct field of each named type.
func comparedObjects(checked *types.Package) map[string]types.Object {
	objects := map[string]types.Object{}
	for _, name := range checked.Scope().Names() {
		object := checked.Scope().Lookup(name)
		objects[name] = object
		named, isNamed := object.Type().(*types.Named)
		if _, isTypeName := object.(*types.TypeName); !isTypeName || !isNamed {
			continue
		}
		for method := range named.Methods() {
			objects[name+"."+method.Name()] = method
		}
		if structure, isStruct := named.Underlying().(*types.Struct); isStruct {
			for field := range structure.Fields() {
				objects[name+"#"+field.Name()] = field
			}
		}
	}
	return objects
}

// cycleSources are pairs of one package each, every pair telling something apart or nothing, over cycles of types, a
// generic, an alias and a constant, so the digests' components meet every shape the closure walk follows.
var cycleSources = [][2]string{
	{
		"type A struct{ next *B }\n\ntype B struct{ back *A; count int }\n\nfunc F() A { return A{} }\n",
		"type A struct{ next *B }\n\ntype B struct{ back *A; count int64 }\n\nfunc F() A { return A{} }\n",
	},
	{
		"type Node struct{ children []*Node }\n\nfunc (Node) Walk(func(*Node)) {}\n",
		"type Node struct{ children []*Node }\n\nfunc (Node) Walk(func(*Node)) {}\n",
	},
	{
		"type Box[T any] struct{ value T }\n\ntype Alias = Box[int]\n\nvar V Alias\n",
		"type Box[T comparable] struct{ value T }\n\ntype Alias = Box[int]\n\nvar V Alias\n",
	},
	{
		"const C = 1\n\ntype I interface{ M() J }\n\ntype J interface{ N() I }\n",
		"const C = 2\n\ntype I interface{ M() J }\n\ntype J interface{ N() I }\n",
	},
}

// Two checks' closure digests of an object are equal exactly when its closures written out whole are: over the verdict
// fixture's darwin, linux and darwin again, and over pairs of cycles, generics, aliases and constants, every object,
// method and field compared tells apart the same pairs both ways.
func TestAClosureDigestIsEqualExactlyWhenItsWholeClosureIs(test *testing.T) {
	test.Parallel()

	type pair struct {
		name   string
		before *types.Package
		after  *types.Package
	}
	darwin := checkedWorld(test, "darwin", "").platform
	pairs := []pair{
		{"darwin and linux", darwin, checkedWorld(test, "linux", "").platform},
		{"darwin twice", darwin, checkedWorld(test, "darwin", "").platform},
	}
	for index, sources := range cycleSources {
		pairs = append(pairs, pair{fmt.Sprintf("cycles %d", index),
			checkedInMemory(test, renderingPath, "package rendered\n\n"+sources[0], nil),
			checkedInMemory(test, renderingPath, "package rendered\n\n"+sources[1], nil)})
	}
	differing := 0
	// Each pair is compared by a memo told no build, which enters every field, and by one told a build's sizes,
	// which renders another package's unexported field by its properties.
	for _, sizes := range []types.Sizes{nil, types.SizesFor("gc", "amd64")} {
		for _, compared := range pairs {
			beforeBuild, afterBuild := NewReadsBuild(), NewReadsBuild()
			beforeBuild.measureBy(sizes)
			afterBuild.measureBy(sizes)
			before, after := comparedObjects(compared.before), comparedObjects(compared.after)
			for _, name := range slices.Sorted(maps.Keys(before)) {
				afterObject, inBoth := after[name]
				if !inBoth {
					continue
				}
				renderedAlike := slices.Equal(closureRendering(before[name], sizes),
					closureRendering(afterObject, sizes))
				digestedAlike := beforeBuild.closureDigest(before[name]) == afterBuild.closureDigest(afterObject)
				if renderedAlike != digestedAlike {
					test.Errorf("%s, %s, sized %t: the closures written out are alike %t, the digests %t",
						compared.name, name, sizes != nil, renderedAlike, digestedAlike)
				}
				if !renderedAlike {
					differing++
				}
			}
		}
	}
	if differing == 0 {
		test.Error("no object's closure differed in any pair, so nothing tells the digests apart")
	}
}

// deepSource is a package of many functions and variables that all reach one deep chain of types, a cycle among them,
// so a verdict over all of them meets each type many times over.
func deepSource() string {
	var source strings.Builder
	source.WriteString("package deep\n\n")
	const depth = 12
	for level := range depth {
		fmt.Fprintf(&source, "type Level%d struct {\n\tnext *Level%d\n\tcount int\n}\n\n", level, (level+1)%depth)
		fmt.Fprintf(&source, "func (Level%d) Size() int { return 0 }\n\n", level)
	}
	for index := range 40 {
		fmt.Fprintf(&source, "func Make%d() Level%d { return Level%d{} }\n\n", index, index%depth, index%depth)
		fmt.Fprintf(&source, "var Held%d *Level%d\n\n", index, (index*5)%depth)
	}
	return source.String()
}

// deepUser is a package that reads every function and variable deepSource declares.
func deepUser() string {
	var source strings.Builder
	source.WriteString("package user\n\nimport \"example.com/deep\"\n\nfunc Use() {\n")
	for index := range 40 {
		fmt.Fprintf(&source, "\t_ = deep.Make%d()\n\t_ = deep.Held%d\n", index, index)
	}
	source.WriteString("}\n")
	return source.String()
}

// deepWorld is deepSource checked, and deepUser over it with its type information.
func deepWorld(test *testing.T) (*types.Package, *types.Package, *types.Info) {
	test.Helper()

	fileSet := token.NewFileSet()
	parse := func(name string, source string) *ast.File {
		parsed, parseError := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		return parsed
	}
	deep, deepError := (&types.Config{}).Check("example.com/deep", fileSet, []*ast.File{parse("deep.go",
		deepSource())}, nil)
	if deepError != nil {
		test.Fatal(deepError)
	}
	info := &types.Info{
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(string) (*types.Package, error) { return deep, nil })}
	user, userError := configuration.Check("example.com/user", fileSet, []*ast.File{parse("user.go", deepUser())},
		info)
	if userError != nil {
		test.Fatal(userError)
	}
	return deep, user, info
}

// A whole verdict over a package that reads eighty objects of another package, every one reaching one deep chain of
// types, renders each named type once in each build, however many objects reach it, and the reads hold, since both
// builds check the same source.
func TestAVerdictRendersEachNamedTypeOncePerBuild(test *testing.T) {
	test.Parallel()

	_, user, info := deepWorld(test)
	built, _, _ := deepWorld(test)
	reads := &build_reads.Reads{}
	reads.RecordTypes(user, info)
	if objects := len(reads.Objects()); objects < 80 {
		test.Fatalf("the user read %d objects of deep, want every one of its 80", objects)
	}
	host, build := NewReadsBuild(), NewReadsBuild()
	counted := func(rendered map[*types.TypeName]int) func(*types.TypeName) {
		return func(typeName *types.TypeName) { rendered[typeName]++ }
	}
	hostRendered, buildRendered := map[*types.TypeName]int{}, map[*types.TypeName]int{}
	host.graph.rendered, build.graph.rendered = counted(hostRendered), counted(buildRendered)
	restoring := Restoring{Path: "example.com/user", Imports: []string{"example.com/deep"}, Reads: reads}
	holds, why := TypesHold(restoring, map[string]*types.Package{"example.com/deep": built}, nil, host, build)
	if !holds {
		test.Fatalf("the reads of one source checked twice do not hold: %s", why)
	}
	for side, rendered := range map[string]map[*types.TypeName]int{"host": hostRendered, "build": buildRendered} {
		if len(rendered) == 0 {
			test.Errorf("the %s rendered no named type", side)
		}
		for typeName, times := range rendered {
			if times != 1 {
				test.Errorf("the %s rendered %s %d times, want once", side, typeName.Name(), times)
			}
		}
	}
}
