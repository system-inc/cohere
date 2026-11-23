package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/export_data"
)

// The agreement fixture's paths: other declares the types fixture reaches in another package, and fixture the rest.
const (
	agreementOtherPath   = "example.com/agreement/other"
	agreementFixturePath = "example.com/agreement/fixture"
)

// agreementPaths are the fixture's packages, each after what it imports.
var agreementPaths = []string{agreementOtherPath, agreementFixturePath}

// agreementSources are the fixture's packages by path, written to exercise everything the renderings read: generics
// with constraints, plain and generic aliases, embedded structs and interfaces, unexported fields and methods on
// pointer and value receivers, interfaces with unexported methods an importer reaches, constants of every kind, struct
// tags, channels, function types, recursive and mutually recursive types, and other's types reached from fixture.
// Every unexported declaration is reached from an exported one, so shallow export data holds each and the test
// compares every object of the source.
var agreementSources = map[string]string{
	agreementOtherPath: `package other

type Thing struct {
	Name  string ` + "`json:\"name\"`" + `
	count int
}

func (thing *Thing) Count() int { return thing.count }

func (thing Thing) label() string { return thing.Name }

type Sizer interface {
	Size() int
	size() int
}

type Box[T any] struct{ Value T }

func (box *Box[T]) Put(value T) { box.Value = value }

const Limit = 1 << 70
`,
	agreementFixturePath: `package fixture

import "example.com/agreement/other"

type Number interface {
	~int | ~int64 | ~float64
}

type Pair[K comparable, V Number] struct {
	Key   K ` + "`json:\"key\"`" + `
	Value V ` + "`json:\"value,omitempty\" xml:\"value\"`" + `
	note  string
}

func (pair *Pair[K, V]) Set(value V) { pair.Value = value }

func (pair Pair[K, V]) Get() V { return pair.Value }

func (pair Pair[K, V]) describe() string { return pair.note }

func Sum[T Number](values ...T) T {
	var total T
	for _, value := range values {
		total += value
	}
	return total
}

type Ordered[T interface {
	~int | ~string
	String() string
}] []T

func Join[S ~[]E, E interface{ String() string }](items S) string {
	joined := ""
	for _, item := range items {
		joined += item.String()
	}
	return joined
}

type Thing = other.Thing

type Set[T comparable] = map[T]struct{}

type Boxed[T any] = other.Box[T]

type Ints = Set[int]

type Wrapper struct {
	other.Thing
	*Pair[string, float64]
	inner   counter
	Handler Handler
	Events  <-chan other.Box[int]
	Sink    chan<- error
	Both    chan struct{ hidden int }
}

func (wrapper Wrapper) Value() float64 { return wrapper.Pair.Value }

func (wrapper *Wrapper) bump() { wrapper.inner.count++ }

type counter struct {
	count int
	label Label
}

func (counter *counter) Add(delta int) int {
	counter.count += delta
	return counter.count
}

func (counter counter) peek() int { return counter.count }

func NewCounter() *counter { return &counter{} }

type Handler func(name string, values ...int) (int, error)

type ReadSizer interface {
	other.Sizer
	Read(buffer []byte) (int, error)
	reset()
}

type measurer interface{ measure() int }

func Measure(value measurer) int { return value.measure() }

type Node struct {
	Next     *Node
	Children []*Node
	Values   map[string][]other.Box[*Node]
}

type Tree struct{ Root *Branch }

type Branch struct {
	Tree   *Tree
	leaves []Pair[int, int]
}

type Grid [3][4]other.Box[Pair[int, int]]

type Label string

func (label Label) String() string { return string(label) }

var Anonymous struct {
	hidden int
	Shown  string ` + "`xml:\"shown\"`" + `
}

var Callbacks map[string]func(other.Sizer) error

const (
	Typed    int64      = 42
	Untyped             = 7
	Big                 = 1 << 100
	Negative            = -Big * Big
	Pi                  = 3.14159265358979323846264338327950288419716939937510582097494459
	Third               = 1.0 / 3
	Small    float32    = 0.1
	Huge                = 1e1000
	Tiny                = 1e-1000
	Complex             = 1 + 2i
	Wave                = Pi + Third*1i
	Typed128 complex128 = 3 - 4i
	Greeting            = "hello, \"world\"\n\u00e9"
	Named    Label      = "named"
	Flag                = true
	Letter              = 'x'
	Limit               = other.Limit
)
`,
}

// agreementUniverses are the fixture's packages checked from source, as the host holds them, and the same packages
// as a build holds them restored from the host: each one's export data written as the keep writes it
// (writeExportData), and read as the build's loader reads it (export_data.Read) against a closure of its own whose
// imports were read the same way before it, its imports set as the loader sets them. Both by path.
func agreementUniverses(test *testing.T) (map[string]*types.Package, map[string]*types.Package) {
	test.Helper()

	fileSet := token.NewFileSet()
	source := map[string]*types.Package{}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if imported, found := source[path]; found {
			return imported, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	for _, path := range agreementPaths {
		parsed, parseError := parser.ParseFile(fileSet, path+".go", agreementSources[path], parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		checked, checkError := configuration.Check(path, fileSet, []*ast.File{parsed}, nil)
		if checkError != nil {
			test.Fatalf("type-checking %s: %v", path, checkError)
		}
		source[path] = checked
	}

	closure := map[string]*types.Package{}
	for _, path := range agreementPaths {
		exportData, writeError := writeExportData(&packages.Package{ID: path, PkgPath: path, Fset: fileSet,
			Types: source[path]})
		if writeError != nil {
			test.Fatal(writeError)
		}
		read, readError := export_data.Read(token.NewFileSet(), closure, exportData, path)
		if readError != nil {
			test.Fatal(readError)
		}
		ownImports := make([]*types.Package, 0, len(source[path].Imports()))
		for _, imported := range source[path].Imports() {
			ownImport, found := closure[imported.Path()]
			if !found {
				test.Fatalf("%s imports %s, which was not read before it", path, imported.Path())
			}
			ownImports = append(ownImports, ownImport)
		}
		read.SetImports(ownImports)
	}
	return source, closure
}

// agreementObjects are the objects of a package the renderings read, each by a label naming it: every package-level
// object, every method declared on a defined type, every explicit method of an interface and every field of a struct
// a defined type declares, in the order a walk of the scope meets them, so the two sides' lists line up by label.
func agreementObjects(checked *types.Package) ([]string, []types.Object) {
	var labels []string
	var objects []types.Object
	add := func(label string, object types.Object) {
		labels = append(labels, label)
		objects = append(objects, object)
	}
	for _, name := range checked.Scope().Names() {
		object := checked.Scope().Lookup(name)
		add(name, object)
		typeName, isTypeName := object.(*types.TypeName)
		if !isTypeName || typeName.IsAlias() {
			// An alias's methods and fields are its target's, compared where the target is declared.
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if !isNamed {
			continue
		}
		for method := range named.Methods() {
			add(name+"."+method.Name(), method)
		}
		switch underlying := named.Underlying().(type) {
		case *types.Interface:
			for method := range underlying.ExplicitMethods() {
				add(name+" interface method "+method.Name(), method)
			}
		case *types.Struct:
			for field := range underlying.Fields() {
				add(name+" field "+field.Name(), field)
			}
		}
	}
	return labels, objects
}

// agreementLines are the rendering lines the decide reads of object in build: its own line and the lines its closure
// digest's own part adds for fields rendered by their properties, and for a type name its node's lines, its methods an
// importer sees and the lines each kind of use reads of it.
func agreementLines(build *ReadsBuild, object types.Object) []string {
	lines := build.useLines(object, build_reads.UseMember)
	typeName, isTypeName := object.(*types.TypeName)
	if !isTypeName {
		return lines
	}
	sizes := build.graph.measuredSizes()
	lines = append(lines, renderedTypeName(typeName, build.graph.importerSees)...)
	lines = append(lines, directTypeNames(declarationTypes(typeName, build.graph.importerSees), sizes).observed...)
	for _, kind := range []build_reads.UseKind{build_reads.UseName, build_reads.UseUnderlying,
		build_reads.UseMethodSet} {
		lines = append(lines, build.useLines(object, kind)...)
	}
	return lines
}

// A build reuses the host's renderings and closure digests for the packages it restored from the host, whose types it
// reads from the export data the host's check of them wrote. That is sound only if a package read from its own export
// data renders and digests exactly as the same package checked from source does, so for every object and method of
// the fixture, the rendering lines and every digest of each kind of use read the same from both, every field rendered
// whole and every unexported field of another package by its properties under amd64's sizes.
func TestTypesRenderAndDigestAlikeFromSourceAndExportData(test *testing.T) {
	test.Parallel()

	source, exported := agreementUniverses(test)
	for name, sizes := range map[string]types.Sizes{"whole": nil, "measured": types.SizesFor("gc", "amd64")} {
		test.Run(name, func(test *testing.T) {
			test.Parallel()

			host, build := NewReadsBuild(), NewReadsBuild()
			host.measureBy(sizes)
			build.measureBy(sizes)
			for _, path := range agreementPaths {
				sourceLabels, sourceObjects := agreementObjects(source[path])
				exportLabels, exportObjects := agreementObjects(exported[path])
				if !slices.Equal(sourceLabels, exportLabels) {
					test.Fatalf("%s holds other objects from export data than from source, so the fixture leaves "+
						"some out:\nsource %v\nexport data %v", path, sourceLabels, exportLabels)
				}
				for index, label := range sourceLabels {
					fromSource := agreementLines(host, sourceObjects[index])
					fromExport := agreementLines(build, exportObjects[index])
					if !slices.Equal(fromSource, fromExport) {
						test.Errorf("%s.%s renders apart:\nsource\n%s\nexport data\n%s", path, label,
							strings.Join(fromSource, "\n"), strings.Join(fromExport, "\n"))
					}
					for _, kind := range []build_reads.UseKind{build_reads.UseWhole, build_reads.UseName,
						build_reads.UseMember, build_reads.UseUnderlying, build_reads.UseMethodSet} {
						if host.useDigest(sourceObjects[index], kind) != build.useDigest(exportObjects[index], kind) {
							test.Errorf("%s.%s digests apart as a use of kind %s, its closure written whole:\n"+
								"source\n%s\nexport data\n%s", path, label, kind,
								strings.Join(closureRendering(sourceObjects[index], sizes), "\n"),
								strings.Join(closureRendering(exportObjects[index], sizes), "\n"))
						}
					}
				}
			}
		})
	}
}
