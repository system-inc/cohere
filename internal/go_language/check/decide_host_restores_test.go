package check

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/bounded"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// The decide's fixture's counts: packages whose restore holds, and packages declined at check by a fact and by an
// answer, enough of each that several of the decide's workers run and both outcomes land at once.
const (
	decideHolding        = 16
	decideFactDeclined   = 8
	decideAnswerDeclined = 8
)

// decideFiles are a module of a platform package whose files differ by build, as os's do, and the packages above it:
// each hold package calls Double, declared alike in each build's own file and pure in every build, so its restore is
// judged and holds; each fact package hands Logf, which forwards its format on the host and not on windows, a format
// its argument does not fit, so the printf wrapper fact it read declines it; and each answer package calls Counted,
// pure on the host and not on windows, so the answer of its purity it read declines it.
func decideFiles() map[string]string {
	// Double is declared alike in each build's own file, so a package reading it reads a file that differs by build
	// and is visited and judged, never left untouched by the walk.
	double := "// Double is twice value.\nfunc Double(value int) int {\n\treturn value * 2\n}\n"
	files := map[string]string{
		"go.mod": "module example.com/decide\n\ngo 1.24\n",
		"platform/platform.go": "// Package platform is declared alike in every build and each build's own way.\n" +
			"package platform\n",
		"platform/platform_other.go": "//go:build !windows\n\npackage platform\n\nimport \"fmt\"\n\n" +
			"// Logf formats as fmt.Sprintf does.\nfunc Logf(format string, arguments ...any) string {\n" +
			"\treturn fmt.Sprintf(format, arguments...)\n}\n\n" +
			"// Counted is thrice value.\nfunc Counted(value int) int {\n\treturn value * 3\n}\n\n" + double,
		"platform/platform_windows.go": "package platform\n\n" +
			"// Logf is its format, its arguments left alone.\nfunc Logf(format string, arguments ...any) string {\n" +
			"\treturn format + string(rune(len(arguments)))\n}\n\n" +
			"// calls counts Counted's calls.\nvar calls int\n\n" +
			"// Counted is thrice value, counting the call.\nfunc Counted(value int) int {\n" +
			"\tcalls++\n\treturn value * 3\n}\n\n" + double,
	}
	above := func(prefix string, count int, body string) {
		for index := range count {
			name := fmt.Sprintf("%s%02d", prefix, index)
			files[name+"/"+name+".go"] = "package " + name + "\n\nimport \"example.com/decide/platform\"\n\n" +
				fmt.Sprintf(body, index) + "\n"
		}
	}
	above("hold", decideHolding, "// Twice is twice %d.\nfunc Twice() int { return platform.Double(%[1]d) }")
	above("fact", decideFactDeclined, "// Line logs %d badly.\nfunc Line() string { return platform.Logf(\"%%d\", "+
		"\"text\") }")
	above("answer", decideAnswerDeclined, "// Thrice is thrice %d.\nfunc Thrice() int { "+
		"return platform.Counted(%[1]d) }")
	return files
}

// decideBuild is windows's run over root, restoring from host, with no store, as under `--no-cache`, and its work on
// a pool of slots, as the engine's builds share one, loaded again until no reload is asked, with what it restored from
// the host, its time blanked, since it is the one count that differs between two runs. With no store nothing is
// written through, so the reload restores again from the host what held at the first load, rather than hit it in the
// build's own store, and its counts, the last load's, name it.
func decideBuild(test *testing.T, root string, host *HostRun, slots int) HostRestoreCounts {
	test.Helper()
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.Store = nil
	// The build's keys hold its target, as the engine's do, so they never meet the host's.
	caching.Context.Build = append(caching.Context.Build, "GOOS=windows")
	caching.Pool = bounded.NewPool(slots)
	session := NewCacheSession(caching, rules)
	session.RestoreFromHost(host)
	for {
		loaded, loadError := load.PackagesCached(root, []string{"./..."}, []string{"GOWORK=off", "GOOS=windows"},
			nil, load.Caching{Restorer: session})
		if loadError != nil || len(loaded.Diagnostics) > 0 {
			test.Fatalf("loading windows's build on %d slots: %v %+v", slots, loadError, loaded.Diagnostics)
		}
		result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
			Bodiless: loaded.Bodiless})
		if runError != nil {
			test.Fatalf("running windows's build on %d slots: %v", slots, runError)
		}
		// A round that asks a reload is dropped, as the pipeline drops it, and its crashes with it: a package handed
		// a placeholder for a missing answer is one (Run), which the reload's round answers from source.
		if len(result.Reloads) == 0 && len(result.Crashes) > 0 {
			test.Fatalf("running windows's build on %d slots: %+v", slots, result.Crashes)
		}
		if len(result.Reloads) == 0 || !session.Reload(result.Reloads) {
			fromHost := result.FromHost
			fromHost.Deciding = 0
			return fromHost
		}
	}
}

// An extra build decides its restores from the host on every core at once and writes what they share in one order
// (#9fer4tx): over a module with sixteen packages whose restore holds and sixteen its check declines, eight by a fact
// and eight by an answer, windows restores the same packages and declines the same ones for the same reasons, counted
// the same, on one slot as on eight. Under -race it is the decide's workers asking the build's facts and answers at
// once: a memo of the facts written without its once, or a count written from a worker, races. The order the decide
// writes in is seen by no result: every count and reason it writes is a map or a sum, and Run sorts the reloads again
// (check.go), so dropping its sorts leaves this green, and what pins the order is that the workers share no write,
// which those two races show.
func TestTheHostRestoresDecidedOnEveryCoreAreTheOnesDecidedOnOne(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, decideFiles())
	host := throughHost(test, root, test.TempDir())
	wide := decideBuild(test, root, host, 8)
	narrow := decideBuild(test, root, host, 1)

	var holding []string
	for index := range decideHolding {
		holding = append(holding, fmt.Sprintf("example.com/decide/hold%02d", index))
	}
	if !slices.Equal(wide.Restored, holding) {
		test.Errorf("windows restored %q from the host and left %q untouched, want every hold package restored and "+
			"nothing else", wide.Restored, wide.Untouched)
	}
	declined := map[string]string{}
	for index := range decideFactDeclined {
		declined[fmt.Sprintf("example.com/decide/fact%02d", index)] = "F "
	}
	for index := range decideAnswerDeclined {
		declined[fmt.Sprintf("example.com/decide/answer%02d", index)] = "A "
	}
	for identifier, row := range declined {
		if why := wide.Declined[identifier]; !strings.HasPrefix(why, row) {
			test.Errorf("windows declined %s for %q, want a reason of row %q", identifier, why, strings.TrimSpace(row))
		}
	}
	if wide.DeclinedAtCheck != len(declined) || wide.ReloadRounds != 1 {
		test.Errorf("windows declined %d at its check over %d reloads, want %d over one", wide.DeclinedAtCheck,
			wide.ReloadRounds, len(declined))
	}
	if !reflect.DeepEqual(wide, narrow) {
		test.Errorf("on eight slots windows restored\n%+v\nand on one\n%+v", wide, narrow)
	}
}
