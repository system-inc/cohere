package check

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/bounded"
)

// poolReaders is how many reader or hold packages the pool's fixtures hold above their platform package, each a
// restore from the host the build decides, enough that the decide's fan-out runs several of them at once.
const poolReaders = 8

// unaskedWideFiles are unaskedFiles with poolReaders reader packages, reader00 on, each calling Counted, so each
// one's restore reads Counted's purity, which windows cannot ask of lower.
func unaskedWideFiles() map[string]string {
	files := map[string]string{
		"go.mod": "module example.com/unaskedwide\n\ngo 1.24\n",
		// The rest of the module is unaskedFiles' own, under this module's path.
		"lower/lower.go":             unaskedFiles["lower/lower.go"],
		"platform/platform.go":       unaskedFiles["platform/platform.go"],
		"platform/platform_other.go": unaskedFiles["platform/platform_other.go"],
		"platform/platform_windows.go": "package platform\n\nimport \"example.com/unaskedwide/lower\"\n\n" +
			"// Counted is thrice value, as lower triples it.\nfunc Counted(value int) int {\n" +
			"\treturn lower.Triple(value)\n}\n",
	}
	for index := range poolReaders {
		name := fmt.Sprintf("reader%02d", index)
		files[name+"/"+name+".go"] = "// Package " + name + " reads Counted.\npackage " + name + "\n\n" +
			"import \"example.com/unaskedwide/platform\"\n\n" +
			fmt.Sprintf("// Thrice is thrice %d.\nfunc Thrice() int {\n\treturn platform.Counted(%[1]d)\n}\n", index)
	}
	return files
}

// Every reader windows restores from the host is declined, decided several at once on the slots of a pool
// (fanOutDeciding), for the answer TestADeclineForAnAnswerTheBuildCannotAskSaysSo declines its one reader for: the host
// writes lower's entry with only the answers its questions computed in it and never asks Triple's purity, so windows
// has no answer of Counted's purity to check any reader's read against. On four slots the build declines what it
// declines on one, each for the same reason, counted the same.
func TestEveryReaderOfAnAnswerTheBuildCannotAskIsDeclinedOnEverySlot(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, unaskedWideFiles())
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.AnswersInPart = map[string]bool{"example.com/unaskedwide/lower": true}
	hosted, _ := hostSessionWith(test, root, caching, rules, nil)
	wide := decideBuild(test, root, hosted.Host, wideSlots)
	narrow := decideBuild(test, root, hosted.Host, 1)

	const want = "A the build has no answer function_purity of example.com/unaskedwide/platform.Counted to ask"
	for index := range poolReaders {
		reader := fmt.Sprintf("example.com/unaskedwide/reader%02d", index)
		if why := wide.Declined[reader]; why != want {
			test.Errorf("windows declined %s for %q, want %q", reader, why, want)
		}
	}
	if wide.DeclinedBy["answers"] != poolReaders || wide.DeclinedBy["unnamedAnswers"] != 0 {
		test.Errorf("windows declined %v by reason, want %d by answers and none by unnamedAnswers", wide.DeclinedBy,
			poolReaders)
	}
	if !reflect.DeepEqual(wide, narrow) {
		test.Errorf("on %d slots windows restored\n%+v\nand on one\n%+v", wideSlots, wide, narrow)
	}
}

// holdingFiles are a module of a platform package whose files differ by build, declaring Double alike in each, and
// poolReaders hold packages, hold00 on, each calling Double, so each one reads a file that differs by build and is
// judged, and its restore holds. Nothing imports the standard library, so the host and the builds analyze little.
func holdingFiles() map[string]string {
	double := "// Double is twice value.\nfunc Double(value int) int {\n\treturn value * 2\n}\n"
	files := map[string]string{
		"go.mod": "module example.com/holding\n\ngo 1.24\n",
		"platform/platform.go": "// Package platform is declared alike in every build and each build's own way.\n" +
			"package platform\n",
		"platform/platform_other.go":   "//go:build !windows\n\npackage platform\n\n" + double,
		"platform/platform_windows.go": "package platform\n\n" + double,
	}
	for index := range poolReaders {
		name := fmt.Sprintf("hold%02d", index)
		files[name+"/"+name+".go"] = "package " + name + "\n\nimport \"example.com/holding/platform\"\n\n" +
			fmt.Sprintf("// Twice is twice %d.\nfunc Twice() int { return platform.Double(%[1]d) }\n", index)
	}
	return files
}

// holdingBuild is windows's run over root on a pool of slots, restoring from host, with its store in a directory of
// the test's own, as buildWith runs it, with what it restored from the host, its time blanked, since it is the one
// count that differs between two runs, and its session.
func holdingBuild(test *testing.T, root string, host *HostRun, slots int) (HostRestoreCounts, *CacheSession) {
	test.Helper()
	rules := throughRules()
	caching := windowsCachingAt(test.TempDir(), rules)
	caching.Pool = bounded.NewPool(slots)
	result, session := buildWith(test, root, caching, host, rules)
	if result.Cache.WrittenThrough != poolReaders {
		test.Errorf("on a pool of %d windows wrote %d entries through, want every hold package's, %d: %+v", slots,
			result.Cache.WrittenThrough, poolReaders, result.Cache)
	}
	fromHost := result.FromHost
	fromHost.Deciding = 0
	return fromHost, session
}

// Every hold package windows restores from the host is decided several at once on the slots of a pool
// (fanOutDeciding), holds, and has its entry written through to the build's store, encoded on the pool's slots and
// written with none held (fanOutWriting): on four slots the build restores, decides and writes through what it does
// on one, each entry decoding to the same one. Under -race it is the decide's workers reading the build's facts and
// answers, and the write-through's encoding the session's entries, at once.
func TestEveryRestoreThatHoldsIsWrittenThroughOnEverySlot(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, holdingFiles())
	host := throughHost(test, root, test.TempDir())
	wide, wideSession := holdingBuild(test, root, host, wideSlots)
	narrow, narrowSession := holdingBuild(test, root, host, 1)

	var holding []string
	for index := range poolReaders {
		holding = append(holding, fmt.Sprintf("example.com/holding/hold%02d", index))
	}
	if !slices.Equal(wide.Restored, holding) || len(wide.Declined) > 0 {
		test.Errorf("windows restored %q from the host and declined %v, want every hold package restored and none "+
			"declined", wide.Restored, wide.Declined)
	}
	if !reflect.DeepEqual(wide, narrow) {
		test.Errorf("on %d slots windows restored\n%+v\nand on one\n%+v", wideSlots, wide, narrow)
	}
	for _, path := range holding {
		wideEntry, _ := throughEntry(test, wideSession, path)
		narrowEntry, _ := throughEntry(test, narrowSession, path)
		if !reflect.DeepEqual(wideEntry, narrowEntry) {
			test.Errorf("on %d slots windows wrote %s through as\n%+v\nand on one as\n%+v", wideSlots, path, wideEntry,
				narrowEntry)
		}
	}
}
