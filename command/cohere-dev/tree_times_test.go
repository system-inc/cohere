//go:build unix

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// An object's time follows its id alone: the same id is the same time, ids that differ in their first sixteen bytes
// are different times, every time lies years in the past, out of reach of Go's two-second rule, and a string that
// isn't an object id is refused, never stamped.
func TestAnObjectsTimeFollowsItsId(test *testing.T) {
	test.Parallel()
	first, second := strings.Repeat("ab", 20), "ab"+strings.Repeat("cd", 19)
	firstTime, firstError := objectTime(first)
	again, againError := objectTime(first)
	secondTime, secondError := objectTime(second)
	if firstError != nil || againError != nil || secondError != nil {
		test.Fatalf("objectTime refused an object id: %v, %v, %v", firstError, againError, secondError)
	}
	if !firstTime.Equal(again) {
		test.Errorf("one id gave %v and %v", firstTime, again)
	}
	if firstTime.Equal(secondTime) {
		test.Errorf("two ids gave one time, %v", firstTime)
	}
	for _, stamped := range []time.Time{firstTime, secondTime} {
		if stamped.Before(objectTimeEpoch) || time.Since(stamped) < 365*24*time.Hour {
			test.Errorf("a stamped time %v lies outside 1980 to a year ago", stamped)
		}
	}
	// A sha256 object id is the same length of hex past its first sixteen bytes, and reads the same way.
	if _, sha256Error := objectTime(strings.Repeat("0f", 32)); sha256Error != nil {
		test.Errorf("a sha256 object id was refused: %v", sha256Error)
	}
	for _, refused := range []string{"", "not hex", strings.Repeat("ab", 15)} {
		if _, refusedError := objectTime(refused); refusedError == nil {
			test.Errorf("objectTime took %q as an object id", refused)
		}
	}
}

// An integrate job stamps the tree it lays by content (#e94a6r4): every tracked file and directory, and the root,
// carry the time their git object's id gives, so the same content laid in another job reads the same to Go's test
// cache, while a file the tree doesn't track keeps the time it was written.
func TestAnIntegrateJobStampsItsTreeByContent(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	merge := fixture.merged()
	ran := fixture.integrateJob(merge)
	if ran.exit != 0 {
		test.Fatalf("the integrate job exited %d:\n%s", ran.exit, ran.errors)
	}
	tree := filepath.Join(fixture.home, integrateJobScratch, "tree")
	if !strings.Contains(ran.errors, "its times by content") {
		test.Errorf("the lay step doesn't say it stamped the tree's times:\n%s", ran.errors)
	}
	listing := fixture.git(fixture.repository, "ls-tree", "-r", "-t", "--full-tree", merge)
	entries := map[string]string{".": fixture.git(fixture.repository, "rev-parse", merge+"^{tree}")}
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		header, path, _ := strings.Cut(line, "\t")
		fields := strings.Fields(header)
		if len(fields) == 3 && fields[0] != "160000" && fields[0] != "120000" {
			entries[path] = fields[2]
		}
	}
	if len(entries) < 3 {
		test.Fatalf("the fixture's merge lists %d entries to check, want a file and a directory at least:\n%s",
			len(entries), listing)
	}
	for path, id := range entries {
		want, wantError := objectTime(id)
		if wantError != nil {
			test.Fatal(wantError)
		}
		information, statError := os.Stat(filepath.Join(tree, filepath.FromSlash(path)))
		if statError != nil {
			test.Errorf("stat %s: %v", path, statError)
			continue
		}
		if !information.ModTime().Equal(want) {
			test.Errorf("%s is stamped %v, want %v, its object %s's time", path, information.ModTime(), want, id)
		}
	}
}
