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

// An integrate job stamps the tree it lays by content (#e94a6r4): every tracked file carries the time its blob's id
// gives, so the same content laid in another job reads the same to Go's test cache, every directory and the root carry
// directoryTime, so a change beneath one moves no listing above it (#ntrewm3), and a file the tree doesn't track keeps
// the time it was written.
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
	entries := map[string]string{".": ""}
	for _, line := range strings.Split(strings.TrimSpace(listing), "\n") {
		header, path, _ := strings.Cut(line, "\t")
		fields := strings.Fields(header)
		if len(fields) == 3 && fields[0] != "160000" && fields[0] != "120000" {
			// A directory's entry is empty: it is stamped at directoryTime, never by its tree's id.
			entries[path] = map[bool]string{true: fields[2], false: ""}[fields[1] == "blob"]
		}
	}
	if len(entries) < 3 {
		test.Fatalf("the fixture's merge lists %d entries to check, want a file and a directory at least:\n%s",
			len(entries), listing)
	}
	directories := 0
	for path, id := range entries {
		want := directoryTime
		if id == "" {
			directories++
		} else {
			stamp, wantError := objectTime(id)
			if wantError != nil {
				test.Fatal(wantError)
			}
			want = stamp
		}
		information, statError := os.Stat(filepath.Join(tree, filepath.FromSlash(path)))
		if statError != nil {
			test.Errorf("stat %s: %v", path, statError)
			continue
		}
		if !information.ModTime().Equal(want) {
			test.Errorf("%s is stamped %v, want %v (its blob %q's time, or directoryTime for a directory)", path,
				information.ModTime(), want, id)
		}
	}
	if directories < 2 || directories == len(entries) {
		test.Errorf("the fixture's merge holds %d directories of %d entries, want the root, one directory beneath it "+
			"and a file", directories, len(entries))
	}
}

// A tracked file whose bytes differ from `HEAD` when the stamp runs, the same size but other bytes, keeps the time it
// was written and is named, never stamped with its old blob's time, which would tell Go's test cache it is unchanged
// (system_cohere's review of #32). Its clean neighbor is stamped as ever, and a file only touched, its bytes `HEAD`'s,
// reads clean once the index is refreshed.
//
// Not parallel: it runs git in this process with a configuration of its own, set with test.Setenv.
func TestAFileThatDiffersFromHeadKeepsItsOwnTime(test *testing.T) {
	for _, setting := range [][2]string{{"GIT_CONFIG_GLOBAL", os.DevNull}, {"GIT_CONFIG_NOSYSTEM", "1"},
		{"GIT_AUTHOR_NAME", "test"}, {"GIT_AUTHOR_EMAIL", "test@example.com"}, {"GIT_COMMITTER_NAME", "test"},
		{"GIT_COMMITTER_EMAIL", "test@example.com"}} {
		test.Setenv(setting[0], setting[1])
	}
	tree := test.TempDir()
	for _, file := range []string{"changed.txt", "clean.txt", "touched.txt"} {
		writeFixtureFile(test, filepath.Join(tree, file), "the committed bytes\n")
	}
	for _, arguments := range [][]string{{"init", "-q"}, {"add", "."}, {"commit", "-q", "-m", "base"}} {
		if output, gitError := gitCombined(tree, arguments...); gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
	}
	writeFixtureFile(test, filepath.Join(tree, "changed.txt"), "the committed bytez\n")
	if information, statError := os.Stat(filepath.Join(tree, "changed.txt")); statError != nil ||
		information.Size() != int64(len("the committed bytes\n")) {
		test.Fatalf("the changed file isn't the committed size: %v", statError)
	}
	now := time.Now()
	if changeError := os.Chtimes(filepath.Join(tree, "touched.txt"), now, now); changeError != nil {
		test.Fatal(changeError)
	}
	unstamped, stampError := stampTreeTimes(tree)
	if stampError != nil {
		test.Fatal(stampError)
	}
	if len(unstamped) != 1 || unstamped[0] != "changed.txt" {
		test.Errorf("the stamp left %q unstamped, want changed.txt alone", unstamped)
	}
	for file, wantStamped := range map[string]bool{"changed.txt": false, "clean.txt": true, "touched.txt": true} {
		blob, blobError := gitOutput(tree, "rev-parse", "HEAD:"+file)
		if blobError != nil {
			test.Fatal(blobError)
		}
		stamp, timeError := objectTime(blob)
		if timeError != nil {
			test.Fatal(timeError)
		}
		information, statError := os.Stat(filepath.Join(tree, file))
		if statError != nil {
			test.Fatal(statError)
		}
		if information.ModTime().Equal(stamp) != wantStamped {
			test.Errorf("%s is at %v, its blob's time %v: stamped %t, want %t", file, information.ModTime(), stamp,
				!wantStamped, wantStamped)
		}
	}
}
