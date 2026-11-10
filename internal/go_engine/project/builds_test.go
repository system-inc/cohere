package project

import (
	"encoding/json"
	"go/build"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testHost is the host every fixture's builds are read for, so a test reads alike on any machine: a Mac on arm64
// with cgo on, as go env reads one with a C compiler.
var testHost = Build{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Flags: []string{"-trimpath"}}

// testPorts are a toolchain's targets as go tool dist list prints them, by name: the arm64 ports beside amd64,
// wasm and plan9 ones a file might name, so a file only another architecture builds has a target to be built by.
var testPorts = []Port{
	{OperatingSystem: "android", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "darwin", Architecture: "amd64", CgoSupported: true},
	{OperatingSystem: "darwin", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "freebsd", Architecture: "amd64", CgoSupported: true},
	{OperatingSystem: "freebsd", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "ios", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "js", Architecture: "wasm"},
	{OperatingSystem: "linux", Architecture: "amd64", CgoSupported: true},
	{OperatingSystem: "linux", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "netbsd", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "openbsd", Architecture: "arm64", CgoSupported: true},
	{OperatingSystem: "plan9", Architecture: "amd64"},
	{OperatingSystem: "windows", Architecture: "amd64", CgoSupported: true},
	{OperatingSystem: "windows", Architecture: "arm64", CgoSupported: true},
}

// censusOf writes files into a directory of the test's own and is a census of all of them as ours, sorted as
// TakeCensus sorts, so a test of the builds reads no workspace.
func censusOf(test *testing.T, files map[string]string) Census {
	test.Helper()
	directory := test.TempDir()
	if writeError := writeFiles(directory, files); writeError != nil {
		test.Fatal(writeError)
	}
	census := Census{Root: directory}
	for name := range files {
		census.Ours = append(census.Ours, filepath.Join(directory, filepath.FromSlash(name)))
	}
	slices.Sort(census.Ours)
	census.Files = slices.Clone(census.Ours)
	return census
}

// buildsFor is BuildsFor's answer for census, failing the test on an error, and failing it too when any build is
// on an architecture other than the host's: the guard every test of the builds holds.
func buildsFor(test *testing.T, census Census, host Build, ports []Port) ([]Build, []Unbuilt) {
	test.Helper()
	builds, unbuilt, buildsError := BuildsFor(census, host, ports, nil)
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if len(builds) == 0 || !builds[0].sameAs(host) {
		test.Fatalf("the builds are %v, and the host's %v is not first", builds, host)
	}
	for _, each := range builds {
		if each.Architecture != host.Architecture {
			test.Fatalf("the builds hold %v, on another architecture than the host's %s", each, host.Architecture)
		}
	}
	return builds, unbuilt
}

// labels are builds as String names them.
func labels(builds []Build) []string {
	named := make([]string, 0, len(builds))
	for _, each := range builds {
		named = append(named, each.String())
	}
	return named
}

// requireEachCompiledOnce fails the test unless every census file outside unbuilt is compiled by at least one
// build, by go/build's own reading.
func requireEachCompiledOnce(test *testing.T, census Census, builds []Build, unbuilt []Unbuilt) {
	test.Helper()
	for _, file := range census.Ours {
		if slices.ContainsFunc(unbuilt, func(each Unbuilt) bool { return each.File == file }) {
			continue
		}
		read, readError := readSource(file)
		if readError != nil {
			test.Fatal(readError)
		}
		compiledBy := 0
		for _, each := range builds {
			compiled, compileError := compiles(each, read)
			if compileError != nil {
				test.Fatal(compileError)
			}
			if compiled {
				compiledBy++
			}
		}
		if compiledBy == 0 {
			test.Errorf("no build of %v compiles %s", labels(builds), file)
		}
	}
}

// A `_windows.go` file, a `linux` file, a `!unix` file and a custom tag's file need three builds beside the host's:
// linux, windows, which builds both the windows and the `!unix` file, and the host's platform with the tag, which
// never rides a platform's build (#h7m6zyp).
func TestBuildsForCompilesEachFileOnce(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":            "package p\n",
		"console_windows.go": "package p\n",
		"epoll.go":           "//go:build linux\n\npackage p\n",
		"not_unix.go":        "//go:build !unix\n\npackage p\n",
		"poison.go":          "//go:build cohere_poison\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	want := []string{"darwin/arm64 cgo", "darwin/arm64 cgo tags cohere_poison", "linux/arm64", "windows/arm64"}
	if !slices.Equal(labels(builds), want) {
		test.Errorf("the builds are %q, want %q", labels(builds), want)
	}
	if len(unbuilt) != 0 {
		test.Errorf("unbuilt are %v, want none", unbuilt)
	}
	requireEachCompiledOnce(test, census, builds, unbuilt)
	for _, extra := range builds[2:] {
		if extra.CgoEnabled || !slices.Equal(extra.Flags, testHost.Flags) {
			test.Errorf("%v has cgo or other flags than the host's %q", extra, testHost.Flags)
		}
	}
}

// A custom tag's file is built on the host's own platform with the tag, never on a platform no file names beside
// it, and each file of one package takes the least setting of its own tags, so two tags of one package each get a
// build (buildsByComponent).
func TestBuildsForPutsTagsOnTheHostsPlatform(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":    "package p\n",
		"poison.go":  "//go:build cohere_poison\n\npackage p\n",
		"planted.go": "//go:build cohere_planted_panic && !windows\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	want := []string{"darwin/arm64 cgo", "darwin/arm64 cgo tags cohere_planted_panic",
		"darwin/arm64 cgo tags cohere_poison"}
	if !slices.Equal(labels(builds), want) || len(unbuilt) != 0 {
		test.Errorf("the builds are %q and unbuilt %v, want %q and none", labels(builds), unbuilt, want)
	}
}

// A file only another architecture builds, by its name or by its constraint, is never a build on that
// architecture: it is unbuilt, its reason naming the layout blind spot.
func TestBuildsForNeverBuildsAnotherArchitecture(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":         "package p\n",
		"layout_amd64.go": "package p\n",
		"wide.go":         "//go:build amd64 && linux\n\npackage p\n",
		"browser.go":      "//go:build js\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if !slices.Equal(labels(builds), []string{"darwin/arm64 cgo"}) {
		test.Errorf("the builds are %q, want the host's alone", labels(builds))
	}
	var files []string
	for _, each := range unbuilt {
		files = append(files, filepath.Base(each.File))
		if !strings.Contains(each.Reason, "layout blind spot") {
			test.Errorf("%s is unbuilt for %q, which names no layout blind spot", each.File, each.Reason)
		}
	}
	if want := []string{"browser.go", "layout_amd64.go", "wide.go"}; !slices.Equal(files, want) {
		test.Errorf("unbuilt are %q, want %q", files, want)
	}
}

// A file no target builds is unbuilt with why: `ignore`, a `GOOS` the toolchain does not target beside a tag, and a
// constraint no `GOOS` meets.
func TestBuildsForNamesWhatNoTargetBuilds(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":       "package p\n",
		"generate.go":   "//go:build ignore\n\npackage main\n",
		"lonely.go":     "//go:build cohere_lonely && hurd\n\npackage p\n",
		"impossible.go": "//go:build linux && windows\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if !slices.Equal(labels(builds), []string{"darwin/arm64 cgo"}) {
		test.Errorf("the builds are %q, want the host's alone", labels(builds))
	}
	reasons := map[string]string{}
	for _, each := range unbuilt {
		reasons[filepath.Base(each.File)] = each.Reason
	}
	want := map[string]string{
		"generate.go":   "`ignore`",
		"lonely.go":     "hurd, which go tool dist list does not target",
		"impossible.go": "no target go tool dist list names builds it",
	}
	if len(reasons) != len(want) {
		test.Errorf("unbuilt are %v, want %d", unbuilt, len(want))
	}
	for file, phrase := range want {
		if !strings.Contains(reasons[file], phrase) {
			test.Errorf("%s is unbuilt for %q, want a reason naming %q", file, reasons[file], phrase)
		}
	}
}

// cgo varies only on the host's platform: a `!cgo` file is the host's platform with cgo off, and a file only cgo on
// another `GOOS` builds is unbuilt, since its C toolchain is not the host's.
func TestBuildsForVariesCgoOnlyOnTheHostsPlatform(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":         "package p\n",
		"pure.go":         "//go:build !cgo\n\npackage p\n",
		"native_linux.go": "package p\n\nimport \"C\"\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if want := []string{"darwin/arm64 cgo", "darwin/arm64"}; !slices.Equal(labels(builds), want) {
		test.Errorf("the builds are %q, want %q", labels(builds), want)
	}
	if len(unbuilt) != 1 || filepath.Base(unbuilt[0].File) != "native_linux.go" ||
		!strings.Contains(unbuilt[0].Reason, "only linux/arm64 cgo builds it") {
		test.Errorf("unbuilt are %v, want native_linux.go, which only cgo on linux builds", unbuilt)
	}
}

// The builds never turn on the order the toolchain lists its targets in: every order of the same ports gives the
// same builds, the preferred platforms first, and the same unbuilt reasons.
func TestBuildsForComeOutInTheSameOrderEveryRun(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":            "package p\n",
		"console_windows.go": "package p\n",
		"epoll.go":           "//go:build linux\n\npackage p\n",
		"kqueue_freebsd.go":  "package p\n",
		"bsd.go":             "//go:build openbsd || netbsd\n\npackage p\n",
		"poison.go":          "//go:build cohere_poison\n\npackage p\n",
		"wide_amd64.go":      "package p\n",
	})
	reversed := slices.Clone(testPorts)
	slices.Reverse(reversed)
	rotated := append(slices.Clone(testPorts[5:]), testPorts[:5]...)
	firstBuilds, firstUnbuilt := buildsFor(test, census, testHost, testPorts)
	want := []string{"darwin/arm64 cgo", "darwin/arm64 cgo tags cohere_poison", "linux/arm64", "windows/arm64",
		"freebsd/arm64", "netbsd/arm64"}
	if !slices.Equal(labels(firstBuilds), want) {
		test.Errorf("the builds are %q, want %q", labels(firstBuilds), want)
	}
	for _, ports := range [][]Port{testPorts, reversed, rotated} {
		builds, unbuilt := buildsFor(test, census, testHost, ports)
		if !slices.Equal(labels(builds), labels(firstBuilds)) || !slices.Equal(unbuilt, firstUnbuilt) {
			test.Errorf("under ports %v the builds are %q and unbuilt %v, where the first run's were %q and %v",
				ports, labels(builds), unbuilt, labels(firstBuilds), firstUnbuilt)
		}
	}
}

// The dimensions come from the constraint lines and from the names as go/build reads them: a `_test` suffix after
// a `GOOS`, a `GOOS` and `GOARCH` pair, `unix` as the ports go/build counts as unix, and none from a name that only
// looks like one, `go1.N`, `ignore` or a tag the toolchain sets.
func TestDimensionsOfReadsConstraintsAndSuffixesAsGoBuildDoes(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"console_windows.go":     "package p\n",
		"syscall_linux_arm64.go": "package p\n",
		"kqueue_freebsd_test.go": "package p\n",
		"windows_helper.go":      "package p\n",
		"unix.go":                "//go:build unix\n\npackage p\n",
		"native.go":              "package p\n\nimport \"C\"\n",
		"poison.go":              "//go:build go1.21 && cohere_poison && !ignore\n\npackage p\n",
		"old.go":                 "// +build cohere_legacy\n\npackage p\n",
		"both.go":                "//go:build cohere_new\n// +build cohere_old\n\npackage p\n",
		"experiment.go":          "//go:build goexperiment.jsonv2 && gc\n\npackage p\n",
		"feature.go":             "//go:build amd64.v3\n\npackage p\n",
		"after_package.go":       "package p\n\n//go:build cohere_late\n",
	})
	ports := []Port{
		{OperatingSystem: "darwin", Architecture: "arm64"}, {OperatingSystem: "linux", Architecture: "arm64"},
		{OperatingSystem: "windows", Architecture: "arm64"}, {OperatingSystem: "js", Architecture: "wasm"},
	}
	dimensions, dimensionsError := DimensionsOf(census.Ours, ports)
	if dimensionsError != nil {
		test.Fatal(dimensionsError)
	}
	want := Dimensions{
		OperatingSystems: []string{"darwin", "freebsd", "linux", "windows"},
		Architectures:    []string{"amd64", "arm64"},
		Tags:             []string{"cohere_legacy", "cohere_new", "cohere_poison"},
		Cgo:              true,
	}
	if !slices.Equal(dimensions.OperatingSystems, want.OperatingSystems) ||
		!slices.Equal(dimensions.Architectures, want.Architectures) || !slices.Equal(dimensions.Tags, want.Tags) ||
		dimensions.Cgo != want.Cgo {
		test.Errorf("the dimensions are %+v, want %+v", dimensions, want)
	}
}

// The host's build is the context load reads from go env and its `GOFLAGS`: tags from the context, every other
// flag kept, and its environment sets the target, cgo, and `GOFLAGS` with the build's own tags.
func TestHostBuildIsWhatGoEnvironmentReads(test *testing.T) {
	test.Parallel()
	buildContext := build.Default
	buildContext.GOOS, buildContext.GOARCH, buildContext.CgoEnabled = "linux", "arm64", false
	buildContext.BuildTags = []string{"cohere_poison", "cohere_planted_panic"}
	host := HostBuild(&buildContext, []string{"-trimpath", "-tags=cohere_poison,cohere_planted_panic", "-mod=mod"})
	if want := "linux/arm64 tags cohere_planted_panic,cohere_poison"; host.String() != want {
		test.Errorf("the host is %s, want %s", host, want)
	}
	want := []string{"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0",
		"GOFLAGS=-trimpath -mod=mod -tags=cohere_planted_panic,cohere_poison"}
	if !slices.Equal(host.Environment(), want) {
		test.Errorf("the host's environment is %q, want %q", host.Environment(), want)
	}
	untagged := Build{OperatingSystem: "windows", Architecture: "arm64", Flags: []string{"-trimpath"}}
	if want := []string{"GOOS=windows", "GOARCH=arm64", "CGO_ENABLED=0", "GOFLAGS=-trimpath"}; !slices.Equal(
		untagged.Environment(), want) {
		test.Errorf("an untagged build's environment is %q, want %q", untagged.Environment(), want)
	}
}

// cohere's own census at the worktree's root, under the host go env reads and the toolchain's own targets: the
// builds are printed, the host's first and every one on its architecture, and every file is compiled by one or
// unbuilt with a reason.
func TestBuildsForCoheresOwnCensus(test *testing.T) {
	test.Parallel()
	root, rootError := filepath.Abs(filepath.Join("..", "..", ".."))
	if rootError != nil {
		test.Fatal(rootError)
	}
	covered, readError := ReadWorkspace(root)
	if readError != nil {
		test.Fatal(readError)
	}
	census, censusError := TakeCensus(covered)
	if censusError != nil {
		test.Fatal(censusError)
	}
	ports, portsError := ReadPorts(root, nil)
	if portsError != nil {
		test.Fatal(portsError)
	}
	host := hostAt(test, root)
	builds, unbuilt := buildsFor(test, census, host, ports)
	requireEachCompiledOnce(test, census, builds, unbuilt)
	for _, each := range builds {
		test.Logf("build %s", each)
	}
	for _, each := range unbuilt {
		relative, relativeError := filepath.Rel(root, each.File)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		test.Logf("unbuilt %s: %s", relative, each.Reason)
	}
}

// hostAt is the host's build in root as the engine's loader reads it: the target, cgo and `GOFLAGS` from go env, as
// load's constraintContext asks, with `GOFLAGS`' `-tags` as the context's tags. go env reads no package, so the
// test's cache key needs nothing recorded for it, and the test reads no `GOFLAGS` of its own.
func hostAt(test *testing.T, root string) Build {
	test.Helper()
	environmentCommand := exec.Command("go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS")
	environmentCommand.Dir = root
	environmentCommand.Env = os.Environ()
	reported, environmentError := environmentCommand.Output()
	if environmentError != nil {
		test.Fatalf("go env: %v", environmentError)
	}
	var values struct {
		Goos       string `json:"GOOS"`
		Goarch     string `json:"GOARCH"`
		CgoEnabled string `json:"CGO_ENABLED"`
		Goflags    string `json:"GOFLAGS"`
	}
	if decodeError := json.Unmarshal(reported, &values); decodeError != nil {
		test.Fatalf("reading go env's answer %q: %v", reported, decodeError)
	}
	goFlags := strings.Fields(values.Goflags)
	buildContext := build.Default
	buildContext.GOOS, buildContext.GOARCH, buildContext.CgoEnabled = values.Goos, values.Goarch,
		values.CgoEnabled == "1"
	buildContext.BuildTags = nil
	for _, flag := range goFlags {
		if tags, isTags := strings.CutPrefix(strings.TrimLeft(flag, "-"), "tags="); isTags {
			buildContext.BuildTags = append(buildContext.BuildTags,
				strings.FieldsFunc(tags, func(character rune) bool { return character == ',' })...)
		}
	}
	return HostBuild(&buildContext, goFlags)
}

// A file whose package is not the one its directory compiles is a program of its own, run by itself (#rkw4xcn):
// adamic's `package main` oracle behind lintoracle, beside a package's tests. No build of the directory's package
// compiles it, so its tag is never set for it, and it is unbuilt with that reason. An external test behind a tag
// tests the package it sits in, and a tagged file of the package itself is that package's, so both still get the
// build their tag needs.
func TestBuildsForNeverFoldsAProgramOfItsOwnIntoItsDirectorysPackage(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"rules/rule/build_test.go": "package assertions\n",
		"rules/rule/oracle.go":     "//go:build lintoracle\n\npackage main\n",
		"probe/probe.go":           "package probe\n",
		"probe/external_test.go":   "//go:build probetag\n\npackage probe_test\n",
		"probe/tagged.go":          "//go:build probetag\n\npackage probe\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if got := labels(builds); !slices.Equal(got, []string{"darwin/arm64 cgo", "darwin/arm64 cgo tags probetag"}) {
		test.Errorf("the builds are %v, want the host's and probetag's alone: lintoracle is never set", got)
	}
	if len(unbuilt) != 1 || filepath.Base(unbuilt[0].File) != "oracle.go" ||
		unbuilt[0].Reason != "a program of its own, package main beside package assertions, so no build of its "+
			"directory's package compiles it" {
		test.Errorf("unbuilt %+v, want oracle.go alone, named as a program of its own", unbuilt)
	}
	requireEachCompiledOnce(test, census, builds, unbuilt)
}

// adamic's oracles as its census holds them (#h7m6zyp): lintoracle is named by `package main` oracles, one beside a
// package's tests, the rest alone in their directories, where they are the directory's package. Setting lintoracle
// for the lone ones compiles the one beside the tests too, so the tag a program of its own names is never set: the
// oracle beside the tests is unbuilt as a program of its own, and each lone one for its tag, naming the directory it
// would break. No build sets lintoracle.
func TestBuildsForNeverSetsATagAProgramOfItsOwnNames(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"rules/beside/build_test.go": "package assertions\n",
		"rules/beside/oracle.go":     "//go:build lintoracle\n\npackage main\n",
		"rules/alone/oracle.go":      "//go:build lintoracle\n\npackage main\n",
		"rules/apart/oracle.go":      "//go:build lintoracle\n\npackage main\n",
		"rules/apart/apart_test.go":  "//go:build lintoracle\n\npackage main\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if got := labels(builds); !slices.Equal(got, []string{"darwin/arm64 cgo"}) {
		test.Errorf("the builds are %v, want the host's alone: lintoracle is never set", got)
	}
	poisoned := "its tag lintoracle is never set: setting it puts package main beside package assertions in " +
		"rules/beside"
	want := map[string]string{
		"rules/alone/oracle.go":     poisoned,
		"rules/apart/apart_test.go": poisoned,
		"rules/apart/oracle.go":     poisoned,
		"rules/beside/oracle.go": "a program of its own, package main beside package assertions, so no build of " +
			"its directory's package compiles it",
	}
	got := map[string]string{}
	for _, each := range unbuilt {
		relative, relativeError := filepath.Rel(census.Root, each.File)
		if relativeError != nil {
			test.Fatal(relativeError)
		}
		got[filepath.ToSlash(relative)] = each.Reason
	}
	if !maps.Equal(got, want) {
		test.Errorf("unbuilt are %v, want %v", got, want)
	}
}

// adamic's shape on a box (#h7m6zyp): a darwin-only file beside two tags, each named in a package of its own, as
// stage3_split_metrics names a lone program and census_overlay an external test. darwin's build carries the host's
// tags alone, so neither tag's type errors can cost it its linting, and each tag gets a build of its own on the host's
// platform. A file that names linux beside a tag gets that tag on linux, and one whose tag only a platform it never
// names would compile is unbuilt for that.
func TestBuildsForKeepsTagsOffPlatformBuilds(test *testing.T) {
	test.Parallel()
	host := Build{OperatingSystem: "linux", Architecture: "arm64", CgoEnabled: true, Flags: []string{"-trimpath"}}
	census := censusOf(test, map[string]string{
		"native/native.go":        "package native\n",
		"native/native_darwin.go": "package native\n",
		"metrics/main.go":         "//go:build stage3_split_metrics\n\npackage main\n",
		"latent/latent.go":        "package latent\n",
		"latent/deferred_test.go": "//go:build census_overlay\n\npackage latent_test\n",
		"probe/probe.go":          "package probe\n",
		"probe/probe_windows.go":  "//go:build probetag\n\npackage probe\n",
		"orphan/orphan.go":        "package orphan\n",
		"orphan/elsewhere.go":     "//go:build !linux && orphantag\n\npackage orphan\n",
	})
	builds, unbuilt := buildsFor(test, census, host, testPorts)
	want := []string{"linux/arm64 cgo", "linux/arm64 cgo tags census_overlay",
		"linux/arm64 cgo tags stage3_split_metrics", "windows/arm64 tags probetag", "darwin/arm64"}
	if got := labels(builds); !slices.Equal(got, want) {
		test.Errorf("the builds are %v, want %v: no platform's build carries a tag no file names beside it", got,
			want)
	}
	if len(unbuilt) != 1 || filepath.Base(unbuilt[0].File) != "elsewhere.go" ||
		unbuilt[0].Reason != "only windows/arm64 tags orphantag builds it, and a platform's build carries the "+
			"host's tags alone, taking a tag only where a file names the platform beside it" {
		test.Errorf("unbuilt %+v, want elsewhere.go alone, named for a tag no file names beside a platform", unbuilt)
	}
	requireEachCompiledOnce(test, census, builds, unbuilt)
}

// Target platforms are the ones the files name and the host's (#ntamp9e, Kirk's ruling, Nov 10). adamic's shape: a
// linux file, a darwin file, and a fallback whose constraint only rules those two out, meant for wasm. With no
// settings, the targets are darwin and linux, so the fallback is no target's, named as a gap, and windows, which the
// fallback's constraint lets in, is never chosen for it. With wasip1/wasm among the settings' targets, a build on it
// checks the fallback.
func TestBuildsForSatisfiesANegationOnlyConstraintFromTheTargets(test *testing.T) {
	test.Parallel()
	ports := append(slices.Clone(testPorts), Port{OperatingSystem: "wasip1", Architecture: "wasm"})
	census := censusOf(test, map[string]string{
		"native_linux.go":   "package native\n",
		"native_darwin.go":  "package native\n",
		"fallback_other.go": "//go:build !linux && !darwin\n\npackage native\n",
	})

	builds, unbuilt, buildsError := BuildsFor(census, testHost, ports, nil)
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if got := labels(builds); !slices.Equal(got, []string{"darwin/arm64 cgo", "linux/arm64"}) {
		test.Errorf("with no targets set, the builds are %v, want the host's and linux's: no platform the files never "+
			"name is chosen", got)
	}
	if len(unbuilt) != 1 || filepath.Base(unbuilt[0].File) != "fallback_other.go" ||
		unbuilt[0].Reason != "no target platform compiles it: !linux && !darwin" {
		test.Errorf("unbuilt %+v, want fallback_other.go named as no target's", unbuilt)
	}

	wasm := Port{OperatingSystem: "wasip1", Architecture: "wasm"}
	builds, unbuilt, buildsError = BuildsFor(census, testHost, ports, []Port{wasm})
	if buildsError != nil {
		test.Fatal(buildsError)
	}
	if got := labels(builds); !slices.Equal(got, []string{"darwin/arm64 cgo", "linux/arm64", "wasip1/wasm"}) ||
		len(unbuilt) != 0 {
		test.Errorf("with wasip1/wasm a target, the builds are %v and %+v unbuilt, want wasip1/wasm checking the "+
			"fallback", got, unbuilt)
	}
}

// A `GOOS` a file names only under a negation is one it rules out, and `unix` names a family and no one platform, so
// neither makes a target; a suffix and a constraint outside a negation each do (positiveOperatingSystems).
func TestPositiveOperatingSystemsAreTheOnesAFileIsBuiltFor(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name     string
		contents string
		want     []string
	}{
		{"plain_windows.go", "package p\n", []string{"windows"}},
		{"ruled_out.go", "//go:build !linux && !darwin\n\npackage p\n", nil},
		{"family.go", "//go:build unix\n\npackage p\n", nil},
		{"either.go", "//go:build linux || (freebsd && !cgo)\n\npackage p\n", []string{"freebsd", "linux"}},
		{"double.go", "//go:build !(!plan9)\n\npackage p\n", []string{"plan9"}},
	} {
		read := source{file: filepath.Join(test.TempDir(), testCase.name), contents: []byte(testCase.contents)}
		if got := positiveOperatingSystems(read); !slices.Equal(got, testCase.want) {
			test.Errorf("%s names %v as built for, want %v", testCase.name, got, testCase.want)
		}
	}
}
