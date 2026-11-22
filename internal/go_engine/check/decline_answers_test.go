package check

import (
	"testing"
)

// The decide fixture's packages this test reads: platform, whose Counted is pure on the host and not on windows, an
// answer package reading Counted's purity, and a hold package, whose reads all hold on windows.
const (
	declinePlatformPath = "example.com/decide/platform"
	declineAnswerPath   = "example.com/decide/answer00"
	declineHoldPath     = "example.com/decide/hold00"
)

// A package check declines for the answers it read of other packages is counted by which of two causes declined it
// (#w8cg30b): an answer that reads differently in the build, a real platform difference, is `answers`, and its why
// names the answer, its kind and object; a record that could not name every answer it read, which no build can check,
// is `unnamedAnswers`. Over the decide fixture on windows, each answer package reads Counted's purity, which differs
// there, and a hold package, whose reads all hold, is declined only once its record says an answer it read went
// unnamed.
func TestADeclineForAnswersSaysWhetherOneDiffersOrWentUnnamed(test *testing.T) {
	test.Parallel()

	for _, step := range []struct {
		name string
		// unnamed is the package whose record is marked as unable to name an answer it read, empty for none.
		unnamed   string
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
			name:      "an answer that went unnamed",
			unnamed:   declineHoldPath,
			declined:  declineHoldPath,
			why:       unnamedAnswers,
			byAnswers: decideAnswerDeclined,
			byUnnamed: 1,
		},
	} {
		test.Run(step.name, func(test *testing.T) {
			test.Parallel()

			root := test.TempDir()
			writeFiles(test, root, decideFiles())
			host := throughHost(test, root, test.TempDir())
			if step.unnamed != "" {
				kept := host.entries[step.unnamed]
				if kept == nil {
					test.Fatalf("the host kept no entry of %s", step.unnamed)
				}
				// The record is read when the build first decodes the entry, so marking what it reads marks the
				// restore, as a record written by a run that could not name an answer is marked.
				written := kept.contents
				kept.contents = func() ([]byte, entryRecord, func() ([]byte, error), error) {
					body, record, readExportData, readError := written()
					record.ReadsIncomplete = true
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
