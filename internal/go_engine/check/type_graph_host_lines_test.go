package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/export_data"
)

// The host lines fixture's paths: platform declares types whose layout differs by target, restored declares types,
// over platform's or of its own, which the build restores from the host, and reader is a package of the host's whose
// reads a build judges.
const (
	hostLinesPlatformPath = "example.com/hostlines/platform"
	hostLinesRestoredPath = "example.com/hostlines/restored"
	hostLinesReaderPath   = "example.com/hostlines/reader"
)

// hostLinesPlatform is platform's source on a target whose word is the integer named, int32 on the host's and int64 in
// a build whose platform differs: X's field, Int's underlying type and Handle's field each follow it, Handle's
// exported, so only its node's own lines tell the two apart.
func hostLinesPlatform(word string) string {
	return "package platform\n\ntype X struct {\n\tF " + word + "\n\tY Other\n}\n\n" +
		"type Other struct{ Name string }\n\ntype Int " + word + "\n\n" +
		"type Handle struct{ Descriptor " + word + " }\n\n" +
		"func (handle Handle) Close() error { return nil }\n"
}

// hostLinesOver is restored's source over platform: defined types declared over platform's (T and U), a struct
// holding platform's Handle in an unexported field, which renders by its properties, a recursive struct of its own
// with an unexported method no importer sees, and an alias of platform's Handle.
const hostLinesOver = `package restored

import "example.com/hostlines/platform"

type T platform.X

type U platform.Int

type Holder struct {
	held  platform.Handle
	Count int
}

func (holder *Holder) Close() error { return holder.held.Close() }

type Plain struct {
	Name string
	Next *Plain
}

func (plain Plain) describe() string { return plain.Name }

type Tied = platform.Handle
`

// hostLinesAlone is restored's source reading nothing of platform: a recursive struct of its own.
const hostLinesAlone = `package restored

type Plain struct {
	Name string
	Next *Plain
}
`

// hostLinesReaders are reader's sources by what each reads whole: restored's Plain, which reads alike in every build,
// and platform's Handle, which reads otherwise in a build whose platform differs.
var hostLinesReaders = map[string]string{
	"plain":  "package reader\n\nimport \"example.com/hostlines/restored\"\n\nvar Kept restored.Plain\n",
	"handle": "package reader\n\nimport \"example.com/hostlines/platform\"\n\nvar Kept platform.Handle\n",
}

// hostLinesChecked is source checked from source as the package at path over imports, by path, its positions in
// fileSet, and its reads, recorded from its type information and syntax as the run records them.
func hostLinesChecked(
	test *testing.T,
	fileSet *token.FileSet,
	path string,
	source string,
	imports map[string]*types.Package,
) (*types.Package, Restoring) {
	test.Helper()

	file, parseError := parser.ParseFile(fileSet, path+".go", source, parser.SkipObjectResolution|parser.ParseComments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Defs:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
		Implicits:  map[ast.Node]types.Object{},
		Instances:  map[*ast.Ident]types.Instance{},
	}
	configuration := types.Config{Importer: importerFunction(func(imported string) (*types.Package, error) {
		if found, isFound := imports[imported]; isFound {
			return found, nil
		}
		return nil, fmt.Errorf("no package %s", imported)
	})}
	checked, checkError := configuration.Check(path, fileSet, []*ast.File{file}, info)
	if checkError != nil {
		test.Fatalf("type-checking %s: %v", path, checkError)
	}
	reads := &build_reads.Reads{}
	reads.RecordTypes(checked, info, []*ast.File{file})
	importPaths := make([]string, 0, len(checked.Imports()))
	for _, imported := range checked.Imports() {
		importPaths = append(importPaths, imported.Path())
	}
	return checked, Restoring{Path: path, Imports: importPaths, Reads: reads}
}

// hostLinesUniverses are the fixture on the host, platform of int32 and restored of restoredSource checked from
// source, with restored's reads, and in the build, platform of buildWord checked from source on its own target and
// restored read from the export data the host's check of it wrote, against the build's platform, as the loader reads
// a package the build restores. Both by path.
func hostLinesUniverses(
	test *testing.T,
	buildWord string,
	restoredSource string,
) (map[string]*types.Package, map[string]*types.Package, Restoring) {
	test.Helper()

	fileSet := token.NewFileSet()
	hostPlatform, _ := hostLinesChecked(test, fileSet, hostLinesPlatformPath, hostLinesPlatform("int32"), nil)
	buildPlatform, _ := hostLinesChecked(test, fileSet, hostLinesPlatformPath, hostLinesPlatform(buildWord), nil)
	hostRestored, restoredReads := hostLinesChecked(test, fileSet, hostLinesRestoredPath, restoredSource,
		map[string]*types.Package{hostLinesPlatformPath: hostPlatform})
	exportData, writeError := writeExportData(&packages.Package{ID: hostLinesRestoredPath,
		PkgPath: hostLinesRestoredPath, Fset: fileSet, Types: hostRestored})
	if writeError != nil {
		test.Fatal(writeError)
	}
	closure := map[string]*types.Package{hostLinesPlatformPath: buildPlatform}
	buildRestored, readError := export_data.Read(token.NewFileSet(), closure, exportData, hostLinesRestoredPath)
	if readError != nil {
		test.Fatal(readError)
	}
	// Its imports are the build's of its host's, as the loader sets them.
	ownImports := make([]*types.Package, 0, len(hostRestored.Imports()))
	for _, imported := range hostRestored.Imports() {
		ownImports = append(ownImports, closure[imported.Path()])
	}
	buildRestored.SetImports(ownImports)
	host := map[string]*types.Package{hostLinesPlatformPath: hostPlatform, hostLinesRestoredPath: hostRestored}
	return host, closure, restoredReads
}

// hostLinesTypeName is the type name declared at name in checked.
func hostLinesTypeName(test *testing.T, checked *types.Package, name string) *types.TypeName {
	test.Helper()

	typeName, isTypeName := checked.Scope().Lookup(name).(*types.TypeName)
	if !isTypeName {
		test.Fatalf("%s declares no type name %s", checked.Path(), name)
	}
	return typeName
}

// A build that restored a package from the host, whose own verdict held, takes its nodes' lines from the host's graph
// and renders exactly as it would without: every object of the fixture digests alike, and every node the digests made
// holds the same lines and the same digest, Holder's observed line of its Handle field the build's own once. Every
// node of the restored package but its alias took the host's lines, and no node of platform did.
func TestARestoredPackageRendersAsTheBuildWouldWithTheHostsLines(test *testing.T) {
	test.Parallel()

	host, closure, restoredReads := hostLinesUniverses(test, "int32", hostLinesOver)
	if holds, why := TypesHold(restoredReads, closure, nil, measuredReadsBuild(), measuredReadsBuild()); !holds {
		test.Fatalf("the restored package's own verdict does not hold, so the build would never restore it: %s", why)
	}
	hostMemo, reusing, rendering := measuredReadsBuild(), measuredReadsBuild(), measuredReadsBuild()
	reusing.graph.learnRestored(hostMemo.graph,
		map[*types.Package]*types.Package{closure[hostLinesRestoredPath]: host[hostLinesRestoredPath]})
	for _, path := range []string{hostLinesPlatformPath, hostLinesRestoredPath} {
		labels, objects := agreementObjects(closure[path])
		for index, object := range objects {
			if reusing.closureDigest(object) != rendering.closureDigest(object) {
				test.Errorf("%s.%s digests apart with the host's lines", path, labels[index])
			}
		}
	}
	restoredNodes, nodes := 0, 0
	for declaring, part := range reusing.graph.packages {
		renderedPart := rendering.graph.packages[declaring]
		for typeName, node := range part.nodes {
			nodes++
			if declaring.Path() == hostLinesRestoredPath && !typeName.IsAlias() {
				restoredNodes++
			}
			rendered := renderedPart.nodes[typeName]
			if rendered == nil {
				test.Errorf("%s.%s has a node only with the host's lines", declaring.Path(), typeName.Name())
				continue
			}
			if !slices.Equal(node.lines, rendered.lines) || node.digest != rendered.digest ||
				node.digested != rendered.digested {
				test.Errorf("%s.%s renders apart with the host's lines:\nreused\n%s\nrendered\n%s", declaring.Path(),
					typeName.Name(), strings.Join(node.lines, "\n"), strings.Join(rendered.lines, "\n"))
			}
		}
	}
	reused, renderedCount, unmatched := reusing.nodeCounts()
	if reused == 0 || reused != restoredNodes || renderedCount != nodes-restoredNodes || unmatched != 0 {
		test.Errorf("want %d nodes reused, %d rendered and none unmatched, got %d, %d and %d", restoredNodes,
			nodes-restoredNodes, reused, renderedCount, unmatched)
	}
}

// A restored package's defined type declared over another package's, `type T platform.X`, has no edge to X, and read
// from the host's export data against a build whose platform differs it takes the build's X struct, so it renders
// apart from the host's T: its lines are the host's only because the package's own verdict held. That verdict reads
// X and Int whole, by the declarations' own reads, so a build whose platform differs declines the package, and it is
// never among the packages whose nodes take the host's lines.
func TestADefinedTypeOverAnotherPackagesIsPinnedByItsPackagesOwnVerdict(test *testing.T) {
	test.Parallel()

	host, closure, restoredReads := hostLinesUniverses(test, "int64", hostLinesOver)
	hostT := renderedTypeName(hostLinesTypeName(test, host[hostLinesRestoredPath], "T"), nil)
	buildT := renderedTypeName(hostLinesTypeName(test, closure[hostLinesRestoredPath], "T"), nil)
	if slices.Equal(hostT, buildT) {
		test.Errorf("the build's T, read from export data against its own platform, renders as the host's, so the "+
			"fixture tells nothing:\n%s", strings.Join(buildT, "\n"))
	}
	judged := verdict{host: measuredReadsBuild(), build: measuredReadsBuild(), closure: closureLookupOf(closure),
		methods: &verdictClosures{host: map[*types.Package]*gatheredClosure{}}}
	declined := map[string]string{}
	for _, use := range restoredReads.Reads.Uses() {
		if holds, why := judged.useHolds(use, false); !holds {
			declined[objectName(use.Object)+" "+string(use.Kind)] = why
		}
	}
	for _, name := range []string{"X", "Int"} {
		read := hostLinesPlatformPath + "." + name + " " + string(build_reads.UseWhole)
		if declined[read] == "" {
			test.Errorf("the restored package's whole use of platform's %s holds in the build, declined %v", name,
				declined)
		}
	}
	if holds, why := TypesHold(restoredReads, closure, nil, measuredReadsBuild(), measuredReadsBuild()); holds {
		test.Errorf("the restored package's own verdict holds in a build whose platform differs: %q", why)
	}
}

// Only a package the build restored from the host takes the host's lines. Platform, which the build checks from
// source on a target where it differs, renders its own, so a read of its Handle declines, while a read of restored's
// Plain, restored and reading nothing of platform, holds with the host's lines.
func TestOnlyARestoredPackageTakesTheHostsLines(test *testing.T) {
	test.Parallel()

	host, closure, restoredReads := hostLinesUniverses(test, "int64", hostLinesAlone)
	if holds, why := TypesHold(restoredReads, closure, nil, measuredReadsBuild(), measuredReadsBuild()); !holds {
		test.Fatalf("the restored package's own verdict does not hold, so the build would never restore it: %s", why)
	}
	hostMemo, build := measuredReadsBuild(), measuredReadsBuild()
	restored := map[*types.Package]*types.Package{closure[hostLinesRestoredPath]: host[hostLinesRestoredPath]}
	for _, step := range []struct {
		reader string
		want   string
	}{
		{"plain", ""},
		{"handle", "T object " + hostLinesPlatformPath + ".Handle renders differently"},
	} {
		_, restoring := hostLinesChecked(test, token.NewFileSet(), hostLinesReaderPath, hostLinesReaders[step.reader],
			host)
		holds, why := TypesHold(restoring, closure, restored, hostMemo, build)
		if holds != (step.want == "") || why != step.want {
			test.Errorf("reading %s, want %t, %q, got %t, %q", step.reader, step.want == "", step.want, holds, why)
		}
	}
	handle := build.graph.renderedNode(hostLinesTypeName(test, closure[hostLinesPlatformPath], "Handle"), amd64Sizes)
	hostHandle := hostMemo.graph.renderedNode(hostLinesTypeName(test, host[hostLinesPlatformPath], "Handle"),
		amd64Sizes)
	if slices.Equal(handle.lines, hostHandle.lines) {
		test.Errorf("the build's Handle, checked from source, renders as the host's:\n%s", strings.Join(handle.lines,
			"\n"))
	}
	if reused, _, _ := build.nodeCounts(); reused == 0 {
		test.Error("no node took the host's lines, so the fixture tells nothing")
	}
}

// hostLinesModule is a module whose platform declares X by target, int32 but on windows: p declares `type T
// platform.X` and reads X whole, so windows declines it at its load and checks it from source; c reads p's T whole,
// and is visited since p is; plain reads nothing of another package, so the walk leaves it untouched; and d reads
// plain's Box whole and platform's Double, declared alike on every target, so it is visited and holds, judged with
// plain's lines taken from the host.
func hostLinesModule() map[string]string {
	return map[string]string{
		"go.mod": "module example.com/hostlines\n\ngo 1.24\n",
		"platform/platform.go": "// Package platform is laid out by target.\npackage platform\n\n" +
			"// Double is twice value.\nfunc Double(value int) int {\n\treturn value * 2\n}\n",
		"platform/platform_other.go": "//go:build !windows\n\npackage platform\n\n// X is a word of the target.\n" +
			"type X struct{ F int32 }\n",
		"platform/platform_windows.go": "package platform\n\n// X is a word of the target.\ntype X struct{ F int64 }\n",
		"p/p.go": "// Package p declares over platform.\npackage p\n\nimport \"example.com/hostlines/platform\"\n\n" +
			"// T is platform's X.\ntype T platform.X\n",
		"c/c.go": "// Package c keeps a T.\npackage c\n\nimport \"example.com/hostlines/p\"\n\n// Kept is a T.\n" +
			"var Kept p.T\n",
		"plain/plain.go": "// Package plain declares a list.\npackage plain\n\n// Box is a list of counts.\n" +
			"type Box struct {\n\tCount int\n\tNext  *Box\n}\n",
		"d/d.go": "// Package d keeps a Box.\npackage d\n\nimport (\n\t\"example.com/hostlines/plain\"\n" +
			"\t\"example.com/hostlines/platform\"\n)\n\n// Kept is a Box.\nvar Kept plain.Box\n\n" +
			"// Twice is twice two.\nfunc Twice() int {\n\treturn platform.Double(2)\n}\n",
	}
}

// A package whose restore is decided late takes the host's lines only for the packages below it whose restore is
// final: the loader decides a package once its imports are done, so p, declined by its own verdict, is checked from
// source before c is judged, and c, reading p's T, is judged against p's own rendering and declines, while d holds
// with the lines of plain, untouched, taken from the host.
func TestAPackageAboveADeclinedOneIsJudgedAgainstItsOwnRendering(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, hostLinesModule())
	host := throughHost(test, root, test.TempDir())
	fromHost := decideBuild(test, root, host, 4)
	for identifier, want := range map[string]string{
		"example.com/hostlines/p": "T object example.com/hostlines/platform.X",
		"example.com/hostlines/c": "T object example.com/hostlines/p.T renders differently",
	} {
		if why := fromHost.Declined[identifier]; !strings.HasPrefix(why, want) {
			test.Errorf("windows declined %s for %q, want a reason starting %q", identifier, why, want)
		}
	}
	if !slices.Equal(fromHost.Restored, []string{"example.com/hostlines/d"}) ||
		!slices.Contains(fromHost.Untouched, "example.com/hostlines/plain") {
		test.Errorf("windows restored %q and left %q untouched, want d restored and plain untouched",
			fromHost.Restored, fromHost.Untouched)
	}
	if fromHost.NodesReused == 0 {
		test.Errorf("windows took no node's lines from the host, rendered %d", fromHost.NodesRendered)
	}
}
