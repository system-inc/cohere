package check

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/bounded"
	"github.com/system-inc/cohere/internal/go_language/rule"
)

// wideBottoms is how many bottom packages the wide modules hold, each saved by the first wave and written again once
// the second asks more of it, enough that rewriteEarlier and the save's fan-out run several of them at once.
const wideBottoms = 8

// wideSlots is the size of the pool the wide runs share, as cohere-go's builds share one, so the session's fan-outs
// take its branch on more than one slot.
const wideSlots = 4

// wideModule is the files of a module at modulePath of wideBottoms bottom packages, bottom00 on, each of whose one
// file is bottomFile under the package name bottom, and top, whose Use sums every bottom's Double of argument.
func wideModule(modulePath string, bottomFile string, argument int) map[string]string {
	files := map[string]string{"go.mod": "module " + modulePath + "\n\ngo 1.24\n"}
	var imports []string
	var calls []string
	for index := range wideBottoms {
		name := fmt.Sprintf("bottom%02d", index)
		files[name+"/bottom.go"] = bottomFile
		imports = append(imports, fmt.Sprintf("\t%s %q\n", name, modulePath+"/"+name))
		calls = append(calls, fmt.Sprintf("%s.Double(%d)", name, argument))
	}
	files["top/top.go"] = "package top\n\nimport (\n" + strings.Join(imports, "") + ")\n\n" +
		"func Use() int {\n\treturn " + strings.Join(calls, " + ") + "\n}\n"
	return files
}

// wideBottomPaths are the import paths of the wide module at modulePath's bottom packages, in order.
func wideBottomPaths(modulePath string) []string {
	var paths []string
	for index := range wideBottoms {
		paths = append(paths, fmt.Sprintf("%s/bottom%02d", modulePath, index))
	}
	return paths
}

// sessionFileOf is the one file the package at path has in session's last load, as its syntax names it.
func sessionFileOf(test *testing.T, session *CacheSession, path string) string {
	test.Helper()
	keyed := throughPackage(test, session, path)
	if len(keyed.CompiledGoFiles) != 1 {
		test.Fatalf("want %s of one file, got %q", path, keyed.CompiledGoFiles)
	}
	return keyed.CompiledGoFiles[0]
}

// The first wave writes each of eight bottoms' entries with twice's placed answer, which its own analysis asked, saved
// on the slots of a pool (fanOutWriting), and the second, whose top asks every bottom's Double, writes every one again
// with Double's beside it, several at once (rewriteEarlier): each entry holds what the one bottom's holds in
// TestAnEntryWhosePlacedAnswersGrewAfterTheFirstWaveIsWrittenAgain. Under -race it is the rewrites reading the
// session and the store and writing the store at once.
func TestEveryEntryWhosePlacedAnswersGrewIsWrittenAgainBesideTheOthers(test *testing.T) {
	test.Parallel()
	const modulePath = "example.com/placedwide"
	directory := test.TempDir()
	writeFiles(test, directory, wideModule(modulePath, placedBottom, 2))
	rules := []rule.Rule{houseRule(placedProbe)}
	caching := testCaching(test, rules)
	caching.Pool = bounded.NewPool(wideSlots)
	cold := runWithSession(test, directory, caching, rules)
	if cold.result.Cache.Waves != 2 || cold.result.Cache.Written != wideBottoms+1 {
		test.Fatalf("want every bottom written by a first wave and top by a second, got %+v", cold.result.Cache)
	}
	want := []string{"Double -1 true", "twice -1 true"}
	for _, path := range wideBottomPaths(modulePath) {
		entry, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, path))
		placed := placedProbeAnswers(test, entry, sessionFileOf(test, cold.session, path))
		if !stored || !slices.Equal(placed, want) {
			test.Errorf("want %s's entry written again to hold %q by place, got %q (stored %t)", path, want, placed,
				stored)
		}
	}
}

// Each of eight bottoms' entries, which the first wave wrote with only the answers it asked, on the slots of a pool, is
// rewritten with Double's once top's analysis asked it, several at once (rewriteEarlier), and is a record whole for the
// next run (#fq7dxvx), as the one bottom's is in TestAnEntryRewrittenWithItsAnswersIsReadWhole: with top edited, the
// run hits every bottom, reading the entries on the pool's slots (fanOut), takes each one's types from the export data
// it named before and its answer from the rewritten body, and asks for no reload.
func TestEveryEntryRewrittenWithItsAnswersIsReadWholeBesideTheOthers(test *testing.T) {
	test.Parallel()
	const modulePath = "example.com/answerswide"
	const bottomFile = "package bottom\n\nfunc Double(value int) int {\n\treturn value * 2\n}\n"
	directory := test.TempDir()
	writeFiles(test, directory, wideModule(modulePath, bottomFile, 2))
	rules := []rule.Rule{houseRule(purityProbe)}
	caching := testCaching(test, rules)
	caching.Pool = bounded.NewPool(wideSlots)
	caching.AnswersInPart = map[string]bool{}
	for _, path := range wideBottomPaths(modulePath) {
		caching.AnswersInPart[path] = true
	}
	cold := runWithSession(test, directory, caching, rules)
	for _, path := range wideBottomPaths(modulePath) {
		written, _, stored := storedEntryAt(caching.Store, sessionKey(test, cold.session, path))
		if !stored || answerCount(written.Answers) == 0 {
			test.Fatalf("want %s's entry rewritten with Double's answer, got %+v (stored %t)", path, written.Answers,
				stored)
		}
	}

	caching.AnswersInPart = nil
	writeFiles(test, directory, wideModule(modulePath, bottomFile, 3))
	edited := runWithSession(test, directory, caching, rules)
	if edited.result.Cache.Hit != wideBottoms || edited.result.Cache.Reloads != 0 ||
		edited.loaded.PackagesFromExportData != wideBottoms {
		test.Fatalf("want every bottom hit, read from its export data, with no reload, got %d read from export data "+
			"and %+v", edited.loaded.PackagesFromExportData, edited.result.Cache)
	}
	var messages []string
	for _, finding := range edited.result.Findings {
		messages = append(messages, finding.Message)
	}
	slices.Sort(messages)
	var want []string
	for _, path := range wideBottomPaths(modulePath) {
		want = append(want, path+".Double pure true")
	}
	if !slices.Equal(messages, want) {
		test.Fatalf("want every bottom's Double answered pure\n%q\ngot\n%q", want, messages)
	}
}
