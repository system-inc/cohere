package check

import (
	"testing"
)

// The decide fixture's packages this test reads: platform, whose Counted is pure on the host and not on windows, an
// answer package reading Counted's purity, and a hold package, whose reads all hold on windows; and a package neither
// build holds, which a planted read names as its owner.
const (
	declinePlatformPath = "example.com/decide/platform"
	declineAnswerPath   = "example.com/decide/answer00"
	declineHoldPath     = "example.com/decide/hold00"
	declineGonePath     = "example.com/decide/gone"
)

// A package check declines for the answers it read of other packages is counted by which of two causes declined it
// (#w8cg30b): an answer that reads differently in the build, a real platform difference, is `answers`, and its why
// names the answer, its kind and object; a record that could not name every answer it read, which no build can check,
// is `unnamedAnswers`. Over the decide fixture on windows, each answer package reads Counted's purity, which differs
// there, and a hold package, whose reads all hold, is declined only once its record says an answer it read went
// unnamed, or names an owner the build has no package of.
func TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		name string
		// marked is the package whose host record mark rewrites, empty for none.
		marked    string
		mark      func(record *entryRecord)
		declined  string
		why       string
		byAnswers int
		byUnnamed int
	}{
		{
			name:      "an answer that differs",
			declined:  declineAnswerPath,
			why:       "A answer function_purity of " + declinePlatformPath + ".Counted differs in the build",
			byAnswers: decideAnswerDeclined,
		},
		{
			name:   "an answer that went unnamed",
			marked: declineHoldPath,
			// As a record written by a run that could not name an answer is marked.
			mark:      func(record *entryRecord) { record.ReadsIncomplete = true },
			declined:  declineHoldPath,
			why:       unnamedAnswers,
			byAnswers: decideAnswerDeclined,
			byUnnamed: 1,
		},
		{
			// Planted, since the pipeline cannot reach it: a package reads answers only of objects it names, whose
			// counterparts TypesHold finds in the build by path, so one reading an owner the build lacks declines at
			// load.
			name:   "an answer of a package the build lacks",
			marked: declineHoldPath,
			mark: func(record *entryRecord) {
				record.Reads = append(record.Reads, storedRead{Kind: "function_purity", Owner: declineGonePath,
					Path: "Gone"})
			},
			declined: declineHoldPath,
			why: "A the build has no package " + declineGonePath + ", whose answer function_purity of " +
				declineGonePath + ".Gone it read",
			byAnswers: decideAnswerDeclined + 1,
		},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			root := test.TempDir()
			writeFiles(test, root, decideFiles())
			host := throughHost(test, root, test.TempDir())
			if step.marked != "" {
				kept := host.entries[step.marked]
				if kept == nil {
					test.Fatalf("the host kept no entry of %s", step.marked)
				}
				// The record is read when the build first decodes the entry, so marking what it reads marks the
				// restore.
				written := kept.contents
				kept.contents = func() ([]byte, entryRecord, func() ([]byte, error), error) {
					body, record, readExportData, readError := written()
					step.mark(&record)
					return body, record, readExportData, readError
				}
			}
			fromHost := decideBuild(test, root, host, 1)
			if why := fromHost.Declined[step.declined]; why != step.why {
				test.Errorf("windows declined %s for %q, want %q", step.declined, why, step.why)
			}
			if fromHost.DeclinedBy["answers"] != step.byAnswers ||
				fromHost.DeclinedBy["unnamedAnswers"] != step.byUnnamed {
				test.Errorf("windows declined %v by reason, want %d by answers and %d by unnamedAnswers",
					fromHost.DeclinedBy, step.byAnswers, step.byUnnamed)
			}
		})
	}
}

// unaskedFiles are a module whose platform package's Counted is pure in every build, thrice its value on the host and
// lower's Triple of it on windows, and a reader package calling Counted, the same in both, whose restore reads
// Counted's purity. Only windows's Counted calls into lower, so the host never asks Triple's purity.
var unaskedFiles = map[string]string{
	"go.mod": "module example.com/unasked\n\ngo 1.24\n",
	"lower/lower.go": "// Package lower triples.\npackage lower\n\n" +
		"// Triple is thrice value.\nfunc Triple(value int) int {\n\treturn value * 3\n}\n",
	"platform/platform.go": "// Package platform is declared each build's own way.\npackage platform\n",
	"platform/platform_other.go": "//go:build !windows\n\npackage platform\n\n" +
		"// Counted is thrice value.\nfunc Counted(value int) int {\n\treturn value * 3\n}\n",
	"platform/platform_windows.go": "package platform\n\nimport \"example.com/unasked/lower\"\n\n" +
		"// Counted is thrice value, as lower triples it.\nfunc Counted(value int) int {\n" +
		"\treturn lower.Triple(value)\n}\n",
	"reader/reader.go": "// Package reader reads Counted.\npackage reader\n\n" +
		"import \"example.com/unasked/platform\"\n\n// Thrice is thrice 1.\nfunc Thrice() int {\n\treturn platform.Counted(1)\n}\n",
}

// A package check declines because the build has no answer to ask of one it read is counted under `answers`, and its
// why says the build had none, apart from one that differs (#w8cg30b). The host writes lower's entry with only the
// answers its questions computed in it, as an entry from before format 10 holds them, and never asks Triple's purity,
// so windows, analyzing Counted from source, reaches lower restored with no answer of Triple: Counted's purity is
// unsure there, and reader's read of it has nothing to be checked against.
func TestADeclineForAnAnswerTheBuildCannotAskSaysSo(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	writeFiles(test, root, unaskedFiles)
	rules := throughRules()
	caching := cachingAt(test.TempDir(), rules)
	caching.AnswersInPart = map[string]bool{"example.com/unasked/lower": true}
	hosted, _ := hostSessionWith(test, root, caching, rules, nil)
	fromHost := decideBuild(test, root, hosted.Host, 1)
	const want = "A the build has no answer function_purity of example.com/unasked/platform.Counted to ask"
	if why := fromHost.Declined["example.com/unasked/reader"]; why != want {
		test.Errorf("windows declined reader for %q, want %q", why, want)
	}
	if fromHost.DeclinedBy["answers"] != 1 || fromHost.DeclinedBy["unnamedAnswers"] != 0 {
		test.Errorf("windows declined %v by reason, want 1 by answers and none by unnamedAnswers", fromHost.DeclinedBy)
	}
}
