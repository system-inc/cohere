package global_rename

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_rename/workspace"

	"golang.org/x/tools/go/packages"
)

// loadElsewhere loads the fixture with go/packages itself and gives FromLoaded its roots and analyzed variants as
// the Go engine's load chooses them (internal/go_engine/load's analyzedOfGraph), here by ForTest, where Load
// chooses by ID.
func (fixture fixture) loadElsewhere(test *testing.T) *workspace.Workspace {
	test.Helper()
	root, resolveError := filepath.EvalSymlinks(fixture.root)
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	configuration := &packages.Config{
		Dir:   root,
		Tests: true,
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedForTest,
	}
	loaded, loadError := packages.Load(configuration, "./...")
	if loadError != nil {
		test.Fatalf("loading: %v", loadError)
	}
	isTestMain := func(candidate *packages.Package) bool {
		if candidate.ForTest != "" {
			return false
		}
		for _, imported := range candidate.Imports {
			if imported.ForTest != "" {
				return true
			}
		}
		return false
	}
	withTestVariant := map[string]bool{}
	for _, loadedPackage := range loaded {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("%s: %v", loadedPackage.ID, loadedPackage.Errors)
		}
		if loadedPackage.ForTest != "" && loadedPackage.PkgPath == loadedPackage.ForTest {
			withTestVariant[loadedPackage.PkgPath] = true
		}
	}
	var roots []*packages.Package
	var analyzed []*packages.Package
	for _, loadedPackage := range loaded {
		if isTestMain(loadedPackage) {
			continue
		}
		roots = append(roots, loadedPackage)
		switch {
		case loadedPackage.ForTest != "":
			if loadedPackage.PkgPath == loadedPackage.ForTest ||
				loadedPackage.PkgPath == loadedPackage.ForTest+"_test" {
				analyzed = append(analyzed, loadedPackage)
			}
		case !withTestVariant[loadedPackage.PkgPath]:
			analyzed = append(analyzed, loadedPackage)
		}
	}
	return workspace.FromLoaded(root, roots, analyzed, nil)
}

// A plan over packages loaded elsewhere, as the Go engine's load gives them, is the plan over Load's: the same
// renames accepted and refused for the same reasons, with the same edits, across a package, its in-package and
// external tests, an importer, an interface renamed with its implementation, and a name a string literal spells.
func TestPlanOverFromLoadedIsThePlanOverLoad(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import "reflect"

// Fetcher fetches.
type Fetcher interface{ FetchURL() string }

type page struct{}

func (page) FetchURL() string { return "" }

// ParseURL parses.
func ParseURL(text string) string { return text }

// Holder holds.
type Holder struct{ OwnerID string }

func Owner(holder Holder) string { return reflect.ValueOf(holder).FieldByName("OwnerID").String() }

func Run() string { return ParseURL(Fetcher(page{}).FetchURL()) }
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

func Owner(holder a.Holder) string { return holder.OwnerID }
`,
	})
	targets := []target{
		{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunction, NewName: "ParseUrl"},
		{PackageSuffix: "internal/a", Name: "FetchURL", Kind: KindMethod, Owner: "Fetcher", NewName: "FetchUrl"},
		{PackageSuffix: "internal/a", Name: "OwnerID", Kind: KindField, NewName: "OwnerId"},
	}
	options := Options{RenameImplementations: true}
	loaded := fixture.load(test)
	overLoad := Plan(loaded, propose(test, loaded, targets...), options)
	elsewhere := fixture.loadElsewhere(test)
	if len(elsewhere.Roots) != len(loaded.Roots) || len(elsewhere.Analyzed) != len(loaded.Analyzed) {
		test.Fatalf(
			"FromLoaded holds %d roots and %d analyzed, Load %d and %d",
			len(elsewhere.Roots),
			len(elsewhere.Analyzed),
			len(loaded.Roots),
			len(loaded.Analyzed),
		)
	}
	overFromLoaded := Plan(elsewhere, propose(test, elsewhere, targets...), options)
	requireAccepted(test, overLoad, 3)
	requireRefused(test, overLoad, "OwnerID", "string literal")
	requireModelClean(test, overLoad)
	if len(overLoad.Edits) == 0 {
		test.Fatal("the plan over Load has no edits to compare")
	}
	if !reflect.DeepEqual(overLoad, overFromLoaded) {
		test.Fatalf("the plans differ:\nover Load:       %+v\nover FromLoaded: %+v", overLoad, overFromLoaded)
	}
}

// writable, given to FromLoaded, is the caller's word on what a rename may write: a rename used in a file it refuses
// is refused as one used in a file that may not be edited, and the same rename over the same load without it is
// accepted.
func TestFromLoadedRefusesARenameUsedInAFileTheCallerCannotWrite(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": "package a\n\nfunc ParseURL(text string) string { return text }\n",
		"internal/b/b.go": "package b\n\nimport \"fixture/internal/a\"\n\nvar Parsed = a.ParseURL(\"x\")\n",
	})
	loaded := fixture.load(test)
	outOfScope := filepath.Join(loaded.Root, "internal", "b", "b.go")
	writable := func(filename string) bool { return filename != outOfScope }
	parseUrl := target{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunction, NewName: "ParseUrl"}

	scoped := workspace.FromLoaded(loaded.Root, loaded.Roots, loaded.Analyzed, writable)
	requireRefused(test, Plan(scoped, propose(test, scoped, parseUrl), Options{}), "ParseURL", "file")
	whole := workspace.FromLoaded(loaded.Root, loaded.Roots, loaded.Analyzed, nil)
	requireAccepted(test, Plan(whole, propose(test, whole, parseUrl), Options{}), 1)
}

// A HostOnly plan loads no other platform and refuses a rename that reaches a package holding a file the host does
// not build, which only that platform's load could prove the rename against, while a rename reaching no such package
// is planned as before.
func TestAHostOnlyPlanRefusesARenameReachingAFileTheHostDoesNotBuild(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go":       "package a\n\nfunc ParseURL(text string) string { return text }\n",
		"internal/a/a_plan9.go": "package a\n\nfunc plan9Only() string { return ParseURL(\"\") }\n",
		"internal/b/b.go": "package b\n\nimport \"fixture/internal/a\"\n\nvar Parsed = a.ParseURL(\"x\")\n\n" +
			"func FormatURL() string { return \"\" }\n",
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "ParseURL", Kind: KindFunction, NewName: "ParseUrl"},
		target{PackageSuffix: "internal/b", Name: "FormatURL", Kind: KindFunction, NewName: "FormatUrl"},
	), Options{HostOnly: true})
	requireRefused(test, result, "ParseURL", "platform")
	requireAccepted(test, result, 1)
	if len(result.platforms) != 0 {
		test.Fatalf("a HostOnly plan loaded the platforms %v", result.platforms)
	}
}

// A plan made over a load that read a rewrite from memory, as the engine's second round of renames is, rewrites each
// file from those bytes: the first round's GroupId becomes GroupKey in both files. Rewritten from disk instead, the
// edits find GroupID where they expect GroupId and the rewrite is refused.
func TestRewrittenFilesOverRewritesFromTheBytesInMemory(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": "package a\n\ntype Record struct{ GroupID string }\n",
		"internal/b/b.go": "package b\n\nimport \"fixture/internal/a\"\n\n" +
			"func Group(record a.Record) string { return record.GroupID }\n",
	})
	loaded := fixture.load(test)
	first := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "GroupID", Kind: KindField, NewName: "GroupId"},
	), Options{})
	firstFiles, firstError := RewrittenFiles(first)
	if firstError != nil {
		test.Fatal(firstError)
	}
	overlaid, overlaidError := workspace.LoadWithOverlay(fixture.root, nil, firstFiles)
	if overlaidError != nil {
		test.Fatal(overlaidError)
	}
	second := Plan(overlaid, propose(test, overlaid,
		target{PackageSuffix: "internal/a", Name: "GroupId", Kind: KindField, NewName: "GroupKey"},
	), Options{})
	requireAccepted(test, second, 1)
	secondFiles, secondError := RewrittenFilesOver(second, firstFiles)
	if secondError != nil {
		test.Fatal(secondError)
	}
	if len(secondFiles) != 2 {
		test.Fatalf("want both files rewritten, got %d", len(secondFiles))
	}
	for filename, contents := range secondFiles {
		if !strings.Contains(string(contents), "GroupKey") || strings.Contains(string(contents), "GroupI") {
			test.Errorf("%s reads\n%s", filename, contents)
		}
	}
	if _, fromDiskError := RewrittenFiles(second); fromDiskError == nil {
		test.Fatal("want the rewrite from disk refused, since disk still holds GroupID")
	}
}

// A method a template calls by name keeps it: the template's text spells it after a dot and before the action's close,
// "{{.TitleHTML}}", which the string-literal guard reads as the member the dot selects, so the rename is refused rather
// than leaving the template calling a method that is gone.
func TestAMethodATemplateCallsByNameIsRefused(test *testing.T) {
	test.Parallel()
	fixture := newFixture(test, map[string]string{
		"internal/a/a.go": `package a

import (
	"strings"
	"text/template"
)

type view struct{}

func (view) TitleHTML() string { return "title" }

func Render() string {
	var rendered strings.Builder
	_ = template.Must(template.New("page").Parse("{{.TitleHTML}}")).Execute(&rendered, view{})
	return rendered.String()
}
`,
	})
	loaded := fixture.load(test)
	result := Plan(loaded, propose(test, loaded,
		target{PackageSuffix: "internal/a", Name: "TitleHTML", Kind: KindMethod, Owner: "view", NewName: "TitleHtml"},
	), Options{HostOnly: true})
	requireRefused(test, result, "TitleHTML", "string literal")
}
