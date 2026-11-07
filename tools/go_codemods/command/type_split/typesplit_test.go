package main

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"sort"
	"strings"
	"testing"
)

// testSurroundings builds for darwin on arm64 with no tags, whatever machine runs the tests, the way
// the rules run where cohere's census does.
var testSurroundings = surroundings{importName: assumedImportName, builds: hostBuild(testContext())}

func testContext() build.Context {
	context := build.Default
	context.GOOS, context.GOARCH, context.BuildTags, context.CgoEnabled = "darwin", "arm64", nil, true
	return context
}

// splitFiles runs a split over an in-memory directory and returns every file after it.
func splitFiles(
	test *testing.T,
	files map[string]string,
	pinned map[string]string,
) (map[string]string, *directoryResult) {
	test.Helper()
	sources := map[string][]byte{}
	for name, content := range files {
		sources[name] = []byte(content)
	}
	result, splitDirectoryError := splitDirectory("example", sources, nil, pinned, testSurroundings)
	if splitDirectoryError != nil {
		test.Fatal(splitDirectoryError)
	}
	if len(result.failures) > 0 {
		test.Fatalf("failures: %v", result.failures)
	}
	after := maps.Clone(files)
	for name, content := range result.changes {
		if content == nil {
			delete(after, name)
		} else {
			after[name] = string(content)
		}
	}
	return after, result
}

// declared lists a file's top-level type names and method names (as Type.Method) and its import paths.
func declared(test *testing.T, source string) (names []string, imports []string) {
	test.Helper()
	syntax, parseFileError := parser.ParseFile(token.NewFileSet(), "", source, parser.ParseComments)
	if parseFileError != nil {
		test.Fatalf("%v\n%s", parseFileError, source)
	}
	for _, spec := range syntax.Imports {
		imports = append(imports, importPath(spec))
	}
	for _, declaration := range syntax.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			if receiver := receiverTypeName(node); receiver != "" {
				names = append(names, receiver+"."+node.Name.Name)
			} else {
				names = append(names, node.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					names = append(names, "type "+typeSpec.Name.Name)
				}
			}
		}
	}
	sort.Strings(imports)
	return names, imports
}

func TestSplit(test *testing.T) {
	test.Parallel()
	type expectation struct {
		names   []string
		imports []string
		// contains are substrings the file must hold, such as a copied build line.
		contains []string
	}
	cases := []struct {
		name        string
		files       map[string]string
		pinned      map[string]string
		expect      map[string]*expectation // nil: the file must not exist
		conflicts   int
		methodMoves int
		// nameConflicts is the files the name pass lists and leaves alone.
		nameConflicts int
	}{
		{
			name: "two types in one file split, imports trimmed on both sides",
			files: map[string]string{"circle.go": `package shapes

import (
	"fmt"
	"math"
)

// Circle is round.
type Circle struct{ radius float64 }

// Area is the circle's area.
func (circle Circle) Area() float64 { return math.Pi * circle.radius * circle.radius }

// Square is not.
type Square struct{ side float64 }

// String names the square.
func (square *Square) String() string { return fmt.Sprint(square.side) }
`},
			expect: map[string]*expectation{
				"circle.go": {
					names:    []string{"type Circle", "Circle.Area"},
					imports:  []string{"math"},
					contains: []string{"// Circle is round.", "// Area is the circle's area."},
				},
				"square.go": {
					names:    []string{"type Square", "Square.String"},
					imports:  []string{"fmt"},
					contains: []string{"// Square is not.\ntype Square", "// String names the square.\nfunc"},
				},
			},
		},
		{
			name: "the type named like the file stays even when smaller, and a free helper only it uses is a family candidate",
			files: map[string]string{"reader.go": `package input

type Buffer struct{ data []byte }

func (buffer *Buffer) Len() int { return len(buffer.data) }
func (buffer *Buffer) Cap() int { return cap(buffer.data) }
func (buffer *Buffer) Reset()   { buffer.data = buffer.data[:0] }

type Reader struct{}

func (Reader) Read() int { return helper() }

func helper() int { return 1 }

func shared() *Buffer { return &Buffer{} }
`},
			expect: map[string]*expectation{
				"reader.go": {names: []string{"type Reader", "Reader.Read", "helper", "shared"}},
				"buffer.go": {names: []string{"type Buffer", "Buffer.Len", "Buffer.Cap", "Buffer.Reset"}},
			},
		},
		{
			name: "methods of a type split by theme across files are not a violation, and stay",
			files: map[string]string{
				"engine.go":       "package motor\n\ntype Engine struct{}\n\nfunc (Engine) Start() { run() }\n",
				"engine_tune.go":  "package motor\n\nfunc (Engine) Tune() { run() }\n\nfunc idle() {}\n",
				"engine_linux.go": "package motor\n\nfunc (Engine) Poll() { run() }\n",
				"engine_test.go":  "package motor\n\nfunc (Engine) fake() { run() }\n",
			},
			expect: map[string]*expectation{
				"engine.go":       {names: []string{"type Engine", "Engine.Start"}},
				"engine_tune.go":  {names: []string{"Engine.Tune", "idle"}},
				"engine_linux.go": {names: []string{"Engine.Poll"}},
				"engine_test.go":  {names: []string{"Engine.fake"}},
			},
		},
		{
			name: "methods of a type declared elsewhere, in a file serving another type, join the declaration",
			files: map[string]string{
				"engine.go": "package motor\n\ntype Engine struct{}\n\nfunc (Engine) Start() { run() }\n",
				"wheel.go": `package motor

import "strings"

type Wheel struct{ size int }

func (wheel Wheel) Size() int { return wheel.size }

// Name is the engine's name.
func (Engine) Name() string { return strings.ToUpper("v8") }

func (wheel Wheel) Spin() { run() }
`,
			},
			expect: map[string]*expectation{
				"engine.go": {
					names:    []string{"type Engine", "Engine.Start", "Engine.Name"},
					imports:  []string{"strings"},
					contains: []string{"// Name is the engine's name.\nfunc (Engine) Name()"},
				},
				"wheel.go": {names: []string{"type Wheel", "Wheel.Size", "Wheel.Spin"}, imports: []string{}},
			},
			methodMoves: 1,
		},
		{
			name: "methods that would push the declaration file past 2000 lines go to a themed file",
			files: map[string]string{
				"engine.go": "package motor\n\n" +
					strings.Repeat("// Padding.\n", 1994) +
					"type Engine struct{}\n\nfunc (Engine) Start() { run() }\n",
				"wheel.go": "package motor\n\ntype Wheel struct{}\n\nfunc (Wheel) Spin() { run() }\n\nfunc (Wheel) Roll() { run() }\n\nfunc (Engine) Name() string { return \"v8\" }\n",
			},
			expect: map[string]*expectation{
				"engine.go":       {names: []string{"type Engine", "Engine.Start"}},
				"wheel.go":        {names: []string{"type Wheel", "Wheel.Spin", "Wheel.Roll"}},
				"engine_wheel.go": {names: []string{"Engine.Name"}},
			},
			methodMoves: 1,
		},
		{
			name: "methods of a production type in a test file serving a test type go to a themed test file",
			files: map[string]string{
				"engine.go":       "package motor\n\ntype Engine struct{}\n\nfunc (Engine) Start() { run() }\n",
				"fixture_test.go": "package motor\n\ntype fixture struct{}\n\nfunc (fixture) Use() { run() }\n\nfunc (Engine) fake() { run() }\n",
			},
			expect: map[string]*expectation{
				"engine.go":              {names: []string{"type Engine", "Engine.Start"}},
				"fixture_test.go":        {names: []string{"type fixture", "fixture.Use"}},
				"engine_fixture_test.go": {names: []string{"Engine.fake"}},
			},
			methodMoves: 1,
		},
		{
			name: "a declaration leaving goes to the file named for it, and a themed file of its methods stays",
			files: map[string]string{
				"apple.go":       "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\n// Banana is yellow.\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"banana_ripe.go": "package fruit\n\nfunc (Banana) Ripen() { run() }\n",
			},
			expect: map[string]*expectation{
				"apple.go":       {names: []string{"type Apple", "Apple.Eat"}},
				"banana_ripe.go": {names: []string{"Banana.Ripen"}},
				"banana.go": {
					names:    []string{"type Banana", "Banana.Peel"},
					contains: []string{"// Banana is yellow.\ntype Banana"},
				},
			},
		},
		{
			name: "a declaration leaving joins a themed file of its methods when its own name is pinned",
			files: map[string]string{
				"apple.go":       "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\n// Banana is yellow.\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"banana_ripe.go": "package fruit\n\nfunc (Banana) Ripen() { run() }\n",
				"banana.go":      "package fruit\n\ntype Cherry struct{}\n\nfunc (Cherry) Pit() { run() }\n",
			},
			pinned: map[string]string{"banana.go": "skipped"},
			expect: map[string]*expectation{
				"apple.go":  {names: []string{"type Apple", "Apple.Eat"}},
				"banana.go": {names: []string{"type Cherry", "Cherry.Pit"}},
				// The themed file then declares Banana under a theme the rule refuses a declaring file, and the
				// pinned banana.go holds the name it would take, so it stays and is listed.
				"banana_ripe.go": {
					names:    []string{"Banana.Ripen", "type Banana", "Banana.Peel"},
					contains: []string{"// Banana is yellow.\ntype Banana"},
				},
			},
			nameConflicts: 2,
		},
		{
			name: "methods follow a declaration that moved in the same run",
			files: map[string]string{
				"apple.go":  "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"cherry.go": "package fruit\n\ntype Cherry struct{}\n\nfunc (Cherry) Pit() { run() }\n\nfunc (Banana) Split() { run() }\n",
			},
			expect: map[string]*expectation{
				"apple.go":  {names: []string{"type Apple", "Apple.Eat"}},
				"cherry.go": {names: []string{"type Cherry", "Cherry.Pit"}},
				"banana.go": {names: []string{"type Banana", "Banana.Peel", "Banana.Split"}},
			},
			methodMoves: 1,
		},
		{
			name: "a _test.go and a _darwin.go keep their suffixes, and a //go:build line is copied",
			files: map[string]string{
				"fixture_test.go": `package motor

import "testing"

type fixture struct{}

func (fixture) Use(t *testing.T) { t.Helper() }

type recorder struct{ calls int }

func (recording *recorder) Record() { recording.calls++ }
`,
				"poll_darwin.go": `//go:build darwin && arm64

// Package-level comment that is not a doc.

package motor

import "syscall"

type poller struct{ fd int }

func (poll poller) Close() error { return syscall.Close(poll.fd) }

type waitGroupHTTP struct{}

func (waitGroupHTTP) Wait() { run() }
`,
			},
			expect: map[string]*expectation{
				"fixture_test.go": {
					names:   []string{"type fixture", "fixture.Use"},
					imports: []string{"testing"},
				},
				"recorder_test.go": {names: []string{"type recorder", "recorder.Record"}},
				"poller_darwin.go": {
					names:   []string{"type poller", "poller.Close"},
					imports: []string{"syscall"},
				},
				"wait_group_http_darwin.go": {
					names:    []string{"type waitGroupHTTP", "waitGroupHTTP.Wait"},
					contains: []string{"//go:build darwin && arm64\n\npackage motor"},
				},
			},
		},
		{
			name: "a type in a grouped block moves out as its own declaration, the rest stay grouped",
			files: map[string]string{"alpha.go": `package greek

// The letters.
type (
	// Alpha is first.
	Alpha struct{}
	// Beta is second.
	Beta struct{ value int } // trailing
	// Gamma has no methods.
	Gamma int
)

func (Alpha) First() { run() }

func (beta Beta) Second() int { return beta.value }
`},
			expect: map[string]*expectation{
				"alpha.go": {
					names:    []string{"type Alpha", "type Gamma", "Alpha.First"},
					contains: []string{"// The letters.\ntype (", "// Gamma has no methods."},
				},
				"beta.go": {
					names:    []string{"type Beta", "Beta.Second"},
					contains: []string{"// Beta is second.\ntype Beta struct{ value int } // trailing"},
				},
			},
		},
		{
			name: "a grouped block that loses every type is removed, its doc travels with the first",
			files: map[string]string{"pairs.go": `package greek

// Pair of letters.
type (
	Delta   struct{}
	Epsilon struct{}
)

type Pairs struct{}

func (Pairs) Count() int { return 2 }
func (Delta) D()         { run() }
func (Epsilon) E()       { run() }
`},
			expect: map[string]*expectation{
				"pairs.go": {names: []string{"type Pairs", "Pairs.Count"}},
				"delta.go": {
					names:    []string{"type Delta", "Delta.D"},
					contains: []string{"// Pair of letters.\ntype Delta struct{}"},
				},
				"epsilon.go": {names: []string{"type Epsilon", "Epsilon.E"}},
			},
		},
		{
			name: "aliased imports travel by their alias",
			files: map[string]string{"client.go": `package network

import (
	stdhttp "net/http"
	"strings"
)

type Client struct{}

func (Client) Do() *stdhttp.Request { return nil }

type Server struct{}

func (Server) Handle(writer stdhttp.ResponseWriter) { _ = strings.ToLower("") }
`},
			expect: map[string]*expectation{
				"client.go": {names: []string{"type Client", "Client.Do"}, imports: []string{"net/http"}},
				"server.go": {
					names:    []string{"type Server", "Server.Handle"},
					imports:  []string{"net/http", "strings"},
					contains: []string{`stdhttp "net/http"`},
				},
			},
		},
		{
			name: "a target serving another type is first named for it, and the type then takes the name it freed",
			files: map[string]string{
				"apple.go":  "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"banana.go": "package fruit\n\ntype Cherry struct{}\n\nfunc (Cherry) Pit() { run() }\n",
			},
			expect: map[string]*expectation{
				"apple.go":  {names: []string{"type Apple", "Apple.Eat"}},
				"cherry.go": {names: []string{"type Cherry", "Cherry.Pit"}},
				"banana.go": {names: []string{"type Banana", "Banana.Peel"}},
			},
		},
		{
			name: "a target test file holding another type with methods is a conflict, and the type stays",
			files: map[string]string{
				"apple_test.go":  "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"banana_test.go": "package fruit\n\ntype Cherry struct{}\n\nfunc (Cherry) Pit() { run() }\n",
			},
			expect: map[string]*expectation{
				"apple_test.go":  {names: []string{"type Apple", "Apple.Eat", "type Banana", "Banana.Peel"}},
				"banana_test.go": {names: []string{"type Cherry", "Cherry.Pit"}},
			},
			conflicts: 1,
		},
		{
			name: "a target holding no type with methods takes the type",
			files: map[string]string{
				"apple.go":  "package fruit\n\nimport \"strings\"\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\ntype Banana struct{}\n\nfunc (Banana) Peel() string { return strings.Repeat(\"a\", 2) }\n",
				"banana.go": "package fruit\n\nimport \"fmt\"\n\nfunc ripe() { fmt.Println() }\n",
			},
			expect: map[string]*expectation{
				"apple.go": {names: []string{"type Apple", "Apple.Eat"}, imports: []string{}},
				"banana.go": {
					names:   []string{"ripe", "type Banana", "Banana.Peel"},
					imports: []string{"fmt", "strings"},
				},
			},
		},
		{
			name: "a target in another package of the directory is a conflict",
			files: map[string]string{
				"apple_test.go":  "package fruit\n\ntype Apple struct{}\n\nfunc (Apple) Eat() { run() }\n\ntype Banana struct{}\n\nfunc (Banana) Peel() { run() }\n",
				"banana_test.go": "package fruit_test\n\nfunc helper() {}\n",
			},
			expect: map[string]*expectation{
				"apple_test.go":  {names: []string{"type Apple", "Apple.Eat", "type Banana", "Banana.Peel"}},
				"banana_test.go": {names: []string{"helper"}},
			},
			conflicts: 1,
		},
		{
			name: "a name whose file name would add a build constraint is a conflict",
			files: map[string]string{
				"main.go": "package app\n\ntype App struct{}\n\nfunc (App) Run() { run() }\n\ntype ConfigTest struct{}\n\nfunc (ConfigTest) Check() { run() }\n\ntype modeLinux struct{}\n\nfunc (modeLinux) Set() { run() }\n",
			},
			expect: map[string]*expectation{
				"main.go": {names: []string{
					"type App",
					"App.Run",
					"type ConfigTest",
					"ConfigTest.Check",
					"type modeLinux",
					"modeLinux.Set",
				}},
			},
			conflicts: 2,
		},
		{
			name: "a pinned or generated file is not touched",
			files: map[string]string{
				"owned.go":     "package app\n\ntype One struct{}\n\nfunc (One) A() { run() }\n\ntype Two struct{}\n\nfunc (Two) B() { run() }\n",
				"generated.go": "// Code generated by hand. DO NOT EDIT.\n\npackage app\n\ntype Three struct{}\n\nfunc (Three) C() { run() }\n\ntype Four struct{}\n\nfunc (Four) D() { run() }\n",
			},
			pinned: map[string]string{"owned.go": "skipped"},
			expect: map[string]*expectation{
				"owned.go":     {names: []string{"type One", "One.A", "type Two", "Two.B"}},
				"generated.go": {names: []string{"type Three", "Three.C", "type Four", "Four.D"}},
				"three.go":     nil,
				"two.go":       nil,
			},
		},
		{
			name: "generic types count and move",
			files: map[string]string{
				"set.go": "package collection\n\ntype Set[T comparable] struct{ items map[T]bool }\n\nfunc (set *Set[T]) Add(item T) { set.items[item] = true }\n\ntype Pair[K comparable, V any] struct{ key K; value V }\n\nfunc (pair Pair[K, V]) Key() K { return pair.key }\n",
			},
			expect: map[string]*expectation{
				"set.go":  {names: []string{"type Set", "Set.Add"}},
				"pair.go": {names: []string{"type Pair", "Pair.Key"}},
			},
		},
	}
	for _, testCase := range cases {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			after, result := splitFiles(test, testCase.files, testCase.pinned)
			for name, want := range testCase.expect {
				content, exists := after[name]
				if want == nil {
					if exists {
						test.Errorf("%s exists, want it absent:\n%s", name, content)
					}
					continue
				}
				if !exists {
					test.Errorf("%s is missing; files: %v", name, slices.Sorted(maps.Keys(after)))
					continue
				}
				names, imports := declared(test, content)
				if !sameElements(names, want.names) {
					test.Errorf("%s declares %v, want %v\n%s", name, names, want.names, content)
				}
				if want.imports != nil && !slices.Equal(imports, want.imports) {
					test.Errorf("%s imports %v, want %v\n%s", name, imports, want.imports, content)
				}
				for _, text := range want.contains {
					if !strings.Contains(content, text) {
						test.Errorf("%s does not contain %q:\n%s", name, text, content)
					}
				}
			}
			if len(result.conflicts) != testCase.conflicts {
				test.Errorf("conflicts %v, want %d", result.conflicts, testCase.conflicts)
			}
			if len(result.nameConflicts) != testCase.nameConflicts {
				test.Errorf("name conflicts %v, want %d", result.nameConflicts, testCase.nameConflicts)
			}
			if moves := result.methodMovesToDeclaration + result.methodMovesToThemed; moves != testCase.methodMoves {
				test.Errorf("method moves %d, want %d", moves, testCase.methodMoves)
			}

			// Idempotence: a second run over the result changes nothing.
			again, second := splitFiles(test, after, testCase.pinned)
			if len(second.changes) > 0 {
				test.Errorf("a second run changes %v", slices.Sorted(maps.Keys(second.changes)))
			}
			if !maps.Equal(again, after) {
				test.Error("a second run's files differ from the first's")
			}
		})
	}
}

func TestFamilyCandidates(test *testing.T) {
	test.Parallel()
	_, result := splitFiles(test, map[string]string{"reader.go": `package input

type Buffer struct{ data []byte; mode bufferMode }

type bufferMode int

func (buffer *Buffer) Len() int { return grow(len(buffer.data)) }

type Reader struct{}

func (Reader) Read() int { return 0 }

func grow(size int) int { return size * 2 }

func shared() *Buffer { return &Buffer{} }
`}, nil)
	want := "example/reader.go: Buffer moved to buffer.go; used only by it: bufferMode, grow"
	if len(result.families) != 1 || result.families[0] != want {
		test.Errorf("families %q, want %q", result.families, want)
	}
}

func TestCheckGroupRejectsChanges(test *testing.T) {
	test.Parallel()
	original := "package p\n\n// Doc.\ntype A struct{}\n\nfunc (A) M() int { return 1 }\n"
	cases := map[string]string{
		"a changed body":   "package p\n\n// Doc.\ntype A struct{}\n\nfunc (A) M() int { return 2 }\n",
		"a lost comment":   "package p\n\ntype A struct{}\n\nfunc (A) M() int { return 1 }\n",
		"a lost method":    "package p\n\n// Doc.\ntype A struct{}\n",
		"a doubled method": "package p\n\n// Doc.\ntype A struct{}\n\nfunc (A) M() int { return 1 }\n\nfunc (A) M() int { return 1 }\n",
	}
	for name, changed := range cases {
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			groups, parseGroupsError := parseGroups(
				"example",
				map[string][]byte{"a.go": []byte(original)},
				nil,
				testSurroundings,
			)
			if parseGroupsError != nil {
				test.Fatal(parseGroupsError)
			}
			if checkGroupError := checkGroup(
				groups[0],
				map[string][]byte{"a.go": []byte(changed)},
			); checkGroupError == nil {
				test.Error("the check passed")
			}
		})
	}
	groups, parseGroupsError := parseGroups(
		"example",
		map[string][]byte{"a.go": []byte(original)},
		nil,
		testSurroundings,
	)
	if parseGroupsError != nil {
		test.Fatal(parseGroupsError)
	}
	moved := map[string][]byte{
		"a.go": []byte("package p\n\n// Doc.\ntype A struct{}\n"),
		"m.go": []byte("//go:build linux\n\npackage p\n\nimport \"fmt\"\n\nfunc (A) M() int { return 1 }\n"),
	}
	if checkGroupError := checkGroup(groups[0], moved); checkGroupError != nil {
		test.Errorf("a pure move fails the check: %v", checkGroupError)
	}
}

func TestNames(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]string{
		"swiftRun": "swift_run", "HTTPServer": "http_server", "ParseJSON": "parse_json", "UserIDs": "user_ids",
		"Base64Encoder": "base64_encoder", "utf8Reader": "utf8_reader", "already_snake": "already_snake", "A": "a",
	} {
		if got := snakeCase(name); got != want {
			test.Errorf("snakeCase(%q) = %q, want %q", name, got, want)
		}
	}
	for name, want := range map[string][2]string{
		"poll_linux_amd64_test.go": {"poll", "_linux_amd64_test"}, "x_test.go": {"x", "_test"},
		"linux.go": {"linux", ""}, "net_windows.go": {"net", "_windows"}, "plain.go": {"plain", ""},
	} {
		if base, suffix := fileNameParts(name); base != want[0] || suffix != want[1] {
			test.Errorf("fileNameParts(%q) = %q, %q, want %q", name, base, suffix, want)
		}
	}
	for path, want := range map[string]string{
		"gopkg.in/yaml.v3":       "yaml",
		"github.com/x/go-isatty": "isatty",
		"example.com/m/v2":       "m",
		"net/http":               "http",
	} {
		if got := assumedImportName(path); got != want {
			test.Errorf("assumedImportName(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestSkipPrefixes(test *testing.T) {
	test.Parallel()
	var skip skipPrefixes
	for _, value := range []string{"internal/sentence", "policy/", "command/cohere/fix.go"} {
		if setError := skip.Set(value); setError != nil {
			test.Fatal(setError)
		}
	}
	if setError := skip.Set(" / "); setError == nil {
		test.Error("an empty prefix was accepted")
	}
	for path, want := range map[string]bool{
		"internal/sentence": true, "internal/sentence/split.go": true, "internal/sentences": false,
		"policy": true, "policy/tools/generate": true, "policyx": false,
		"command/cohere/fix.go": true, "command/cohere/fix_test.go": false, "command/cohere": false,
	} {
		if got := skip.matches(path); got != want {
			test.Errorf("matches(%q) = %v, want %v", path, got, want)
		}
	}
}

func sameElements(got []string, want []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want)))
}
