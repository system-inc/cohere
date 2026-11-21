package unused_code

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
)

// recordedSource is one file of a package a record test type-checks: the name it is parsed under and its source.
type recordedSource struct {
	name   string
	source string
}

// recordedPackage is a package built from sources with a recorder on: its graph, entered, and what a test needs to
// read a record against the source, each parsed file's syntax, token file and bytes by the name it was parsed under.
type recordedPackage struct {
	graph      *graph
	files      []*ast.File
	tokenFiles map[string]*token.File
	sources    map[string]string
}

// recordPackage parses and type-checks sources as one package, each file in generatedNames a generated one, and
// builds its graph with a recorder on. Every call parses afresh, into a file set of its own.
func recordPackage(test *testing.T, sources []recordedSource, generatedNames []string) recordedPackage {
	test.Helper()
	checked := checkShapes(test, "recorded", sources, generatedNames)
	recorded := recordedPackage{
		files:      checked.files,
		tokenFiles: map[string]*token.File{},
		sources:    map[string]string{},
	}
	for index, each := range sources {
		recorded.tokenFiles[each.name] = checked.fileSet.File(checked.files[index].FileStart)
		recorded.sources[each.name] = each.source
	}
	built, buildError := checked.build(true, nil)
	if buildError != nil {
		test.Fatalf("building the graph: %v", buildError)
	}
	recorded.graph = built
	return recorded
}

// bodies is each function declaration with a body in the package's files, by its name, and the place a record of it
// is keyed by, read here from the syntax as rule.StandingSpans reads a standing body's: the file its `func` is in,
// past any `//line` directive, and the offset of that `func` in the file as parsed.
func (recorded recordedPackage) bodies(test *testing.T) map[string]recordedPlace {
	test.Helper()
	places := map[string]recordedPlace{}
	for _, file := range recorded.files {
		tokenFile := recorded.graph.fileSet.File(file.FileStart)
		for _, declaration := range file.Decls {
			function, isFunction := declaration.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			if _, isRepeated := places[function.Name.Name]; isRepeated {
				test.Fatalf("two functions of the package are named %s, so a name tells no body", function.Name.Name)
			}
			places[function.Name.Name] = recordedPlace{
				File:   tokenFile.Position(function.Pos()).Filename,
				Offset: tokenFile.Offset(function.Pos()),
			}
		}
	}
	return places
}

// recordAt is the one record keyed by place.
func (recorded recordedPackage) recordAt(test *testing.T, place recordedPlace) bodyRecord {
	test.Helper()
	var found []bodyRecord
	for _, record := range recorded.graph.bodyRecords() {
		if record.Body == place {
			found = append(found, record)
		}
	}
	if len(found) != 1 {
		test.Fatalf("%d records are keyed by %+v, not one", len(found), place)
	}
	return found[0]
}

// described is record as a test states one, a line for each local, each use, and one each for the rooted locals, the
// files and the flags. It says everything the record holds: a place reads as its file, line and column, which are its
// offset in the file the test parsed, and an object declared outside the body reads with the identifier written
// there, so an expectation can be checked against the source by eye.
func (recorded recordedPackage) described(record bodyRecord) string {
	var text strings.Builder
	for index, local := range record.Locals {
		owners := []string{}
		for _, owner := range local.Owners {
			owners = append(owners, recorded.describedEnd(owner))
		}
		owned := "owned by nothing"
		if len(owners) > 0 {
			owned = "owned by " + strings.Join(owners, " and ")
		}
		fmt.Fprintf(&text, "local %d: %s %q at %s, %s\n", index, local.Kind, local.Name,
			recorded.describedPlace(local.Place), owned)
	}
	for _, use := range record.Uses {
		fmt.Fprintf(&text, "use: %s -> %s\n", recorded.describedEnd(use.By), recorded.describedEnd(use.Used))
	}
	rooted := []string{}
	for _, local := range record.Rooted {
		rooted = append(rooted, fmt.Sprintf("local %d", local))
	}
	fmt.Fprintf(&text, "rooted: %s\n", joinedOrNone(rooted))
	fmt.Fprintf(&text, "files: %s\n", joinedOrNone(record.Files))
	flags := []string{}
	if record.Present.UnexportedInterfaceMethod {
		flags = append(flags, "unexportedInterfaceMethod")
	}
	if record.Present.LocalMethodSet {
		flags = append(flags, "localMethodSet")
	}
	if record.Present.InterfaceInstance {
		flags = append(flags, "interfaceInstance")
	}
	if record.Present.IgnoredType {
		flags = append(flags, "ignoredType")
	}
	if record.Present.AllFieldsUsed {
		flags = append(flags, "allFieldsUsed")
	}
	if record.Present.UnplacedEnd {
		flags = append(flags, "unplacedEnd")
	}
	fmt.Fprintf(&text, "present: %s\n", joinedOrNone(flags))
	return text.String()
}

// joinedOrNone is words joined by spaces, or `none` for no word.
func joinedOrNone(words []string) string {
	if len(words) == 0 {
		return "none"
	}
	return strings.Join(words, " ")
}

// describedEnd is end as described states it.
func (recorded recordedPackage) describedEnd(end recordedEnd) string {
	switch end.Kind {
	case endFunction, endRoot:
		return string(end.Kind)
	case endLocal:
		return fmt.Sprintf("local %d", end.Local)
	case endDeclared:
		source := recorded.sources[end.Place.File]
		length := 0
		for end.Place.Offset+length < len(source) && isIdentifierByte(source[end.Place.Offset+length]) {
			length++
		}
		return fmt.Sprintf("%s at %s", source[end.Place.Offset:end.Place.Offset+length],
			recorded.describedPlace(end.Place))
	default:
		return fmt.Sprintf("an end of kind %q", end.Kind)
	}
}

// isIdentifierByte is whether character can be part of an identifier the fixtures write, which are all ASCII.
func isIdentifierByte(character byte) bool {
	return character == '_' || character >= '0' && character <= '9' || character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z'
}

// describedPlace is place as its file, line and column in the file the test parsed under that name.
func (recorded recordedPackage) describedPlace(place recordedPlace) string {
	tokenFile, isParsed := recorded.tokenFiles[place.File]
	if !isParsed || place.Offset > tokenFile.Size() {
		return fmt.Sprintf("%s at offset %d, which no parsed file holds", place.File, place.Offset)
	}
	position := tokenFile.PositionFor(tokenFile.Pos(place.Offset), false)
	return fmt.Sprintf("%s:%d:%d", place.File, position.Line, position.Column)
}

// recordCase is one package of shapes: its files, which of them are generated, and for every function declaration
// with a body, by its name, the record its body must give, as described states one.
type recordCase struct {
	name      string
	sources   []recordedSource
	generated []string
	bodies    map[string]string
}

// noRecord is the record of a body that adds nothing to the graph.
const noRecord = `rooted: none
files: none
present: none
`

// recordCases is a body for each shape of edge, node and flag a record holds, each with the whole record it must
// give. Each expectation was read against the walk, line by line, and not copied from a run.
var recordCases = []recordCase{
	{
		// The local made by `:=` is in no declaration statement, so the walk sees it, and takes it as the function's,
		// only in the scope of the function's type, which it reads before the body. The selection passes through
		// outer's embedded field, which the selector never writes.
		name: "a plain call and a selection through an embedded field",
		sources: []recordedSource{{"shape.go", `package shape

type inner struct{ value int }

type outer struct{ inner }

func build() outer { return outer{} }

func Read() int {
	made := build()
	return made.value
}
`}},
		bodies: map[string]string{
			"build": `use: function -> outer at shape.go:5:6
rooted: none
files: shape.go
present: none
`,
			"Read": `local 0: var "made" at shape.go:10:2, owned by function
use: function -> local 0
use: function -> build at shape.go:7:6
use: function -> value at shape.go:3:20
use: function -> inner at shape.go:5:20
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// A write to a package-level variable is a use only when the variable is declared in a _test.go file, so the
		// file's name is an input of the record with or without the edge.
		name: "a write and a read of a package-level variable, in a plain file and in a test file",
		sources: []recordedSource{
			{"sink.go", `package shape

var plain int

func WritePlain() { plain = 1 }

func ReadPlain() int { return plain }

func WriteSink() { sink = 1 }

func ReadSink() int { return sink }
`},
			{"sink_test.go", `package shape

var sink int

func WriteBoth() {
	plain = 2
	sink++
}
`},
		},
		bodies: map[string]string{
			"WritePlain": `rooted: none
files: sink.go
present: none
`,
			"ReadPlain": `use: function -> plain at sink.go:3:5
rooted: none
files: sink.go
present: none
`,
			"WriteSink": `use: function -> sink at sink_test.go:3:5
rooted: none
files: sink_test.go
present: none
`,
			"ReadSink": `use: function -> sink at sink_test.go:3:5
rooted: none
files: sink_test.go
present: none
`,
			"WriteBoth": `use: function -> sink at sink_test.go:3:5
rooted: none
files: sink.go sink_test.go
present: none
`,
		},
	},
	{
		// No identifier names a field of an unkeyed literal, so the walk uses every field of the literal's type, which
		// an inner literal of a slice does not even write.
		name: "an unkeyed struct literal",
		sources: []recordedSource{{"shape.go", `package shape

type pair struct{ first, second int }

func Unkeyed() pair { return pair{1, 2} }

func Elided() []pair { return []pair{{1, 2}} }
`}},
		bodies: map[string]string{
			"Unkeyed": `use: function -> pair at shape.go:3:6
use: function -> first at shape.go:3:19
use: function -> second at shape.go:3:26
rooted: none
files: shape.go
present: none
`,
			"Elided": `use: function -> pair at shape.go:3:6
use: function -> first at shape.go:3:19
use: function -> second at shape.go:3:26
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// The fields of the two structs use each other, edges whose ends are both outside the body and neither the
		// function. The graph keeps them from the first body alone, and each body's record holds them. So it does the
		// function's use of its own parameter, which the signature added before the body read it. The source's
		// fields may be those of an anonymous struct a signature writes, as declared outside the body as any.
		name: "a struct to struct conversion, the same one in two bodies",
		sources: []recordedSource{{"shape.go", `package shape

type source struct{ a, b int }

type target struct{ a, b int }

func First(value source) target { return target(value) }

func Second(value source) target { return target(value) }

func Signed(value struct{ a, b int }) target { return target(value) }
`}},
		bodies: map[string]string{
			"First": `use: function -> target at shape.go:5:6
use: function -> value at shape.go:7:12
use: a at shape.go:3:21 -> a at shape.go:5:21
use: a at shape.go:5:21 -> a at shape.go:3:21
use: b at shape.go:3:24 -> b at shape.go:5:24
use: b at shape.go:5:24 -> b at shape.go:3:24
rooted: none
files: shape.go
present: none
`,
			"Second": `use: function -> target at shape.go:5:6
use: function -> value at shape.go:9:13
use: a at shape.go:3:21 -> a at shape.go:5:21
use: a at shape.go:5:21 -> a at shape.go:3:21
use: b at shape.go:3:24 -> b at shape.go:5:24
use: b at shape.go:5:24 -> b at shape.go:3:24
rooted: none
files: shape.go
present: none
`,
			"Signed": `use: function -> target at shape.go:5:6
use: function -> value at shape.go:11:13
use: a at shape.go:11:27 -> a at shape.go:5:21
use: a at shape.go:5:21 -> a at shape.go:11:27
use: b at shape.go:11:30 -> b at shape.go:5:24
use: b at shape.go:5:24 -> b at shape.go:11:30
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// Every field of the struct is used, and every field of a struct or an array of structs it holds by value,
		// but none behind a pointer. A walk that uses fields so keeps its body present.
		name: "an unsafe.Pointer conversion over nested struct and array fields",
		sources: []recordedSource{{"shape.go", `package shape

import "unsafe"

type leaf struct{ low, high byte }

type behind struct{ hidden byte }

type header struct {
	first leaf
	rest  [2]leaf
	far   *behind
}

func Cast(pointer unsafe.Pointer) *header { return (*header)(pointer) }
`}},
		bodies: map[string]string{
			"Cast": `use: function -> header at shape.go:9:6
use: function -> pointer at shape.go:15:11
use: function -> first at shape.go:10:2
use: function -> low at shape.go:5:19
use: function -> high at shape.go:5:24
use: function -> rest at shape.go:11:2
use: function -> far at shape.go:12:2
rooted: none
files: shape.go
present: allFieldsUsed
`,
		},
	},
	{
		// The scope of the function's type lists the immediate locals by name, so that is the order they are numbered
		// in. What a local's declaration reads is used by the local and not by the function.
		name: "a local const, type, alias and a local struct's fields",
		sources: []recordedSource{{"shape.go", `package shape

func Locals() int {
	const limit = 3
	type count int
	type alias = count
	type point struct{ x, y int }
	var origin point
	var total alias = limit
	return int(total) + origin.x
}
`}},
		bodies: map[string]string{
			"Locals": `local 0: type "alias" at shape.go:6:7, owned by function
local 1: type "count" at shape.go:5:7, owned by function
local 2: const "limit" at shape.go:4:8, owned by function
local 3: var "origin" at shape.go:8:6, owned by function
local 4: type "point" at shape.go:7:7, owned by function
local 5: var "total" at shape.go:9:6, owned by function
local 6: field "x" at shape.go:7:21, owned by local 4
local 7: field "y" at shape.go:7:24, owned by local 4
use: function -> local 3
use: function -> local 5
use: local 0 -> local 1
use: local 3 -> local 4
use: local 5 -> local 0
use: local 5 -> local 2
use: function -> local 6
rooted: none
files: none
present: none
`,
		},
	},
	{
		// go/types checks the type once a variable, so it makes the field twice, where the one identifier is written,
		// and keeps the second for that identifier. The walk reads the type once a name and resolves the identifier
		// each time, so the second field is seen twice and owned by each variable, and the first is a node only
		// because a selection uses it: a local with no owner, at the place of another.
		name: "two variables of one anonymous struct type",
		sources: []recordedSource{{"shape.go", `package shape

func Pair() int {
	var a, b struct{ x int }
	return a.x + b.x
}
`}},
		bodies: map[string]string{
			"Pair": `local 0: var "a" at shape.go:4:6, owned by function
local 1: var "b" at shape.go:4:9, owned by function
local 2: field "x" at shape.go:4:19, owned by local 0 and local 1
local 3: field "x" at shape.go:4:19, owned by nothing
use: function -> local 0
use: function -> local 1
use: local 0 -> local 2
use: local 1 -> local 2
use: function -> local 3
use: function -> local 2
rooted: none
files: none
present: none
`,
		},
	},
	{
		// go/types makes one variable a clause, all three where the one identifier is written, so a local can only be
		// its index.
		name: "a type switch with a bound variable",
		sources: []recordedSource{{"shape.go", `package shape

func Describe(value any) int {
	switch bound := value.(type) {
	case int:
		return bound
	case string:
		return len(bound)
	default:
		_ = bound
		return 0
	}
}
`}},
		bodies: map[string]string{
			"Describe": `local 0: var "bound" at shape.go:4:9, owned by function
local 1: var "bound" at shape.go:4:9, owned by function
local 2: var "bound" at shape.go:4:9, owned by function
use: function -> value at shape.go:3:15
use: function -> local 0
use: function -> local 1
use: function -> local 2
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// An unnamed parameter has no declaring identifier, and go/types places it at its type. A blank result is in no
		// scope, so the walk never sees it and only uses it: a local with no owner.
		name: "a function literal with an unnamed parameter, and one with a blank result",
		sources: []recordedSource{{"shape.go", `package shape

func Unnamed() func(int) int {
	return func(int) int { return 1 }
}

func BlankResult() func() int {
	return func() (_ int) { return 1 }
}
`}},
		bodies: map[string]string{
			"Unnamed": `local 0: var "" at shape.go:4:14, owned by function
use: function -> local 0
rooted: none
files: none
present: none
`,
			"BlankResult": `local 0: var "_" at shape.go:8:17, owned by nothing
use: function -> local 0
rooted: none
files: none
present: none
`,
		},
	},
	{
		// An interface literal keeps its body present only for an unexported method, its own or one it embeds, which
		// only go/types' method set shows.
		name: "an interface literal with exported methods, with an unexported one, and embedding one",
		sources: []recordedSource{{"shape.go", `package shape

type sealed interface{ seal() }

func Exported(value any) bool {
	_, held := value.(interface{ Close() error })
	return held
}

func Unexported(value any) bool {
	_, held := value.(interface{ close() error })
	return held
}

func Embedding(value any) bool {
	_, held := value.(interface {
		sealed
		Close() error
	})
	return held
}
`}},
		bodies: map[string]string{
			"Exported": `local 0: var "held" at shape.go:6:5, owned by function
local 1: func "Close" at shape.go:6:31, owned by function
use: function -> local 0
use: function -> value at shape.go:5:15
use: function -> local 1
rooted: none
files: shape.go
present: none
`,
			"Unexported": `local 0: var "held" at shape.go:11:5, owned by function
local 1: func "close" at shape.go:11:31, owned by function
use: function -> local 0
use: function -> value at shape.go:10:17
use: function -> local 1
rooted: none
files: shape.go
present: unexportedInterfaceMethod
`,
			"Embedding": `local 0: var "held" at shape.go:16:5, owned by function
local 1: func "Close" at shape.go:18:3, owned by function
use: function -> local 0
use: function -> value at shape.go:15:16
use: function -> sealed at shape.go:3:6
use: function -> local 1
rooted: none
files: shape.go
present: unexportedInterfaceMethod
`,
		},
	},
	{
		// A local named type keeps its body present when it has a method, which no syntax of its own declaration
		// shows for `type L T` or `type L I`, and an embedded field that brings no method does not.
		name: "a local type of a type with methods, of an interface, and one with an empty method set",
		sources: []recordedSource{{"shape.go", `package shape

type base struct{}

func (base) Method() {}

type wrapper struct{ base }

type shaped interface{ Area() int }

type inner struct{ count int }

func Derived() {
	type local wrapper
	var value local
	value.Method()
}

func Interfaced(value shaped) int {
	type local shaped
	return local(value).Area()
}

func Empty() int {
	type local struct{ inner }
	return local{}.count
}
`}},
		bodies: map[string]string{
			"Method": noRecord,
			"Derived": `local 0: type "local" at shape.go:14:7, owned by function
local 1: var "value" at shape.go:15:6, owned by function
use: function -> local 1
use: local 0 -> wrapper at shape.go:7:6
use: local 1 -> local 0
use: function -> Method at shape.go:5:13
use: function -> base at shape.go:7:22
rooted: none
files: shape.go
present: localMethodSet
`,
			"Interfaced": `local 0: type "local" at shape.go:20:7, owned by function
use: local 0 -> shaped at shape.go:9:6
use: function -> local 0
use: function -> value at shape.go:19:17
use: function -> Area at shape.go:9:24
rooted: none
files: shape.go
present: localMethodSet
`,
			"Empty": `local 0: type "local" at shape.go:25:7, owned by function
local 1: field "inner" at shape.go:25:21, owned by local 0
use: local 1 -> inner at shape.go:11:6
use: function -> local 0
use: function -> count at shape.go:11:20
use: function -> local 1
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// Only an instantiation of a generic interface is among the interfaces entry reads, and only the body that
		// writes it is kept present. A use of an instantiated field is a use of the generic one.
		name: "an instantiation of a generic interface of the package, and one of a generic struct",
		sources: []recordedSource{{"shape.go", `package shape

type holder[T any] interface{ Hold() T }

type box[T any] struct{ item T }

func Instantiate(value any) bool {
	_, held := value.(holder[int])
	return held
}

func Boxed() int {
	var kept box[int]
	return kept.item
}
`}},
		bodies: map[string]string{
			"Instantiate": `local 0: var "held" at shape.go:8:5, owned by function
use: function -> local 0
use: function -> value at shape.go:7:18
use: function -> holder at shape.go:3:6
rooted: none
files: shape.go
present: interfaceInstance
`,
			"Boxed": `local 0: var "kept" at shape.go:13:6, owned by function
use: function -> local 0
use: local 0 -> box at shape.go:5:6
use: function -> item at shape.go:5:25
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// The root uses what a directive ignores. For a type it also uses the type's methods and fields, which for an
		// alias of a package-level type are declared outside the body, so the record cannot hold them and says so.
		name: "an ignore directive on a local const, and on a local alias of a package-level type",
		sources: []recordedSource{{"shape.go", `package shape

type target struct{ field int }

func (target) method() {}

func IgnoredConst() {
	//lint:ignore U1000 kept for the test
	const kept = 1
}

func IgnoredAlias() {
	//lint:ignore U1000 kept for the test
	type kept = target
}

func Unignored() {
	const kept = 1
}
`}},
		bodies: map[string]string{
			"method": noRecord,
			"IgnoredConst": `local 0: const "kept" at shape.go:9:8, owned by function
rooted: local 0
files: none
present: none
`,
			"IgnoredAlias": `local 0: type "kept" at shape.go:14:7, owned by function
use: local 0 -> target at shape.go:3:6
rooted: local 0
files: shape.go
present: ignoredType
`,
			"Unignored": `local 0: const "kept" at shape.go:18:8, owned by function
rooted: none
files: none
present: none
`,
		},
	},
	{
		// The root uses every node declared in a generated file, a body's locals among them.
		name:      "a body in a generated file",
		generated: []string{"generated.go"},
		sources: []recordedSource{
			{"generated.go", `package shape

func Generated() {
	const made = 1
}
`},
			{"written.go", `package shape

func Written() {
	const made = 1
}
`},
		},
		bodies: map[string]string{
			"Generated": `local 0: const "made" at generated.go:4:8, owned by function
rooted: local 0
files: none
present: none
`,
			"Written": `local 0: const "made" at written.go:4:8, owned by function
rooted: none
files: none
present: none
`,
		},
	},
	{
		// The pointer's type is named, so nothing in the body names the struct whose fields the conversion uses, nor
		// the struct that one holds. The body is kept present for it, and one that only returns the pointer is not.
		name: "a conversion to unsafe.Pointer through a named pointer type",
		sources: []recordedSource{{"shape.go", `package shape

import "unsafe"

type inner struct{ x int }

type outer struct{ held inner }

type named *outer

func address(pointer named) unsafe.Pointer { return unsafe.Pointer(pointer) }

func plain(pointer named) *outer { return pointer }
`}},
		bodies: map[string]string{
			"address": `use: function -> pointer at shape.go:11:14
use: function -> held at shape.go:7:20
use: function -> x at shape.go:5:20
rooted: none
files: shape.go
present: allFieldsUsed
`,
			"plain": `use: function -> pointer at shape.go:13:12
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// A directive on the whole file ignores the local alias as one on its line would, and roots the methods and
		// fields of the package-level type it names all the same.
		name: "a file ignore directive over a local alias of a package-level type",
		sources: []recordedSource{{"shape.go", `//lint:file-ignore U1000 kept for the test

package shape

type target struct{ field int }

func (target) method() {}

func Aliasing() {
	type kept = target
	var _ kept
}
`}},
		bodies: map[string]string{
			"method": noRecord,
			"Aliasing": `local 0: type "kept" at shape.go:10:7, owned by function
local 1: var "_" at shape.go:11:6, owned by function
use: local 0 -> target at shape.go:5:6
use: local 1 -> local 0
use: function -> local 1
rooted: local 0 local 1
files: shape.go
present: ignoredType
`,
		},
	},
	{
		// go/types makes the field once a variable and keeps the second for the one identifier, as it does for two
		// locals, but here both fields are declared outside the bodies, where an end is its place alone. The first
		// variable's field is not what its place finds, so the body that selects it is kept present, and the body that
		// selects the second's is not.
		name: "two package-level variables of one anonymous struct type",
		sources: []recordedSource{{"shape.go", `package shape

var first, second struct{ value int }

func ReadFirst() int { return first.value }

func ReadSecond() int { return second.value }
`}},
		bodies: map[string]string{
			"ReadFirst": `use: function -> first at shape.go:3:5
use: function -> value at shape.go:3:27
rooted: none
files: shape.go
present: unplacedEnd
`,
			"ReadSecond": `use: function -> second at shape.go:3:12
use: function -> value at shape.go:3:27
rooted: none
files: shape.go
present: none
`,
		},
	},
	{
		// A selection through an instance resolves to an instantiated field, which is the generic field's node, so
		// the record has one local for both.
		name: "a local generic type's field, used through an instance",
		sources: []recordedSource{{"shape.go", `package shape

func Generic() int {
	type box[T any] struct{ item T }
	var kept box[int]
	return kept.item
}
`}},
		bodies: map[string]string{
			"Generic": `local 0: type "box" at shape.go:4:7, owned by function
local 1: var "kept" at shape.go:5:6, owned by function
local 2: type param "T" at shape.go:4:11, owned by local 0
local 3: field "item" at shape.go:4:26, owned by local 0
use: function -> local 1
use: local 0 -> local 2
use: local 3 -> local 2
use: local 1 -> local 0
use: function -> local 3
rooted: none
files: none
present: none
`,
		},
	},
}

// TestRecorderRecordsEachShapeOfBody holds the recorder to the whole record of a body of each shape (#xeafx79): its
// locals in the walk's order, each with its place, kind, name and owners, its use edges, the locals the root uses
// after the walk, the files it names and the flags that keep it present. Every function declaration with a body in a
// case has an expectation, and the package has one record a body, keyed where a standing body is keyed.
func TestRecorderRecordsEachShapeOfBody(test *testing.T) {
	test.Parallel()
	for _, each := range recordCases {
		test.Run(each.name, func(test *testing.T) {
			test.Parallel()
			recorded := recordPackage(test, each.sources, each.generated)
			places := recorded.bodies(test)
			if records := recorded.graph.bodyRecords(); len(records) != len(places) {
				test.Errorf("the package has %d records for its %d bodies", len(records), len(places))
			}
			if recorded.graph.recorder.isOpen {
				test.Error("the cursor is still open once the walk is over")
			}
			for name, place := range places {
				expected, isExpected := each.bodies[name]
				if !isExpected {
					test.Errorf("the case expects nothing of %s, so its record goes unread", name)
					continue
				}
				if found := recorded.described(recorded.recordAt(test, place)); found != expected {
					test.Errorf("the record of %s is\n%s\nnot\n%s", name, found, expected)
				}
			}
			for name := range each.bodies {
				if _, isDeclared := places[name]; !isDeclared {
					test.Errorf("the case expects a record of %s, which the package does not declare", name)
				}
			}
		})
	}
}

// TestRecorderGivesOneBodyTheSameBytesTwice holds a record to one encoding (#xeafx79): each case is parsed, checked
// and walked twice, each time into a file set, a type-checker and a graph of its own, and each body's two records
// must encode to the same bytes. A build compares its record of a body with the host's end for end, so a record that
// took its order from a map, a pointer or a node's index would differ between two runs that agree.
func TestRecorderGivesOneBodyTheSameBytesTwice(test *testing.T) {
	test.Parallel()
	compared := 0
	for _, each := range recordCases {
		first := recordPackage(test, each.sources, each.generated)
		second := recordPackage(test, each.sources, each.generated)
		for name, place := range first.bodies(test) {
			firstBytes, firstError := first.recordAt(test, place).encoded()
			secondBytes, secondError := second.recordAt(test, place).encoded()
			if firstError != nil || secondError != nil {
				test.Fatalf("encoding the records of %s in %q: %v, %v", name, each.name, firstError, secondError)
			}
			if !bytes.Equal(firstBytes, secondBytes) {
				test.Errorf("the body of %s in %q recorded twice gives\n%s\nand\n%s", name, each.name, firstBytes,
					secondBytes)
			}
			compared++
		}
	}
	if compared == 0 {
		test.Fatal("no body was recorded twice, so the comparison proves nothing")
	}
}

// lineDirectiveSource is a file a `//line` directive renames from its first function on.
const lineDirectiveSource = `package shape

//line named.go:40
func After() {
	const kept = 1
	below()
}

func below() {}
`

// TestRecorderKeysByTheLineDirectivesFileAndPlacesByTheParsedOne holds a record's two kinds of place apart
// (#xeafx79). Its key is a standing body's (rule.StandingBodies.Stands): the file a `//line` directive names, and the
// offset in the file as parsed, which the directive does not move. Every other place in it, a local's, an end's and
// each of its files, names the file as it was parsed, though the directive renames those positions too, since a
// build finds an object in its own parsed files.
func TestRecorderKeysByTheLineDirectivesFileAndPlacesByTheParsedOne(test *testing.T) {
	test.Parallel()
	recorded := recordPackage(test, []recordedSource{{"parsed.go", lineDirectiveSource}}, nil)
	record := recorded.recordAt(test, recordedPlace{
		File:   "named.go",
		Offset: strings.Index(lineDirectiveSource, "func After"),
	})
	expectedLocals := []recordedLocal{{
		Place:  recordedPlace{File: "parsed.go", Offset: strings.Index(lineDirectiveSource, "kept")},
		Kind:   "const",
		Name:   "kept",
		Owners: []recordedEnd{{Kind: endFunction}},
	}}
	if !reflect.DeepEqual(record.Locals, expectedLocals) {
		test.Errorf("the body's locals are %+v, not %+v", record.Locals, expectedLocals)
	}
	expectedUses := []recordedUse{{
		By: recordedEnd{Kind: endFunction},
		Used: recordedEnd{Kind: endDeclared, Place: recordedPlace{
			File:   "parsed.go",
			Offset: strings.Index(lineDirectiveSource, "func below") + len("func "),
		}},
	}}
	if !reflect.DeepEqual(record.Uses, expectedUses) {
		test.Errorf("the body's uses are %+v, not %+v", record.Uses, expectedUses)
	}
	if !reflect.DeepEqual(record.Files, []string{"parsed.go"}) {
		test.Errorf("the body's files are %v, not the one parsed file", record.Files)
	}
}

// sharedKeySources are two files whose `//line` directives name one file, each with a function whose `func` is at
// the same offset of its own file, so the two bodies have one key. The second file's later function is under the
// directive's name too, at an offset of its own.
var sharedKeySources = []recordedSource{
	{"one.go", `package shape

//line shared.go:10
func First() {}
`},
	{"two.go", `package shape

//line shared.go:20
func Second() {}

func Third() {}
`},
}

// TestRecorderKeepsNoRecordOfBodiesThatShareAKey holds the records to one body a key (#xeafx79): of two bodies whose
// keys are the same, the package has a record of neither, not of the first and not of the last, since a build asking
// by that key could be handed either. A body keyed alone under the same directive's name keeps its record.
func TestRecorderKeepsNoRecordOfBodiesThatShareAKey(test *testing.T) {
	test.Parallel()
	recorded := recordPackage(test, sharedKeySources, nil)
	places := recorded.bodies(test)
	if places["First"] != places["Second"] {
		test.Fatalf("First is keyed %+v and Second %+v, two keys, so dropping neither proves nothing", places["First"],
			places["Second"])
	}
	if walked := len(recorded.graph.recorder.records); walked != 3 {
		test.Fatalf("the walk recorded %d bodies, not the package's three", walked)
	}
	kept := recorded.graph.bodyRecords()
	if len(kept) != 1 || kept[0].Body != places["Third"] {
		test.Errorf("the package keeps %+v, not the one record of Third at %+v", kept, places["Third"])
	}
}

// TestRecorderFlagsALocalOfAFileWhoseParsedNameIsShared holds a build to placing a spliced local in one file
// (#xeafx79): a local is placed by the name its file was parsed under, and where two files of the package were parsed
// under one name a build could place it in either, so the body is kept present. A body whose locals are all in a file
// of its own name is not flagged.
func TestRecorderFlagsALocalOfAFileWhoseParsedNameIsShared(test *testing.T) {
	test.Parallel()
	recorded := recordPackage(test, []recordedSource{
		{"same.go", `package shape

func First() int {
	const limit = 1
	return limit
}
`},
		{"same.go", `package shape

// Second is placed further into its file than First, so the two keys differ.
func Second() int {
	const limit = 2
	return limit
}
`},
		{"alone.go", `package shape

func Third() int {
	const limit = 3
	return limit
}
`},
	}, nil)
	places := recorded.bodies(test)
	flagged := map[recordedPlace]bool{}
	for _, record := range recorded.graph.recorder.records {
		if len(record.Locals) == 0 {
			test.Fatalf("the body at %+v recorded no local, so its flag proves nothing", record.Body)
		}
		flagged[record.Body] = record.Present.UnplacedEnd
	}
	for name, want := range map[string]bool{"First": true, "Second": true, "Third": false} {
		if got, isRecorded := flagged[places[name]]; !isRecorded || got != want {
			test.Errorf("%s at %+v: recorded %t, unplacedEnd %t, want it %t", name, places[name], isRecorded, got, want)
		}
	}
}

// recordFlags is whether each body checked's recording graph walked is flagged UnplacedEnd, by its function's name and
// in the order the walk read them, each record found by its position among the records, which are in that order.
func recordFlags(test *testing.T, checked checkedPackage, built *graph) map[string]bool {
	test.Helper()
	names := []string{}
	for _, file := range checked.files {
		for _, declaration := range file.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil {
				names = append(names, function.Name.Name)
			}
		}
	}
	if len(built.recorder.records) != len(names) {
		test.Fatalf("the walk recorded %d bodies of the package's %d", len(built.recorder.records), len(names))
	}
	flagged := map[string]bool{}
	for index, name := range names {
		flagged[name] = built.recorder.records[index].Present.UnplacedEnd
	}
	return flagged
}

// TestRecorderFlagsABodyWhoseWalkSeesWhatItDoesNotDeclare holds the recorder to flagging and not stopping
// (#xeafx79): a body's walk that sees an object outside the body's braces, past its signature, makes an own edge no
// record has a place for. No syntax go/types checks does that, so Shrunk's closing brace is moved to just past its
// opening one, which leaves the variable of its `if` outside. The body is kept present and the host's run goes on,
// and Kept, whose braces are its own, is not flagged.
func TestRecorderFlagsABodyWhoseWalkSeesWhatItDoesNotDeclare(test *testing.T) {
	test.Parallel()
	checked := checkShapes(test, "shrunk", []recordedSource{{"shape.go", `package shape

func Shrunk() int {
	if limit := 1; limit > 0 {
		return limit
	}
	return 0
}

func Kept() int {
	if limit := 2; limit > 0 {
		return limit
	}
	return 0
}
`}}, nil)
	for _, declaration := range checked.files[0].Decls {
		if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Name.Name == "Shrunk" {
			function.Body.Rbrace = function.Body.Lbrace + 1
		}
	}
	built, buildError := checked.build(true, nil)
	if buildError != nil {
		test.Fatal(buildError)
	}
	if flagged := recordFlags(test, checked, built); !reflect.DeepEqual(flagged,
		map[string]bool{"Shrunk": true, "Kept": false}) {
		test.Errorf("the bodies are flagged %v, not Shrunk alone", flagged)
	}
}

// unheldSources are a package whose second file a test leaves out of the file set its graph is handed: Read uses a
// variable declared there, Other's body is there with a local, and Kept is wholly in the first file.
var unheldSources = []recordedSource{
	{"one.go", `package shape

func Read() int { return value }

func Kept() int {
	const step = 1
	return step
}
`},
	{"two.go", `package shape

var value int

func Other() {
	const step = 1
}
`},
}

// TestRecorderFlagsWhatItsFileSetDoesNotHold holds the recorder to flagging and not stopping (#xeafx79): a graph
// handed a file set that holds the package's first file alone, at the place it was parsed at, has no file for what is
// declared in the second. Read's end there and Other's key and local have no place, so both bodies are kept present
// and the host's run goes on; Kept is recorded as ever, and a graph over the same file set takes its record.
func TestRecorderFlagsWhatItsFileSetDoesNotHold(test *testing.T) {
	test.Parallel()
	checked := checkShapes(test, "unheld", unheldSources, nil)
	partial := checked
	partial.fileSet = token.NewFileSet()
	if _, parseError := parser.ParseFile(partial.fileSet, unheldSources[0].name, unheldSources[0].source,
		parser.ParseComments); parseError != nil {
		test.Fatalf("parsing %s: %v", unheldSources[0].name, parseError)
	}
	if partial.fileSet.File(checked.files[0].FileStart) == nil ||
		partial.fileSet.File(checked.files[1].FileStart) != nil {
		test.Fatal("the file set does not hold the first file alone, at its place, so the case proves nothing")
	}
	recording, recordError := partial.build(true, nil)
	if recordError != nil {
		test.Fatal(recordError)
	}
	flagged := recordFlags(test, partial, recording)
	if !reflect.DeepEqual(flagged, map[string]bool{"Read": true, "Kept": false, "Other": true}) {
		test.Errorf("the bodies are flagged %v, not Read and Other", flagged)
	}
	if other := recording.recorder.records[2]; other.Body != (recordedPlace{}) || len(other.Locals) != 1 {
		test.Errorf("Other's record is keyed %+v with %d locals, not by no place with its one", other.Body,
			len(other.Locals))
	}
	if taken, takeError := partial.build(false, []bodyRecord{recording.recorder.records[1]}); takeError != nil {
		test.Errorf("a graph over the same file set did not take Kept's record: %v", takeError)
	} else if len(taken.splice.locals) != 1 {
		test.Errorf("taking Kept's record spliced %d locals, not its one", len(taken.splice.locals))
	}
}

// TestRecorderChangesNothingTheRuleFinds holds a graph that records to the graph that does not, over every package of
// the copy of the original's testdata, test variants included (#xeafx79): the same nodes and edges, and the same
// verdict on every object, which is also the verdict the analyzer itself returned. Every function declaration with a
// body there has its one record, and recorded twice it gives the same bytes. The fixtures hold many shapes no case
// above writes, so this is also where the recorder meets them. A body whose walk sees what it does not declare is
// kept present (UnplacedEnd) rather than stopping the run, so a shape that did so would show here only among the
// present, which the self-splice counts by flag.
func TestRecorderChangesNothingTheRuleFinds(test *testing.T) {
	test.Parallel()
	actions, runError := fixtureRun()
	if runError != nil {
		test.Fatal(runError)
	}
	bodies, locals, uses, rooted, present := 0, 0, 0, 0, 0
	for _, action := range actions.port {
		loaded := action.Package
		answer, generatedError := generated.Analyzer.Run(&analysis.Pass{Fset: loaded.Fset, Files: loaded.Syntax})
		if generatedError != nil {
			test.Fatalf("reading the generated files of %s: %v", loaded, generatedError)
		}
		generatedFiles, isGenerated := answer.(map[string]generated.Generator)
		if !isGenerated {
			test.Fatalf("the generated files of %s are %T", loaded, answer)
		}
		build := func(records bool) *graph {
			built := newGraph(loaded.Fset, loaded.Syntax, loaded.Types, loaded.TypesInfo,
				parseDirectives(loaded.Syntax, loaded.Fset), generatedFiles)
			if records {
				built.recordBodies()
			}
			if entryError := built.entry(); entryError != nil {
				test.Fatalf("%s: building the graph: %v", loaded, entryError)
			}
			return built
		}
		plain, recording, again := build(false), build(true), build(true)
		verdict := plain.results()
		if !reflect.DeepEqual(verdict, action.Result) {
			test.Errorf("%s: a graph built here gives %+v, where the analyzer gave %+v", loaded, verdict, action.Result)
		}
		if found := recording.results(); !reflect.DeepEqual(found, verdict) {
			test.Errorf("%s: recording, the verdict is %+v, and without %+v", loaded, found, verdict)
		}
		if len(recording.nodes) != len(plain.nodes) || len(recording.uses) != len(plain.uses) ||
			len(recording.owns) != len(plain.owns) {
			test.Errorf("%s: recording, the graph has %d nodes, %d uses and %d ownerships, and without %d, %d and %d",
				loaded, len(recording.nodes), len(recording.uses), len(recording.owns), len(plain.nodes),
				len(plain.uses), len(plain.owns))
		}

		declared := 0
		for _, file := range loaded.Syntax {
			for _, declaration := range file.Decls {
				if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Body != nil {
					declared++
				}
			}
		}
		records, recordsAgain := recording.bodyRecords(), again.bodyRecords()
		if len(records) != declared {
			test.Errorf("%s: %d records for %d function declarations with a body", loaded, len(records), declared)
		}
		if len(recordsAgain) != len(records) {
			test.Fatalf("%s: recorded twice, %d records and then %d", loaded, len(records), len(recordsAgain))
		}
		for index, record := range records {
			firstBytes, firstError := record.encoded()
			secondBytes, secondError := recordsAgain[index].encoded()
			if firstError != nil || secondError != nil {
				test.Fatalf("%s: encoding the records at %+v: %v, %v", loaded, record.Body, firstError, secondError)
			}
			if !bytes.Equal(firstBytes, secondBytes) {
				test.Errorf("%s: the body at %+v recorded twice gives\n%s\nand\n%s", loaded, record.Body, firstBytes,
					secondBytes)
			}
			bodies++
			locals += len(record.Locals)
			uses += len(record.Uses)
			rooted += len(record.Rooted)
			if record.Present != (recordedPresence{}) {
				present++
			}
		}
	}
	test.Logf("recorded %d bodies over %d packages: %d locals, %d uses, %d rooted locals, %d bodies kept present",
		bodies, len(actions.port), locals, uses, rooted, present)
	// No fixture roots a local after the walk, which the cases above do.
	if bodies == 0 || locals == 0 || uses == 0 || present == 0 {
		test.Fatal("the fixtures recorded no body, no local, no use or kept no body present, so agreeing over them " +
			"proves less than it reads")
	}
}
