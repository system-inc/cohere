package global_rename

import (
	"bufio"
	"bytes"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/tools/go_codemods/rename"
	"github.com/system-inc/cohere/tools/go_codemods/workspace"
)

// fixture is a temporary multi-package module with its own go.work, loaded as a cohere worktree is.
type fixture struct {
	root string
}

func newFixture(test *testing.T, files map[string]string) fixture {
	test.Helper()
	root := test.TempDir()
	all := map[string]string{
		"go.work": "go 1.27\n\nuse .\n",
		"go.mod":  "module fixture\n\ngo 1.27\n",
	}
	for name, contents := range files {
		all[name] = contents
	}
	for name, contents := range all {
		path := filepath.Join(root, name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return fixture{root: root}
}

func (fixture fixture) load(test *testing.T) *workspace.Workspace {
	test.Helper()
	loaded, loadError := workspace.Load(fixture.root, nil)
	if loadError != nil {
		test.Fatalf("loading: %v", loadError)
	}
	return loaded
}

func (fixture fixture) read(test *testing.T, name string) string {
	test.Helper()
	contents, readError := os.ReadFile(filepath.Join(fixture.root, name))
	if readError != nil {
		test.Fatal(readError)
	}
	return string(contents)
}

// target names one declaration to propose: the identifier named Name that declares an object of Kind, in the
// package whose path ends with PackageSuffix, and for a method or field, whose receiver or struct mentions
// Owner.
type target struct {
	PackageSuffix string
	Name          string
	Kind          string
	Owner         string
	NewName       string
}

// propose finds each target in the analyzed variants and proposes it.
func propose(test *testing.T, loaded *workspace.Workspace, targets ...target) []Proposal {
	test.Helper()
	var proposals []Proposal
	for _, wanted := range targets {
		found := false
		for _, analyzedPackage := range loaded.Analyzed {
			if !strings.HasSuffix(analyzedPackage.PkgPath, wanted.PackageSuffix) || found {
				continue
			}
			for identifier, object := range analyzedPackage.TypesInfo.Defs {
				if object == nil || identifier.Name != wanted.Name || kindOf(object) != wanted.Kind {
					continue
				}
				if wanted.Owner != "" && !ownedBy(object, wanted.Owner) {
					continue
				}
				proposals = append(
					proposals,
					Proposal{Package: analyzedPackage, Object: object, NewName: wanted.NewName},
				)
				found = true
				break
			}
		}
		if !found {
			test.Fatalf("no declaration matches %+v", wanted)
		}
	}
	return proposals
}

// ownedBy reports whether a method's receiver, or the struct holding a field, is the type named owner.
func ownedBy(object types.Object, owner string) bool {
	if function, isFunction := object.(*types.Func); isFunction && function.Signature().Recv() != nil {
		return strings.Contains(function.Signature().Recv().Type().String(), owner)
	}
	if variable, isVariable := object.(*types.Var); isVariable && variable.IsField() {
		scope := variable.Pkg().Scope()
		named, isNamed := scope.Lookup(owner).(*types.TypeName)
		if !isNamed {
			return false
		}
		structType, isStruct := named.Type().Underlying().(*types.Struct)
		if !isStruct {
			return false
		}
		for index := range structType.NumFields() {
			if structType.Field(index) == variable {
				return true
			}
		}
	}
	return false
}

// applyClean writes the plan, reloads, and requires zero fingerprint mismatches.
func applyClean(test *testing.T, fixture fixture, loaded *workspace.Workspace, result *Result) {
	test.Helper()
	var output bytes.Buffer
	report := bufio.NewWriter(&output)
	mismatches, applyError := Apply(loaded, fixture.root, nil, result, report)
	if flushError := report.Flush(); flushError != nil {
		test.Fatalf("writing the report: %v", flushError)
	}
	if applyError != nil {
		test.Fatalf("applying: %v\n%s", applyError, output.String())
	}
	if mismatches != 0 {
		test.Fatalf("%d fingerprint mismatches:\n%s", mismatches, output.String())
	}
}

func requireAccepted(test *testing.T, result *Result, count int) {
	test.Helper()
	if len(result.Accepted) != count {
		test.Fatalf(
			"accepted %d renames, want %d: %+v\nrefused: %+v",
			len(result.Accepted),
			count,
			result.Accepted,
			result.Rejections,
		)
	}
}

func requireRefused(test *testing.T, result *Result, oldName string, class string) {
	test.Helper()
	for _, accepted := range result.Accepted {
		if accepted.OldName == oldName {
			test.Fatalf("%s was accepted, want it refused (%s)", oldName, class)
		}
	}
	for _, rejection := range result.Rejections {
		if rejection.OldName == oldName && strings.Contains(rejection.Class, class) {
			return
		}
	}
	test.Fatalf("%s not refused as %q; refusals: %+v", oldName, class, result.Rejections)
}

func requireModelClean(test *testing.T, result *Result) {
	test.Helper()
	if len(result.Unresolved)+len(result.SelectionModelMismatches)+len(result.SatisfactionModelMismatches) > 0 {
		test.Fatalf(
			"model disagreed with go/types: %v %v %v",
			result.Unresolved,
			result.SelectionModelMismatches,
			result.SatisfactionModelMismatches,
		)
	}
}

func TestFieldRenamedAcrossPackages(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

// Record is one row.
type Record struct {
	// GroupID names the group.
	GroupID string
	Count   int
}

func (record Record) Describe() string { return record.GroupID }
`,
		"internal/b/b.go": `package b

import "fixture/internal/a"

func Build() a.Record {
	record := a.Record{GroupID: "x"}
	record.GroupID += "y"
	pointer := &record
	return a.Record{GroupID: pointer.GroupID, Count: 1}
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{})
	requireAccepted(test, result, 1)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(
		test,
		"internal/b/b.go",
	); strings.Contains(source, "GroupID") || strings.Count(source, "GroupId") != 4 {
		test.Fatalf("b.go not renamed:\n%s", source)
	}
	if source := fixture.read(test, "internal/a/a.go"); !strings.Contains(source, "// GroupId names the group.") {
		test.Fatalf("the field's doc comment was not respelled:\n%s", source)
	}
}

func TestMethodBreakingInterfaceSatisfactionRefused(test *testing.T) {
	test.Parallel()
	test.Run("same package, implementation alone", func(test *testing.T) {
		test.Parallel()
		fixture := newFixture(test, map[string]string{
			"internal/a/a.go": `package a

type Fetcher interface{ FetchURL() string }

type page struct{}

func (page) FetchURL() string { return "" }

func Use(fetcher Fetcher) string { return fetcher.FetchURL() }

func Run() string { return Use(page{}) }
`,
		})
		loaded := fixture.load(test)
		result := Plan(loaded, propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "FetchURL", Kind: KindMethod, Owner: "page", NewName: "FetchUrl"},
		), Options{})
		requireRefused(test, result, "FetchURL", "interface satisfaction")
		requireModelClean(test, result)
	})
	test.Run("same package, interface and implementation together", func(test *testing.T) {
		test.Parallel()
		fixture := newFixture(test, map[string]string{
			"internal/a/a.go": `package a

type Fetcher interface{ FetchURL() string }

type page struct{}

func (page) FetchURL() string { return "" }

func Use(fetcher Fetcher) string { return fetcher.FetchURL() }

func Run() string { return Use(page{}) }
`,
		})
		loaded := fixture.load(test)
		result := Plan(loaded, propose(test, loaded,
			target{PackageSuffix: "internal/a", Name: "FetchURL", Kind: KindMethod, Owner: "page", NewName: "FetchUrl"},
			target{
				PackageSuffix: "internal/a",
				Name:          "FetchURL",
				Kind:          KindMethod,
				Owner:         "Fetcher",
				NewName:       "FetchUrl",
			},
		), Options{})
		requireAccepted(test, result, 2)
		applyClean(test, fixture, loaded, result)
	})
	test.Run("an interface of another package", func(test *testing.T) {
		test.Parallel()
		fixture := newFixture(test, map[string]string{
			"internal/contract/contract.go": `package contract

type Identified interface{ UserID() string }
`,
			"internal/a/a.go": `package a

import "fixture/internal/contract"

type account struct{}

func (account) UserID() string { return "" }

var _ contract.Identified = account{}
`,
		})
		loaded := fixture.load(test)
		result := Plan(loaded, propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "UserID", Kind: KindMethod, NewName: "UserId"},
		), Options{})
		requireRefused(test, result, "UserID", "implements an interface of another package")
	})
}

func TestMethodCollidingWithPromotedMethodRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type Base struct{}

func (Base) FetchUrl() string { return "base" }

type Wrapper struct{ Base }

func (Wrapper) FetchURL() string { return "wrapper" }
`,
		"internal/b/b.go": `package b

import "fixture/internal/a"

func Read(wrapper a.Wrapper) string { return wrapper.FetchUrl() + wrapper.FetchURL() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "FetchURL", Kind: KindMethod, Owner: "Wrapper", NewName: "FetchUrl"},
	), Options{})
	requireRefused(test, result, "FetchURL", "selector")
	requireModelClean(test, result)
}

// encodedFixture marshals a struct straight to json and another through a wrapper that takes any, and prints
// both, so a run before and after a rename shows whether the keys held.
var encodedFixture = map[string]string{
	"internal/a/a.go": `package a

import "encoding/json"

type Payload struct {
	UserID string
	Name   string ` + "`json:\"name\"`" + `
}

type Wrapped struct {
	GroupID string
}

func Encode(payload Payload) ([]byte, error) { return json.Marshal(payload) }

func write(value any) []byte {
	encoded, _ := json.Marshal(value)
	return encoded
}

func EncodeWrapped() []byte { return write(Wrapped{GroupID: "g"}) }
`,
	"cmd/show/main.go": `package main

import (
	"fmt"

	"fixture/internal/a"
)

func main() {
	encoded, _ := a.Encode(a.Payload{UserID: "u", Name: "n"})
	fmt.Println(string(encoded))
	fmt.Println(string(a.EncodeWrapped()))
}
`,
}

func runShow(test *testing.T, fixture fixture) string {
	test.Helper()
	command := exec.Command("go", "run", "./cmd/show")
	command.Dir = fixture.root
	output, runError := command.CombinedOutput()
	if runError != nil {
		test.Fatalf("go run: %v\n%s", runError, output)
	}
	return string(output)
}

func TestEncodedFieldRefusedUntagged(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, encodedFixture)
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "UserID", Kind: KindField, NewName: "UserId"},
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{})
	requireRefused(test, result, "UserID", "encoding guard")
	requireRefused(test, result, "GroupID", "encoding guard")
	if len(result.EncodedFields) != 2 {
		test.Fatalf("encoded fields reported: %+v", result.EncodedFields)
	}
}

func TestEncodedFieldTaggedKeepsItsKey(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, encodedFixture)
	before := runShow(test, fixture)
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "UserID", Kind: KindField, NewName: "UserId"},
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{TagEncodedFields: true})
	requireAccepted(test, result, 2)
	if len(result.TagEdits) != 2 {
		test.Fatalf("tag edits: %+v", result.TagEdits)
	}
	applyClean(test, fixture, loaded, result)
	source := fixture.read(test, "internal/a/a.go")
	if !strings.Contains(source, "UserId string `json:\"UserID\"`") ||
		!strings.Contains(source, "GroupId string `json:\"GroupID\"`") {
		test.Fatalf("tags not added:\n%s", source)
	}
	if after := runShow(test, fixture); after != before {
		test.Fatalf("the encoded output changed:\nbefore %safter  %s", before, after)
	}
	if !strings.Contains(before, `"UserID":"u"`) || !strings.Contains(before, `"GroupID":"g"`) {
		test.Fatalf("the fixture does not encode what it should: %s", before)
	}
}

func TestExportedFunctionInLibraryPackageRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"library/library.go": `package library

func ParseURL(text string) string { return text }

func parseJSON(text string) string { return text }

func Both(text string) string { return ParseURL(parseJSON(text)) }
`,
		"internal/a/a.go": `package a

import "fixture/library"

func Run() string { return library.ParseURL("x") }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "library", Name: "ParseURL", Kind: KindFunc, NewName: "ParseUrl"},
		target{PackageSuffix: "library", Name: "parseJSON", Kind: KindFunc, NewName: "parseJson"},
	), Options{})
	requireRefused(test, result, "ParseURL", "exported outside the workspace")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
}

// A pinned library's export is renamed with its readers; another library's export is still refused.
func TestExportedFunctionInPinnedLibraryRenamed(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"library/library.go": `package library

func ParseURL(text string) string { return text }
`,
		"other/other.go": `package other

func ParseJSON(text string) string { return text }
`,
		"internal/a/a.go": `package a

import (
	"fixture/library"
	"fixture/other"
)

func Run() string { return library.ParseURL(other.ParseJSON("x")) }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "library", Name: "ParseURL", Kind: KindFunc, NewName: "ParseUrl"},
		target{PackageSuffix: "other", Name: "ParseJSON", Kind: KindFunc, NewName: "ParseJson"},
	), Options{PinnedLibraries: map[string]string{"fixture/library": "read at a pin"}})
	requireRefused(test, result, "ParseJSON", "exported outside the workspace")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if got, want := fixture.read(test, "internal/a/a.go"), `package a

import (
	"fixture/library"
	"fixture/other"
)

func Run() string { return library.ParseUrl(other.ParseJSON("x")) }
`; got != want {
		test.Fatalf("internal/a/a.go:\n%s\nwant:\n%s", got, want)
	}
}

func TestNameInStringLiteralRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "reflect"

type Record struct{ GroupID string }

func Group(record Record) string {
	return reflect.ValueOf(record).FieldByName("GroupID").String()
}

type Holder struct{ OwnerID string }

func Path() string { return "Holder.OwnerID" }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
		target{PackageSuffix: "internal/a", Name: "OwnerID", Kind: KindField, NewName: "OwnerId"},
	), Options{})
	requireRefused(test, result, "GroupID", "string literal")
	requireRefused(test, result, "OwnerID", "string literal")
}

func TestPackageFunctionRenamedWithItsTestVariantUses(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

// ParseURL parses.
func ParseURL(text string) string { return text }
`,
		"internal/a/a_internal_test.go": `package a

import "testing"

func TestParseURL(test *testing.T) {
	if ParseURL("x") != "x" {
		test.Fatal("parse failed")
	}
}
`,
		"internal/a/a_test.go": `package a_test

import (
	"testing"

	"fixture/internal/a"
)

func TestExternal(test *testing.T) {
	if a.ParseURL("y") != "y" {
		test.Fatal("no")
	}
}
`,
		"internal/b/b.go": `package b

import "fixture/internal/a"

var Parse = a.ParseURL
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunc, NewName: "ParseUrl"},
		target{PackageSuffix: "internal/a", Name: "TestParseURL", Kind: KindFunc, NewName: "TestParseUrl"},
	), Options{})
	requireAccepted(test, result, 2)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	for _, name := range []string{
		"internal/a/a.go",
		"internal/a/a_internal_test.go",
		"internal/a/a_test.go",
		"internal/b/b.go",
	} {
		source := fixture.read(test, name)
		if strings.Contains(source, "ParseURL") {
			test.Fatalf("%s still names ParseURL:\n%s", name, source)
		}
	}
	command := exec.Command("go", "test", "./...")
	command.Dir = fixture.root
	if output, testError := command.CombinedOutput(); testError != nil {
		test.Fatalf("go test after the rename: %v\n%s", testError, output)
	}
}

func TestLocalShadowingRenamedPackageVariableRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

var userID = "global"

func Read() string {
	userId := "local"
	return userId + userID
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(
		loaded,
		propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "userID", Kind: KindVariable, NewName: "userId"},
		),
		Options{},
	)
	requireRefused(test, result, "userID", "conflict")
}

// overridingFixture is an interface and a type that embeds it and overrides one of its methods: calls through
// the interface reach the override only while the two share a name.
func overridingFixture(extra map[string]string) map[string]string {
	files := map[string]string{
		"internal/a/a.go": `package a

type Program interface {
	FS() int
	Other() int
}
`,
		"internal/b/b.go": `package b

import "fixture/internal/a"

type counting struct{ a.Program }

func (counting) FS() int { return 1 }

type direct struct{}

func (direct) FS() int    { return 2 }
func (direct) Other() int { return 3 }

func Read(program a.Program) int { return program.FS() }

func Wrap(program a.Program) []a.Program { return []a.Program{counting{program}, direct{}} }
`,
	}
	for name, contents := range extra {
		files[name] = contents
	}
	return files
}

// An interface's method renamed without the override of a type embedding it would leave the type satisfying
// the interface through the embedded method, so a call through the interface would skip the override: refused.
// The override is the only implementation, since a plain one would stop satisfying the interface and be refused
// for that.
func TestInterfaceMethodRenamedApartFromAnOverrideRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, overridingFixture(map[string]string{
		"internal/b/b.go": `package b

import "fixture/internal/a"

type counting struct{ a.Program }

func (counting) FS() int { return 1 }

func Wrap(program a.Program) a.Program { return counting{program} }
`,
	}))
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "FS", Kind: KindMethod, Owner: "Program", NewName: "FileSystem"},
	), Options{})
	requireRefused(test, result, "FS", "interface satisfaction")
}

// With RenameImplementations, the interface's method takes its implementations with it, an override and a
// plain method alike, and every call reaches what it reached before.
func TestInterfaceMethodRenamedWithItsImplementations(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, overridingFixture(nil))
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "FS", Kind: KindMethod, Owner: "Program", NewName: "FileSystem"},
	),
		Options{RenameImplementations: true})
	requireAccepted(test, result, 3)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	want := `package b

import "fixture/internal/a"

type counting struct{ a.Program }

func (counting) FileSystem() int { return 1 }

type direct struct{}

func (direct) FileSystem() int { return 2 }
func (direct) Other() int      { return 3 }

func Read(program a.Program) int { return program.FileSystem() }

func Wrap(program a.Program) []a.Program { return []a.Program{counting{program}, direct{}} }
`
	if source := fixture.read(test, "internal/b/b.go"); source != want {
		test.Fatalf("b.go reads:\n%s\nwant:\n%s", source, want)
	}
}

// An implementation that may not move, here because it is generated, keeps the interface's method where it is
// too, and the refusal names it.
func TestInterfaceMethodRefusedWithAnImplementationThatMayNotMove(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, overridingFixture(map[string]string{
		"internal/b/generated.go": `// Code generated by fixture. DO NOT EDIT.

package b

type generated struct{}

func (generated) FS() int    { return 4 }
func (generated) Other() int { return 5 }
`,
	}))
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "FS", Kind: KindMethod, Owner: "Program", NewName: "FileSystem"},
	),
		Options{RenameImplementations: true})
	requireAccepted(test, result, 0)
	for _, rejection := range result.Rejections {
		if rejection.OldName == "FS" &&
			strings.HasSuffix(rejection.Position.Filename, "internal/a/a.go") &&
			strings.Contains(rejection.Reason, "implemented by the method") &&
			strings.Contains(rejection.Reason, "may not be edited") {
			return
		}
	}
	test.Fatalf("the interface's refusal does not name the implementation that may not move: %+v", result.Rejections)
}

func TestTypeRenamedWithItsEmbeddedFields(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type HTTPClient struct{ Timeout int }

type Service struct {
	*HTTPClient
	Name string
}

func Make() Service { return Service{HTTPClient: &HTTPClient{}} }

func Timeout(service Service) int { return service.HTTPClient.Timeout + service.Timeout }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "HTTPClient", Kind: KindType, NewName: "HttpClient"},
	), Options{})
	requireAccepted(test, result, 2)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/a/a.go"); strings.Contains(source, "HTTPClient") {
		test.Fatalf("HTTPClient remains:\n%s", source)
	}
}

func TestEncodedThroughFunctionFieldAndResultRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "encoding/gob"
import "bytes"

type Pass struct{ Export func(fact any) }

type store struct{ facts map[string]any }

func (current *store) export(fact any) { current.facts["x"] = fact }

func (current *store) encode() []byte {
	var buffer bytes.Buffer
	for _, fact := range current.facts {
		_ = gob.NewEncoder(&buffer).Encode(fact)
	}
	return buffer.Bytes()
}

type Fact struct{ ReportID string }

type Result struct{ OwnerURL string }

func build() (any, error) { return Result{OwnerURL: "u"}, nil }

func Run() []byte {
	current := &store{facts: map[string]any{}}
	pass := Pass{Export: current.export}
	pass.Export(&Fact{ReportID: "r"})
	built, _ := build()
	_ = gob.NewEncoder(&bytes.Buffer{}).Encode(built)
	return current.encode()
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "ReportID", Kind: KindField, NewName: "ReportId"},
		target{PackageSuffix: "internal/a", Name: "OwnerURL", Kind: KindField, NewName: "OwnerUrl"},
	), Options{TagEncodedFields: true})
	requireRefused(test, result, "ReportID", "encoding guard")
	requireRefused(test, result, "OwnerURL", "encoding guard")
}

// A plan whose rewrite loads cleanly but resolves an identifier to another declaration is refused whole: the
// overlay reload sees the mismatch and no file is written, the one that holds the bad edit nor any other.
func TestApplyWritesNothingWhenTheRewriteMismatches(test *testing.T) {
	test.Parallel()
	source := `package a

func one() int { return 1 }

func two() int { return 2 }

func Run() int { return one() }
`
	other := `package a

func Other() int { return two() }
`
	fixture := newFixture(test, map[string]string{"internal/a/a.go": source, "internal/a/other.go": other})
	loaded := fixture.load(test)
	filename := filepath.Join(loaded.Root, "internal/a/a.go")
	otherFilename := filepath.Join(loaded.Root, "internal/a/other.go")
	useOffset := strings.LastIndex(source, "one()")
	result := &Result{Edits: []rename.Edit{
		// Respells the use of one as two, which loads and type-checks, yet now resolves to two.
		{Filename: filename, Offset: useOffset, OldText: "one", NewText: "two"},
		// A sound edit in another file, which must not be written either.
		{Filename: otherFilename, Offset: strings.Index(other, "two()"), OldText: "two", NewText: "two"},
	}}
	var output bytes.Buffer
	report := bufio.NewWriter(&output)
	mismatches, applyError := Apply(loaded, fixture.root, nil, result, report)
	if flushError := report.Flush(); flushError != nil {
		test.Fatalf("writing the report: %v", flushError)
	}
	if applyError != nil {
		test.Fatalf("applying: %v\n%s", applyError, output.String())
	}
	if mismatches == 0 || !strings.Contains(output.String(), "nothing was written") {
		test.Fatalf("mismatches %d, want at least one and a refusal:\n%s", mismatches, output.String())
	}
	if got := fixture.read(test, "internal/a/a.go"); got != source {
		test.Fatalf("a.go was written despite the mismatch:\n%s", got)
	}
}

// A plan whose rewrite does not type-check is refused before anything is written.
func TestApplyWritesNothingWhenTheRewriteDoesNotLoad(test *testing.T) {
	test.Parallel()
	source := `package a

func one() int { return 1 }

func Run() int { return one() }
`
	fixture := newFixture(test, map[string]string{"internal/a/a.go": source})
	loaded := fixture.load(test)
	filename := filepath.Join(loaded.Root, "internal/a/a.go")
	result := &Result{Edits: []rename.Edit{
		{Filename: filename, Offset: strings.LastIndex(source, "one()"), OldText: "one", NewText: "missing"},
	}}
	var output bytes.Buffer
	report := bufio.NewWriter(&output)
	_, applyError := Apply(
		loaded,
		fixture.root,
		nil,
		result,
		report,
	)
	if flushError := report.Flush(); flushError != nil {
		test.Fatalf("writing the report: %v", flushError)
	}
	if applyError == nil || !strings.Contains(applyError.Error(), "nothing was written") {
		test.Fatalf("want a load refusal, got %v\n%s", applyError, output.String())
	}
	if got := fixture.read(test, "internal/a/a.go"); got != source {
		test.Fatalf("a.go was written despite the failed load:\n%s", got)
	}
}

// A reflect call reads a Go name only where its package reads one through reflect. Here package a asks reflect
// only whether a value is nil, so a's field is renamed; package b reads a field's name, so b's is refused.
func TestReflectWithoutNamesLeavesFieldsRenamable(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "reflect"

type Record struct{ GroupID *string }

func isNil(value any) bool {
	reflected := reflect.ValueOf(value)
	return reflected.Kind() == reflect.Pointer && reflected.IsNil()
}

func Empty(record Record) bool { return isNil(record) || isNil(record.GroupID) }
`,
		"internal/b/b.go": `package b

import "reflect"

type Record struct{ OwnerID string }

func FirstField(record Record) string { return reflect.TypeOf(record).Field(0).Name }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
		target{PackageSuffix: "internal/b", Name: "OwnerID", Kind: KindField, NewName: "OwnerId"},
	), Options{})
	requireRefused(test, result, "OwnerID", "encoding guard")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(
		test,
		"internal/a/a.go",
	); !strings.Contains(source, "type Record struct{ GroupId *string }") {
		test.Fatalf("a.go not renamed:\n%s", source)
	}
}

// A reflect.Type handed to another package may have its names read there, out of sight, so the package that
// hands it on counts as reading names.
func TestReflectHandedToAnotherPackageReadsNames(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/describe/describe.go": `package describe

import "reflect"

func Name(goType reflect.Type) string { return goType.Field(0).Name }
`,
		"internal/a/a.go": `package a

import (
	"reflect"

	"fixture/internal/describe"
)

type Record struct{ GroupID string }

func First(record Record) string { return describe.Name(reflect.TypeOf(record)) }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{})
	requireRefused(test, result, "GroupID", "encoding guard")
}

// A literal a person reviewed in one file no longer refuses the rename; the same literal in a file nobody
// reviewed still does, and a review that clears nothing is reported. Both files read Go names through reflect
// and see package a, so their literals could name the field.
func TestReviewedLiteralClearsOnlyItsFile(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"internal/a/a.go": `package a

type Record struct {
	URL    string
	UserID string
}

func Read(record Record) string { return record.URL + record.UserID }
`,
		"internal/words/words.go": `package words

import (
	"reflect"

	"fixture/internal/a"
)

var Allowed = map[string]bool{"URL": true}

func Describe(record a.Record) string { return a.Read(record) + reflect.TypeOf(0).Name() }
`,
	}
	reviews := map[string]map[string]string{
		"URL":    {"internal/words/words.go": "a word in a list"},
		"UserID": {"internal/words/words.go": "reviewed, but nothing spells it there"},
	}
	fixture := newFixture(test, files)
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "URL", Kind: KindField, NewName: "Url"},
	), Options{ReviewedLiterals: reviews})
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/a/a.go"); !strings.Contains(source, "record.Url") {
		test.Fatalf("a.go not renamed:\n%s", source)
	}

	files["internal/other/other.go"] = `package other

import (
	"reflect"

	"fixture/internal/a"
)

func Key(record a.Record) string { return "URL" + a.Read(record) + reflect.TypeOf(0).Name() }
`
	fixture = newFixture(test, files)
	loaded = fixture.load(test)
	result = Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "URL", Kind: KindField, NewName: "Url"},
		target{PackageSuffix: "internal/a", Name: "UserID", Kind: KindField, NewName: "UserId"},
	), Options{ReviewedLiterals: reviews})
	requireRefused(test, result, "URL", "string literal")
	for _, rejection := range result.Rejections {
		if rejection.OldName == "URL" && (!strings.Contains(rejection.Reason, "internal/other/other.go") ||
			strings.Contains(rejection.Reason, "words.go")) {
			test.Fatalf("the refusal should name only the unreviewed file: %s", rejection.Reason)
		}
	}
	if !strings.Contains(
		strings.Join(result.Notes, "\n"),
		`reviewed literal "UserID" in internal/words/words.go cleared nothing`,
	) {
		test.Fatalf("an unused review was not reported: %v", result.Notes)
	}
}

// A test go test is told to run by a pattern that spells its name keeps the name; a test nothing names by a
// string is renamed.
func TestTestRunByNameRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func Arguments() []string { return []string{"test", "-run=^TestRuleCPUBudget$", "./internal/a"} }
`,
		"internal/a/a_test.go": `package a

import "testing"

func TestRuleCPUBudget(test *testing.T) { test.Log(Arguments()) }

func TestParseURL(test *testing.T) { test.Log("parses") }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "TestRuleCPUBudget", Kind: KindFunc, NewName: "TestRuleCpuBudget"},
		target{PackageSuffix: "internal/a", Name: "TestParseURL", Kind: KindFunc, NewName: "TestParseUrl"},
	), Options{})
	requireRefused(test, result, "TestRuleCPUBudget", "string literal")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(
		test,
		"internal/a/a_test.go",
	); !strings.Contains(source, "func TestParseUrl(") || !strings.Contains(source, "func TestRuleCPUBudget(") {
		test.Fatalf("a_test.go:\n%s", source)
	}
}

// A rename that would carry a line past 120 columns is refused, and one that keeps every line inside is applied;
// a line gofmt only realigns, beside a field whose name grew, blames that field.
func TestRenameMakingALineLongRefused(test *testing.T) {
	test.Parallel()
	long := strings.Repeat("x", 92)
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func Read(memoryFS string) string {
	return memoryFS + "` + long + `"
}

type Options struct {
	FS       string
	Label    string // ` + strings.Repeat("y", 95) + `
	recordFS int
}

func Short(listingFS int) int {
	return listingFS
}

func Wide(widthFS int) int {
	return widthFS + len("` + strings.Repeat("z", 120) + `")
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "memoryFS", Kind: KindVariable, NewName: "memoryFileSystem"},
		target{PackageSuffix: "internal/a", Name: "recordFS", Kind: KindField, NewName: "recordFileSystem"},
		target{PackageSuffix: "internal/a", Name: "listingFS", Kind: KindVariable, NewName: "listingFileSystem"},
		target{PackageSuffix: "internal/a", Name: "widthFS", Kind: KindVariable, NewName: "widthFileSystem"},
	), Options{LimitLineLength: true})
	requireRefused(test, result, "memoryFS", "line length")
	requireRefused(test, result, "recordFS", "line length")
	// widthFS's line was long before the rename, so the rename makes no new long line.
	requireAccepted(test, result, 2)
	applyClean(test, fixture, loaded, result)
	longLines := 0
	for _, line := range strings.Split(fixture.read(test, "internal/a/a.go"), "\n") {
		if lineWidth(line) > lineLimit {
			longLines++
		}
	}
	if longLines != 1 {
		test.Fatalf("%d long lines after the rename, want the one that was long before", longLines)
	}
}

// Spelling FS out must not take a name already declared: MemoryFS cannot become the MemoryFileSystem beside it.
func TestFileSystemNameAlreadyTakenRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type MemoryFS struct{}

type MemoryFileSystem struct{}

type RecordingFS struct{}

func Use() (MemoryFS, MemoryFileSystem, RecordingFS) { return MemoryFS{}, MemoryFileSystem{}, RecordingFS{} }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "MemoryFS", Kind: KindType, NewName: "MemoryFileSystem"},
		target{PackageSuffix: "internal/a", Name: "RecordingFS", Kind: KindType, NewName: "RecordingFileSystem"},
	), Options{})
	requireRefused(test, result, "MemoryFS", "conflict")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
}

// A constant declared in a function exports nothing, so its capital may go; a package-level one keeps it.
func TestLocalConstantMayLoseItsCapital(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

const PS = " "

func Separators() string {
	const LS = " "
	return LS + PS
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "LS", Kind: KindConst, NewName: "lineSeparator"},
		target{PackageSuffix: "internal/a", Name: "PS", Kind: KindConst, NewName: "paragraphSeparator"},
	), Options{})
	requireRefused(test, result, "PS", "name")
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
	if source := fixture.read(test, "internal/a/a.go"); !strings.Contains(source, "return lineSeparator + PS") {
		test.Fatalf("a.go:\n%s", source)
	}
}

// Without LimitLineLength, the default, a rename is taken whatever its line's length, for the wrap to settle.
func TestLineLengthIsNotJudgedByDefault(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

func Read(memoryFS string) string {
	return memoryFS + "` + strings.Repeat("x", 92) + `"
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(
		test,
		loaded,
		target{PackageSuffix: "internal/a", Name: "memoryFS", Kind: KindVariable, NewName: "memoryFileSystem"},
	), Options{})
	requireAccepted(test, result, 1)
	applyClean(test, fixture, loaded, result)
}

// A string literal refuses a rename only where something reading Go names at run time can reach the declaration.
// "t", "T" and the dotted "div.p" as CSS do not, nor "props" there, nor "u" and "Count" in a test that reads no
// source, nor "w" in a reflect reader that cannot see package a, nor "Limit" in one that can, since reflect reads
// no constant. "v" in the reflect reader that sees package a does; "size" anywhere does, since Probed reaches
// reflect and reflect may call its method by name; "*a.config" does, the shape %T prints; and "Sum" in a test that
// reads a.go does. Probed's field is refused by the encoding guard whatever the literals say.
func TestStringLiteralRefusesOnlyWhereANameReaderCanReachTheName(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "fixture/internal/reader"

type Record struct {
	t int
	p int
	u int
	v int
	w int
	T int
}

func Sum(record Record) int { return record.t + record.p + record.u + record.v + record.w + record.T }

type Probed struct{ q int }

func (probed Probed) size() int { return probed.q }

func Probe(probed Probed) bool { return reader.Has(probed, "other") && probed.size() > 0 }

type config struct{ depth int }

type props struct{ depth int }

const Limit = 3

func Count() int { return config{}.depth + props{}.depth + Limit }
`,
		"internal/a/a_test.go": `package a_test

import "testing"

func TestName(test *testing.T) { test.Log("u", "Count") }
`,
		"internal/a/source_test.go": `package a_test

import (
	"os"
	"strings"
	"testing"
)

func TestSumIsDeclared(test *testing.T) {
	source, readError := os.ReadFile("a.go")
	if readError != nil || !strings.Contains(string(source), "Sum") {
		test.Fatal("a.go declares no Sum")
	}
}
`,
		"internal/css/css.go": `package css

var Units = []string{"t", "T", "div.p", "size", "props", "*a.config"}
`,
		"internal/reader/reader.go": `package reader

import "reflect"

// Default is the field Has looks for when asked for none.
const Default = "w"

func Has(value any, name string) bool {
	_, found := reflect.TypeOf(value).FieldByName(name)
	return found
}
`,
		"internal/inspect/inspect.go": `package inspect

import (
	"reflect"

	"fixture/internal/a"
)

// Fields are the names Describe is asked about.
var Fields = []string{"v", "Limit"}

func Describe(value any) string { return reflect.TypeOf(value).Name() + reflect.TypeOf(a.Count()).Name() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "t", Kind: KindField, NewName: "total"},
		target{PackageSuffix: "internal/a", Name: "p", Kind: KindField, NewName: "part"},
		target{PackageSuffix: "internal/a", Name: "u", Kind: KindField, NewName: "unit"},
		target{PackageSuffix: "internal/a", Name: "v", Kind: KindField, NewName: "value"},
		target{PackageSuffix: "internal/a", Name: "w", Kind: KindField, NewName: "width"},
		target{PackageSuffix: "internal/a", Name: "T", Kind: KindField, NewName: "Total"},
		target{PackageSuffix: "internal/a", Name: "q", Kind: KindField, NewName: "quantity"},
		target{PackageSuffix: "internal/a", Name: "size", Kind: KindMethod, NewName: "measure"},
		target{PackageSuffix: "internal/a", Name: "config", Kind: KindType, NewName: "configuration"},
		target{PackageSuffix: "internal/a", Name: "props", Kind: KindType, NewName: "properties"},
		target{PackageSuffix: "internal/a", Name: "Limit", Kind: KindConst, NewName: "Ceiling"},
		target{PackageSuffix: "internal/a", Name: "Sum", Kind: KindFunc, NewName: "Add"},
		target{PackageSuffix: "internal/a", Name: "Count", Kind: KindFunc, NewName: "Tally"},
	), Options{})
	requireRefused(test, result, "v", "string literal")
	requireRefused(test, result, "q", "encoding guard")
	requireRefused(test, result, "size", "string literal")
	requireRefused(test, result, "config", "string literal")
	requireRefused(test, result, "Sum", "string literal")
	requireAccepted(test, result, 8)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	want := `package a

import "fixture/internal/reader"

type Record struct {
	total int
	part  int
	unit  int
	v     int
	width int
	Total int
}

func Sum(record Record) int {
	return record.total + record.part + record.unit + record.v + record.width + record.Total
}

type Probed struct{ q int }

func (probed Probed) size() int { return probed.q }

func Probe(probed Probed) bool { return reader.Has(probed, "other") && probed.size() > 0 }

type config struct{ depth int }

type properties struct{ depth int }

const Ceiling = 3

func Tally() int { return config{}.depth + properties{}.depth + Ceiling }
`
	if got := fixture.read(test, "internal/a/a.go"); got != want {
		test.Fatalf("internal/a/a.go:\n%s\nwant:\n%s", got, want)
	}
}

// An implementation's method may be renamed with the interface of another workspace package that requires it,
// when that interface's own method takes the same new name: Graph.Params and node.Params become Parameters
// together. Without the interface's rename, here a library's export nobody pinned, the implementation keeps the
// name the interface forces; and a method sort.Interface requires is never renamed.
func TestMethodRenamedWithTheInterfaceThatRequiresIt(test *testing.T) {
	test.Parallel()
	files := map[string]string{
		"graph/graph.go": `package graph

type Graph interface{ Params() []int }

func Count(graph Graph) int { return len(graph.Params()) }
`,
		"internal/a/a.go": `package a

import (
	"sort"

	"fixture/graph"
)

type node struct{}

func (node) Params() []int { return nil }

type list []int

func (values list) Len() int                    { return len(values) }
func (values list) Less(left, right int) bool { return values[left] < values[right] }
func (values list) Swap(left, right int)      { values[left], values[right] = values[right], values[left] }

func Run() int {
	sort.Sort(list{2, 1})
	return graph.Count(node{})
}
`,
	}
	targets := func(loaded *workspace.Workspace) []Proposal {
		return propose(test, loaded,
			target{PackageSuffix: "graph", Name: "Params", Kind: KindMethod, Owner: "Graph", NewName: "Parameters"},
			target{PackageSuffix: "internal/a", Name: "Params", Kind: KindMethod, Owner: "node", NewName: "Parameters"},
			target{PackageSuffix: "internal/a", Name: "Len", Kind: KindMethod, Owner: "list", NewName: "Length"},
		)
	}
	fixture := newFixture(test, files)
	loaded := fixture.load(test)
	result := Plan(loaded, targets(loaded), Options{})
	requireRefused(test, result, "Params", "exported outside the workspace")
	requireRefused(test, result, "Len", "implements an interface of another package")
	heldByInterface := false
	for _, rejection := range result.Rejections {
		if rejection.OldName == "Params" && strings.HasSuffix(rejection.Position.Filename, "internal/a/a.go") {
			heldByInterface = rejection.Class == "implements an interface of another package"
		}
	}
	if !heldByInterface {
		test.Fatalf("node.Params not held by the interface; refusals: %+v", result.Rejections)
	}
	requireAccepted(test, result, 0)

	fixture = newFixture(test, files)
	loaded = fixture.load(test)
	result = Plan(
		loaded,
		targets(loaded),
		Options{PinnedLibraries: map[string]string{"fixture/graph": "read at a pin"}},
	)
	requireRefused(test, result, "Len", "implements an interface of another package")
	requireAccepted(test, result, 2)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	if got := fixture.read(test, "graph/graph.go"); !strings.Contains(got, "Graph interface{ Parameters() []int }") ||
		!strings.Contains(got, "graph.Parameters()") {
		test.Fatalf("graph/graph.go not renamed:\n%s", got)
	}
	if got := fixture.read(test, "internal/a/a.go"); !strings.Contains(got, "func (node) Parameters() []int") ||
		!strings.Contains(got, "func (values list) Len() int") {
		test.Fatalf("internal/a/a.go not renamed as wanted:\n%s", got)
	}
}

// A type may take the name of a local that reads it before the local's scope opens: config's local
// configuration := &config{} and props' parameter properties props. A read of the type inside the local's scope
// still refuses it: opts' second read sits after options := opts{}.
func TestTypeTakesTheNameOfALocalThatReadsIt(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

type config struct{ depth int }

func newConfig() *config {
	configuration := &config{depth: 1}
	return configuration
}

type props struct{ depth int }

func read(properties props) int { return properties.depth }

type opts struct{ depth int }

func later() int {
	options := opts{}
	var other opts
	return options.depth + other.depth
}

func Use() int { return newConfig().depth + read(props{}) + later() }
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "config", Kind: KindType, NewName: "configuration"},
		target{PackageSuffix: "internal/a", Name: "props", Kind: KindType, NewName: "properties"},
		target{PackageSuffix: "internal/a", Name: "opts", Kind: KindType, NewName: "options"},
	), Options{})
	requireRefused(test, result, "opts", "conflict: scope")
	requireAccepted(test, result, 2)
	requireModelClean(test, result)
	applyClean(test, fixture, loaded, result)
	want := `package a

type configuration struct{ depth int }

func newConfig() *configuration {
	configuration := &configuration{depth: 1}
	return configuration
}

type properties struct{ depth int }

func read(properties properties) int { return properties.depth }

type opts struct{ depth int }

func later() int {
	options := opts{}
	var other opts
	return options.depth + other.depth
}

func Use() int { return newConfig().depth + read(properties{}) + later() }
`
	if got := fixture.read(test, "internal/a/a.go"); got != want {
		test.Fatalf("internal/a/a.go:\n%s\nwant:\n%s", got, want)
	}
}

// typeNameCase is one shape of code a type's values reach, and whether it reads the type's name.
type typeNameCase struct {
	name    string
	source  string
	refused bool
}

// A type's name is read only by what reads a type's name: reflect.Type's Name or String, reflect values leaving
// for code the workspace does not hold, fmt's %T and %#v, gob.Register. Walking a type's fields through reflect,
// even handing the fields' types out of the package, and encoding it to json or gob, read no type's name.
func TestTypeNameGuardRefusesOnlyWhatReadsATypesName(test *testing.T) {
	test.Parallel()
	cases := []typeNameCase{
		{name: "fields walked, their types handed out", refused: false, source: `package a

import (
	"encoding/gob"
	"encoding/json"
	"io"
	"reflect"
)

type rawConfig struct {
	Rules map[string]int ` + "`json:\"rules\"`" + `
}

type Field struct {
	Name string
	Type reflect.Type
}

// Fields lists the struct's fields by their tags.
func Fields() []Field {
	var fields []Field
	structType := reflect.TypeOf(rawConfig{})
	for index := 0; index < structType.NumField(); index++ {
		field := structType.Field(index)
		fields = append(fields, Field{Name: field.Tag.Get("json"), Type: field.Type})
	}
	return fields
}

func Encode(writer io.Writer) error {
	if encodeError := json.NewEncoder(writer).Encode(rawConfig{}); encodeError != nil {
		return encodeError
	}
	return gob.NewEncoder(writer).Encode(rawConfig{})
}
`},
		{name: "reflect.Type's Name", refused: true, source: `package a

import "reflect"

type rawConfig struct{ Rules int }

func Name() string { return describe(reflect.TypeOf(rawConfig{})) }

func describe(goType reflect.Type) string {
	for goType.Kind() == reflect.Pointer {
		goType = goType.Elem()
	}
	return goType.Name()
}
`},
		{name: "reflect.TypeFor's String", refused: true, source: `package a

import "reflect"

type rawConfig struct{ Rules int }

func Name() string { return reflect.TypeFor[*rawConfig]().String() }
`},
		{name: "a reflect.Type printed", refused: true, source: `package a

import (
	"fmt"
	"reflect"
)

type rawConfig struct{ Rules int }

func Name() string { return fmt.Sprint(reflect.TypeOf(rawConfig{})) }
`},
		{name: "a reflect.Type kept in a map and read later", refused: true, source: `package a

import "reflect"

type rawConfig struct{ Rules int }

func Names() []string {
	seen := map[reflect.Type]bool{}
	seen[reflect.TypeOf(rawConfig{})] = true
	var names []string
	for goType := range seen {
		names = append(names, goType.Name())
	}
	return names
}
`},
		{name: "fmt's %T", refused: true, source: `package a

import "fmt"

type rawConfig struct{ Rules int }

func Describe() error { return fmt.Errorf("cannot use %d of %T", 1, rawConfig{}) }
`},
		{name: "fmt's %#v through an interface", refused: true, source: `package a

import "fmt"

type rawConfig struct{ Rules int }

func Describe() string {
	var held any = rawConfig{}
	return fmt.Sprintf("%#v", held)
}
`},
		{name: "fmt's %v and %+v", refused: false, source: `package a

import "fmt"

type rawConfig struct{ Rules int }

func Describe() string { return fmt.Sprintf("%v %+v %T", rawConfig{}, rawConfig{}, 1) }
`},
		{name: "a format that is no constant", refused: true, source: `package a

import "fmt"

type rawConfig struct{ Rules int }

func describe(format string, arguments ...any) string { return fmt.Sprintf(format, arguments...) }

func Describe() string { return describe("%v", rawConfig{}) }
`},
		{name: "gob.Register", refused: true, source: `package a

import "encoding/gob"

type rawConfig struct{ Rules int }

func init() { gob.Register(rawConfig{}) }
`},
	}
	for _, each := range cases {
		fixture := newFixture(test, map[string]string{"internal/a/a.go": each.source})
		loaded := fixture.load(test)
		result := Plan(loaded, propose(
			test,
			loaded,
			target{PackageSuffix: "internal/a", Name: "rawConfig", Kind: KindType, NewName: "rawConfiguration"},
		), Options{})
		if !each.refused {
			if len(result.Accepted) != 1 {
				test.Fatalf("%s: refused, want renamed: %+v", each.name, result.Rejections)
			}
			applyClean(test, fixture, loaded, result)
			continue
		}
		if len(result.Accepted) != 0 {
			test.Fatalf("%s: renamed, want refused", each.name)
		}
		requireRefused(test, result, "rawConfig", "encoding guard")
	}
}

// A reflect value of a type a value holds, reached through Field, reads the held type's name and not the holder's:
// the holder is renamed and the held type keeps its name.
func TestHeldTypeNameReadRefusesOnlyTheHeldType(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "reflect"

type ruleValue struct{ Level int }

type rawConfig struct{ Rules []ruleValue }

func FieldTypeNames() []string {
	structType := reflect.TypeOf(rawConfig{})
	var names []string
	for index := range structType.NumField() {
		names = append(names, structType.Field(index).Type.Elem().Name())
	}
	return names
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "rawConfig", Kind: KindType, NewName: "rawConfiguration"},
		target{PackageSuffix: "internal/a", Name: "ruleValue", Kind: KindType, NewName: "ruleSetting"},
	), Options{})
	requireAccepted(test, result, 1)
	if result.Accepted[0].OldName != "rawConfig" {
		test.Fatalf("accepted %+v, want the holder", result.Accepted)
	}
	requireRefused(test, result, "ruleValue", "encoding guard")
	applyClean(test, fixture, loaded, result)
}

func TestFormatTypeNameOperands(test *testing.T) {
	test.Parallel()
	for format, want := range map[string]map[int]encoder{
		"%d of %T":        {1: encoderFormatType},
		"%#v and %v":      {0: encoderFormatGoSyntax},
		"%*d %T":          {2: encoderFormatType},
		"%[2]T %[1]v":     {1: encoderFormatType},
		"100%% %T":        {0: encoderFormatType},
		"%-8s|%+v|%x":     {},
		"%.*f then %#v":   {2: encoderFormatGoSyntax},
		"%[1]T is %#[1]v": {0: encoderFormatType | encoderFormatGoSyntax},
	} {
		got := formatTypeNameOperands(format)
		if len(got) != len(want) {
			test.Fatalf("%q: got %v, want %v", format, got, want)
		}
		for operand, set := range want {
			if got[operand] != set {
				test.Fatalf("%q: got %v, want %v", format, got, want)
			}
		}
	}
}
