package check

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"
	"honnef.co/go/tools/analysis/facts/deprecated"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
)

// The verdict fixture's paths: platform has files of one target each, and every user package imports it.
const (
	verdictPlatform = "example.com/verdict/platform"
	verdictUser     = "example.com/verdict/user"
)

// verdictFiles are platform, with what darwin and linux declare alike and what each declares its own way, and the user
// packages, each reading platform one way. Stable is the same in both builds, unexported fields and all. Handle reaches
// state, whose unexported field differs by target and nothing else does. Shared's Size method differs by target, Name
// is a constant of a value per target, Identity is a generic the stable user instantiates over Stable, OnlyDarwin is
// declared on darwin alone, and Flusher, with its Flush method, on linux alone. freebsd lays state out as darwin does
// and sizes Shared as linux does.
var verdictFiles = map[string]string{
	"platform/platform.go": "package platform\n\n" +
		"type Stable struct {\n\tcount int\n\tLabel string\n}\n\n" +
		"func NewStable() Stable { return Stable{} }\n\n" +
		"type Handle struct{ current *state }\n\n" +
		"type Shared struct{ Value int }\n\n" +
		"type Closer interface{ Close() error }\n\n" +
		"func Identity[Value any](value Value) Value { return value }\n\n" +
		"type Box[Value any] struct{ Held Value }\n",
	"platform/platform_darwin.go": "package platform\n\n" +
		"const Name = \"darwin\"\n\n" +
		"type state struct{ descriptor int }\n\n" +
		"func (Shared) Size() int { return 0 }\n\n" +
		"func OnlyDarwin() {}\n",
	"platform/platform_linux.go": "package platform\n\n" +
		"const Name = \"linux\"\n\n" +
		"type state struct{ descriptor uintptr }\n\n" +
		"func (Shared) Size() int64 { return 0 }\n\n" +
		"type Flusher struct{}\n\n" +
		"func (Flusher) Flush() {}\n",
	"platform/platform_freebsd.go": "package platform\n\n" +
		"const Name = \"freebsd\"\n\n" +
		"type state struct{ descriptor int }\n\n" +
		"func (Shared) Size() int64 { return 0 }\n",
}

// verdictUsers are the user packages' sources, by the way each reads platform. A darwin-only one is checked on the
// host alone.
var verdictUsers = map[string]string{
	"stable": "package user\n\nimport \"example.com/verdict/platform\"\n\n" +
		"type Mine struct{}\n\nfunc (Mine) Close() error { return nil }\n\nfunc (Mine) Flush() {}\n\n" +
		"func Use() platform.Stable { return platform.Identity(platform.NewStable()) }\n",
	"handle": "package user\n\nimport \"example.com/verdict/platform\"\n\n" +
		"func Use(handle platform.Handle) platform.Handle { return handle }\n",
	"method": "package user\n\nimport \"example.com/verdict/platform\"\n\n" +
		"func Use(shared platform.Shared) int { return shared.Value }\n",
	"name": "package user\n\nimport \"example.com/verdict/platform\"\n\n" +
		"func Use() string { return platform.Name }\n",
	"darwin": "package user\n\nimport \"example.com/verdict/platform\"\n\n" +
		"func Use() { platform.OnlyDarwin() }\n",
}

// verdictWorld is one build of the fixture for goos: platform type-checked from the files goos's build context
// matches, and, when user names one of verdictUsers, the user package over it, with its type information and syntax.
type verdictWorld struct {
	platform *types.Package
	user     *types.Package
	info     *types.Info
	files    []*ast.File
}

// checkedWorld type-checks the fixture for goos, with the user package of verdictUsers named user, or none when user
// is empty.
func checkedWorld(test *testing.T, goos string, user string) verdictWorld {
	test.Helper()

	directory := test.TempDir()
	writeFiles(test, directory, verdictFiles)
	context := build.Default
	context.GOOS, context.GOARCH, context.CgoEnabled = goos, "amd64", false
	fileSet := token.NewFileSet()
	platformDirectory := filepath.Join(directory, "platform")
	entries, readError := os.ReadDir(platformDirectory)
	if readError != nil {
		test.Fatal(readError)
	}
	var platformFiles []*ast.File
	for _, entry := range entries {
		matched, matchError := context.MatchFile(platformDirectory, entry.Name())
		if matchError != nil {
			test.Fatal(matchError)
		}
		if !matched {
			continue
		}
		parsed, parseError := parser.ParseFile(fileSet, filepath.Join(platformDirectory, entry.Name()), nil,
			parser.SkipObjectResolution)
		if parseError != nil {
			test.Fatal(parseError)
		}
		platformFiles = append(platformFiles, parsed)
	}
	world := verdictWorld{}
	platform, checkError := (&types.Config{}).Check(verdictPlatform, fileSet, platformFiles, nil)
	if checkError != nil {
		test.Fatalf("type-checking platform for %s: %v", goos, checkError)
	}
	world.platform = platform
	if user == "" {
		return world
	}
	userFile, parseError := parser.ParseFile(fileSet, "user.go", verdictUsers[user], parser.SkipObjectResolution)
	if parseError != nil {
		test.Fatal(parseError)
	}
	world.files = []*ast.File{userFile}
	world.info = &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(path string) (*types.Package, error) {
		if path == verdictPlatform {
			return platform, nil
		}
		return nil, fmt.Errorf("no package %s", path)
	})}
	world.user, checkError = configuration.Check(verdictUser, fileSet, world.files, world.info)
	if checkError != nil {
		test.Fatalf("type-checking the %s user for %s: %v", user, goos, checkError)
	}
	return world
}

// hostReads are the reads of the user package named user on the darwin host, its type reads recorded from its type
// information and syntax, as the run records them, and the host's world.
func hostReads(test *testing.T, user string) (Restoring, verdictWorld) {
	test.Helper()

	host := checkedWorld(test, "darwin", user)
	reads := &build_reads.Reads{}
	reads.RecordTypes(host.user, host.info, host.files)
	return Restoring{Path: verdictUser, Imports: []string{verdictPlatform}, Reads: reads}, host
}

// factKeyOf is a fact lookup's question as one string, the key verdictFacts holds facts by.
func factKeyOf(owner string, path objectpath.Path, factType reflect.Type) string {
	return owner + " " + string(path) + " " + factType.String()
}

// linuxWorld is the fixture's linux build of platform as a verdict reads it: its memo, the packages the user package
// reaches by path, and its facts, facts by factKeyOf.
type linuxWorld struct {
	build   *ReadsBuild
	closure map[string]*types.Package
	fact    FactLookup
}

// linuxBuild is the fixture's linux build of platform, whose facts are facts, by factKeyOf.
func linuxBuild(test *testing.T, facts map[string]analysis.Fact) linuxWorld {
	test.Helper()

	linux := checkedWorld(test, "linux", "")
	return linuxWorld{
		build:   NewReadsBuild(),
		closure: map[string]*types.Package{verdictPlatform: linux.platform},
		fact: func(owner string, path objectpath.Path, factType reflect.Type) (analysis.Fact, bool) {
			fact, found := facts[factKeyOf(owner, path, factType)]
			return fact, found
		},
	}
}

// holdsIn is the whole verdict on restoring's reads in linux, its types' half as the load asks it, then its facts'
// half over every fact type it read, as every action on it asks its own.
func holdsIn(restoring Restoring, host *ReadsBuild, linux linuxWorld) (bool, string) {
	if holds, why := TypesHold(restoring, linux.closure, nil, host, linux.build); !holds {
		return false, why
	}
	return FactsHold(restoring, linux.closure, host, linux.build, linux.fact, ReadFactTypes(restoring))
}

// A package reads the same in another build when every object it read is there and reads alike for the kind of use it
// made of it, and it does not when one differs in what that use can observe: an unexported field two types down of a
// type it takes by value, a method of a type it takes by value, a constant's value, or an object the build does not
// declare. Each fails naming the read. The verdicts share
// one host and one build, as a run's do, so under the race detector they prove the memos safe across goroutines.
func TestATypeReadHoldsOnlyWhenEveryObjectRendersAlikeInTheBuild(test *testing.T) {
	test.Parallel()

	host, linux := NewReadsBuild(), linuxBuild(test, nil)
	for _, step := range []struct {
		user string
		want string
	}{
		{"stable", ""},
		{"handle", "T object " + verdictPlatform + ".Handle renders differently"},
		{"method", "T object " + verdictPlatform + ".Shared renders differently"},
		{"name", "T object " + verdictPlatform + ".Name: its own line differs"},
		{"darwin", "T object " + verdictPlatform + ".OnlyDarwin: the build's " + verdictPlatform +
			" has no OnlyDarwin"},
	} {
		test.Run(step.user, func(test *testing.T) {
			test.Parallel()

			restoring, _ := hostReads(test, step.user)
			holds, why := holdsIn(restoring, host, linux)
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want the %s user's reads to hold %t, %q, got %t, %q", step.user, step.want == "",
					step.want, holds, why)
			}
		})
	}
}

// The closure digest of an object is the same for two checks of the same declarations, and differs for two whose
// only difference is an unexported field of a struct its type reaches.
func TestTheClosureDigestFollowsUnexportedFieldsAndNothingElseMoves(test *testing.T) {
	test.Parallel()

	first := checkedWorld(test, "darwin", "")
	again := checkedWorld(test, "darwin", "")
	linux := checkedWorld(test, "linux", "")
	handleOf := func(world verdictWorld) cache.Key {
		return NewReadsBuild().closureDigest(world.platform.Scope().Lookup("Handle"))
	}
	if handleOf(first) != handleOf(again) {
		test.Errorf("two checks of darwin digest Handle apart:\n%s\n\n%s",
			strings.Join(closureRendering(first.platform.Scope().Lookup("Handle"), nil), "\n"),
			strings.Join(closureRendering(again.platform.Scope().Lookup("Handle"), nil), "\n"))
	}
	if handleOf(first) == handleOf(linux) {
		test.Errorf("darwin and linux digest Handle alike, though state's descriptor differs:\n%s",
			strings.Join(closureRendering(first.platform.Scope().Lookup("Handle"), nil), "\n"))
	}
}

// A fact read holds when the build has the fact with an equal value, or lacks it as the host did, and fails when the
// build has it where the host did not, or the other way around, or with another value.
func TestAFactReadHoldsOnlyWhenTheBuildReadsTheSameFact(test *testing.T) {
	test.Parallel()

	valueFactType := reflect.TypeFor[*valueFact]()
	newStablePath := objectpath.Path("NewStable")
	for _, step := range []struct {
		name      string
		hostFound bool
		buildFact analysis.Fact
		want      string
	}{
		{"a hit in both", true, &valueFact{Count: 1}, ""},
		{"a miss in both", false, nil, ""},
		{"a hit on the host, a miss in the build", true, nil, "F fact *check.valueFact of " + verdictPlatform +
			".NewStable: found true on the host, false in the build"},
		{"a miss on the host, a hit in the build", false, &valueFact{Count: 1}, "F fact *check.valueFact of " +
			verdictPlatform + ".NewStable: found false on the host, true in the build"},
		{"another value", true, &valueFact{Count: 2}, "F fact *check.valueFact of " + verdictPlatform +
			".NewStable: its value differs"},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			restoring, host := hostReads(test, "stable")
			read := build_reads.Fact{Object: host.platform.Scope().Lookup("NewStable"), Package: host.platform,
				FactType: valueFactType, Found: step.hostFound}
			if step.hostFound {
				read.Value = &valueFact{Count: 1}
			}
			restoring.Reads.AddFact(read)
			facts := map[string]analysis.Fact{}
			if step.buildFact != nil {
				facts[factKeyOf(verdictPlatform, newStablePath, valueFactType)] = step.buildFact
			}
			holds, why := holdsIn(restoring, NewReadsBuild(), linuxBuild(test, facts))
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
		})
	}
}

// Each kind of lookup a rule records is asked again of the build: a scope lookup by path and name, the closure's
// interface, a package name, an Interface or Method question by its answer, or by a candidate of the subject's shape
// the build gains (methodLookupHolds), and a deprecation by the build's deprecated fact. Each holds when the build
// answers alike and fails naming itself when it does not.
func TestEachLookupKindIsAskedAgainOfTheBuild(test *testing.T) {
	test.Parallel()

	deprecatedOf := func(message string) map[string]analysis.Fact {
		return map[string]analysis.Fact{
			factKeyOf(verdictPlatform, "Stable", deprecatedFactType): &deprecated.IsDeprecated{Msg: message},
		}
	}
	for _, step := range []struct {
		name   string
		lookup func(test *testing.T, host verdictWorld) build_reads.Lookup
		facts  map[string]analysis.Fact
		want   string
	}{
		{
			name: "a scope lookup both builds answer alike",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform, Name: "Stable",
					Found: true, Answer: host.platform.Scope().Lookup("Stable")}
			},
		},
		{
			name: "a scope lookup absent in the build",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform, Name: "OnlyDarwin",
					Found: true, Answer: host.platform.Scope().Lookup("OnlyDarwin")}
			},
			want: "L lookup Scope " + verdictPlatform + ".OnlyDarwin: found true on the host, false in the build",
		},
		{
			name: "a scope miss the build answers",
			lookup: func(*testing.T, verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryScope, Path: verdictPlatform, Name: "Flusher"}
			},
			want: "L lookup Scope " + verdictPlatform + ".Flusher: found false on the host, true in the build",
		},
		{
			name: "the closure's interface",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				_, declared := build_reads.ClosureInterface(host.user.Imports(), verdictPlatform, "Closer")
				return build_reads.Lookup{Query: build_reads.QueryClosure, Path: verdictPlatform, Name: "Closer",
					Found: declared != nil, Answer: declared}
			},
		},
		{
			name: "a package name of the closure",
			lookup: func(*testing.T, verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryPackageName, Name: "platform", Found: true}
			},
		},
		{
			name: "a package name the closure lacks",
			lookup: func(*testing.T, verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryPackageName, Name: "elsewhere", Found: true}
			},
			want: "L lookup PackageName elsewhere: found true on the host, false in the build",
		},
		{
			name: "an interface method whose candidates are the same",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryInterface, Name: "Close",
					Subject: methodOf(host.user, "Mine", "Close"), Found: true,
					Answer: host.platform.Scope().Lookup("Closer")}
			},
		},
		{
			name: "a method a type of the build alone declares",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryMethod, Name: "Flush",
					Subject: methodOf(host.user, "Mine", "Flush")}
			},
			want: "L lookup Method Flush: " + verdictPlatform + ".Flusher.Flush is new in the build, of the " +
				"subject's shape",
		},
		{
			name: "a deprecation with the same message",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
					Subject: host.platform.Scope().Lookup("Stable"), Found: true, Text: "use Other"}
			},
			facts: deprecatedOf("use Other"),
		},
		{
			name: "a deprecation with another message",
			lookup: func(_ *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
					Subject: host.platform.Scope().Lookup("Stable"), Found: true, Text: "use Other"}
			},
			facts: deprecatedOf("use Another"),
			want:  "D lookup Deprecated " + verdictPlatform + " of " + verdictPlatform + ".Stable: its message differs",
		},
		{
			name: "no deprecation of an instance's field, which has no objectpath",
			lookup: func(test *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
					Subject: boxIntField(test, host)}
			},
		},
		{
			name: "a deprecation of an instance's field, which no build can hold",
			lookup: func(test *testing.T, host verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: build_reads.QueryDeprecated, Path: verdictPlatform,
					Subject: boxIntField(test, host), Found: true, Text: "use Other"}
			},
			want: "D lookup Deprecated " + verdictPlatform + ": the subject has no objectpath",
		},
		{
			name: "a lookup of a kind the verdict does not know",
			lookup: func(*testing.T, verdictWorld) build_reads.Lookup {
				return build_reads.Lookup{Query: "Planted", Name: "Anything"}
			},
			want: "L lookup Planted Anything: a lookup of this kind cannot be asked again",
		},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			restoring, host := hostReads(test, "stable")
			restoring.Reads.AddLookup(step.lookup(test, host))
			holds, why := holdsIn(restoring, NewReadsBuild(), linuxBuild(test, step.facts))
			if holds != (step.want == "") || why != step.want {
				test.Errorf("want %t, %q, got %t, %q", step.want == "", step.want, holds, why)
			}
		})
	}
}

// boxIntField is the field Held of platform's Box instantiated with int, a field of an instance, which has no
// objectpath, as a key of a composite literal of the instance names it.
func boxIntField(test *testing.T, host verdictWorld) types.Object {
	test.Helper()

	instance, instanceError := types.Instantiate(nil, host.platform.Scope().Lookup("Box").Type(),
		[]types.Type{types.Typ[types.Int]}, true)
	if instanceError != nil {
		test.Fatal(instanceError)
	}
	structure, isStruct := instance.Underlying().(*types.Struct)
	if !isStruct {
		test.Fatalf("Box[int] is a %T, want a struct", instance.Underlying())
	}
	return structure.Field(0)
}

// methodOf is the method named method of the type named typeName in declaring.
func methodOf(declaring *types.Package, typeName string, method string) types.Object {
	named, _ := declaring.Scope().Lookup(typeName).Type().(*types.Named)
	for declared := range named.Methods() {
		if declared.Name() == method {
			return declared
		}
	}
	return nil
}

// Reads marked never, or never recorded, fail at once, whatever the build holds, with why.
func TestNeverAndUnknownReadsNeverHold(test *testing.T) {
	test.Parallel()

	never, _ := hostReads(test, "stable")
	never.Reads.MarkNever("a finding names another package's file")
	unknown := Restoring{Path: verdictUser, Imports: []string{verdictPlatform}, Reads: &build_reads.Reads{}}
	for _, step := range []struct {
		restoring Restoring
		want      string
	}{
		{never, "never: a finding names another package's file"},
		{unknown, "unknown: its type reads were never recorded"},
	} {
		holds, why := holdsIn(step.restoring, NewReadsBuild(), linuxBuild(test, nil))
		if holds || why != step.want {
			test.Errorf("want false, %q, got %t, %q", step.want, holds, why)
		}
	}
}
