package project

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// selectionDeadline bounds a selection that must finish by the files, not the combinations of their tags. It is a
// correctness guard, not a measure of speed: a walk of every set of the fixture's 47 tags makes 2^47 sets a platform
// and never finishes, while the selection by component finishes in milliseconds, so the deadline sits far from both.
const selectionDeadline = 30 * time.Second

// manyTagsFiles are files that name more tags than any set of them can be weighed one by one: 40 tags each named by
// one linux file and one windows file of a package of its own, two linux files that want two other tags in opposite
// ways, a package that keeps two versions apart in a file each, one whose file compiles under either of two tags it
// keeps apart elsewhere, and a file only another architecture compiles.
func manyTagsFiles() map[string]string {
	files := map[string]string{}
	for index := range 40 {
		files[fmt.Sprintf("tag%02d/linux.go", index)] = fmt.Sprintf("//go:build linux && tag%02d\n\npackage p\n", index)
		files[fmt.Sprintf("tag%02d/windows.go", index)] =
			fmt.Sprintf("//go:build windows && tag%02d\n\npackage p\n", index)
	}
	files["conflict/x.go"] = "//go:build linux && tagx && !tagy\n\npackage conflict\n"
	files["conflict/y.go"] = "//go:build linux && tagy && !tagx\n\npackage conflict\n"
	files["versions/version_a.go"] = "//go:build linux && versiona\n\npackage versions\n"
	files["versions/version_b.go"] = "//go:build linux && versionb\n\npackage versions\n"
	files["either/either.go"] = "//go:build linux && (eithera || eitherb)\n\npackage either\n"
	files["either/a.go"] = "//go:build linux && eithera\n\npackage either\n"
	files["either/b.go"] = "//go:build linux && eitherb\n\npackage either\n"
	files["elsewhere.go"] = "//go:build amd64\n\npackage p\n"
	files["host.go"] = "package p\n"
	return files
}

// With 47 tags the builds are chosen tag component by tag component, in time bounded by the files
// (selectionDeadline), and no build sets two components' tags (#h7m6zyp): each of the 40 packages' tags gets a linux
// build and a windows build of its own, the two linux files that want opposite tags get a linux build each, two tags
// one package keeps apart never ride one build, even where a file compiles under either, every file some build of
// the host's architecture compiles is compiled, and only the file no build on the host's architecture compiles is
// unbuilt.
func TestBuildsForChoosesByTagComponentWhenTheTagsAreMany(test *testing.T) {
	test.Parallel()
	census := censusOf(test, manyTagsFiles())
	type selection struct {
		builds  []Build
		unbuilt []Unbuilt
		failure error
	}
	done := make(chan selection, 1)
	go func() {
		builds, unbuilt, buildsError := BuildsFor(census, testHost, testPorts, nil)
		done <- selection{builds: builds, unbuilt: unbuilt, failure: buildsError}
	}()
	var chosen selection
	select {
	case chosen = <-done:
	case <-time.After(selectionDeadline):
		test.Fatalf("the selection did not finish in %v: it walks the combinations of the tags", selectionDeadline)
	}
	if chosen.failure != nil {
		test.Fatal(chosen.failure)
	}
	linuxTags := []string{"eithera", "eitherb"}
	var windowsTags []string
	for index := range 40 {
		linuxTags = append(linuxTags, fmt.Sprintf("tag%02d", index))
		windowsTags = append(windowsTags, fmt.Sprintf("tag%02d", index))
	}
	linuxTags = append(linuxTags, "tagx", "tagy", "versiona", "versionb")
	want := []string{"darwin/arm64 cgo"}
	for _, tag := range linuxTags {
		want = append(want, "linux/arm64 tags "+tag)
	}
	for _, tag := range windowsTags {
		want = append(want, "windows/arm64 tags "+tag)
	}
	if got := labels(chosen.builds); !slices.Equal(got, want) {
		test.Errorf("the builds are\n%v\nwant\n%v", got, want)
	}
	requireEachCompiledOnce(test, census, chosen.builds, chosen.unbuilt)
	var unbuiltNames []string
	for _, each := range chosen.unbuilt {
		unbuiltNames = append(unbuiltNames, filepath.Base(each.File))
	}
	if !slices.Equal(unbuiltNames, []string{"elsewhere.go"}) {
		test.Errorf("unbuilt %v, want only elsewhere.go, which no build on arm64 compiles", unbuiltNames)
	}
}

// gccgo's architectures are architectures, never tags: encoding/binary's big-endian file names `m68k` beside gc's
// big-endian targets, and setting `m68k` as a tag on arm64 would compile it beside the little-endian file. Such a file
// is unbuilt, named for the architecture no target of ours is, and no build sets the name.
func TestBuildsForReadsGccgosArchitecturesAsArchitectures(test *testing.T) {
	test.Parallel()
	census := censusOf(test, map[string]string{
		"native_big.go":    "//go:build armbe || arm64be || m68k || shbe\n\npackage p\n",
		"native_little.go": "//go:build 386 || amd64 || arm || arm64\n\npackage p\n",
		"only_m68k.go":     "//go:build m68k\n\npackage p\n",
	})
	builds, unbuilt := buildsFor(test, census, testHost, testPorts)
	if got := labels(builds); !slices.Equal(got, []string{"darwin/arm64 cgo"}) {
		test.Errorf("the builds are %v, want the host's alone: no build sets m68k as a tag", got)
	}
	reasons := map[string]string{}
	for _, each := range unbuilt {
		reasons[filepath.Base(each.File)] = each.Reason
	}
	for _, file := range []string{"native_big.go", "only_m68k.go"} {
		if reason, found := reasons[file]; !found || !strings.Contains(reason, "go tool dist list does not target") {
			test.Errorf("%s is unbuilt for %q, want it named for an architecture no target is", file, reason)
		}
	}
}
