package project

import (
	"encoding/json"
	"go/build"
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
	var census Census
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
	builds, unbuilt, buildsError := BuildsFor(census, host, ports)
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

// A `_windows.go` file, a `linux` file, a `!unix` file and a custom tag's file need two builds beside the host's:
// linux with the tag folded in, and windows, which builds both the windows and the `!unix` file.
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
	want := []string{"darwin/arm64 cgo", "linux/arm64 tags cohere_poison", "windows/arm64"}
	if !slices.Equal(labels(builds), want) {
		test.Errorf("the builds are %q, want %q", labels(builds), want)
	}
	if len(unbuilt) != 0 {
		test.Errorf("unbuilt are %v, want none", unbuilt)
	}
	requireEachCompiledOnce(test, census, builds, unbuilt)
	for _, extra := range builds[1:] {
		if extra.CgoEnabled || !slices.Equal(extra.Flags, testHost.Flags) {
			test.Errorf("%v has cgo or other flags than the host's %q", extra, testHost.Flags)
		}
	}
}

// A custom tag's file that nothing else needs an extra build for is built on the host's own platform with the
// tag, never on another platform.
func TestBuildsForFoldsTagsIntoTheHostsPlatformWhenNothingElseNeedsABuild(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"main.go":    "package p\n",
		"poison.go":  "//go:build cohere_poison\n\npackage p\n",
		"planted.go": "//go:build cohere_planted_panic && !windows\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	want := []string{"darwin/arm64 cgo", "darwin/arm64 cgo tags cohere_planted_panic,cohere_poison"}
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
	want := []string{"darwin/arm64 cgo", "linux/arm64 tags cohere_poison", "windows/arm64", "freebsd/arm64",
		"netbsd/arm64"}
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
