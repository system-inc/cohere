package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestAReplaysFooterIsTheReplays is the replay's footer, golden: the recorded run's findings and counts,
// with the replay's own time, every file answered by the cache and nothing checked fresh or rewritten.
// The recorded run took 4.7s; a footer saying so would claim a run the replay did not do.
func TestAReplaysFooterIsTheReplays(t *testing.T) {
	recorded := cleanSummary()
	recorded.Total = 4700 * time.Millisecond
	recorded.Findings = 2
	recorded.Changed = []changedFile{{Path: "a.ts", Formatted: true}}
	recorded.Gaps.CrashedFiles = 1

	replayed := replayedSummary(recorded, 50*time.Millisecond)
	got := footer(replayed, plain, footerOptions{})
	want := "✗ ☠️ 0.05s • 2 findings (480 rules • 3.9K cached) • ⚠ 1 file crashed"
	if got != want {
		t.Errorf("the replay's footer:\n got  %s\n want %s", got, want)
	}
	if err := isTheReplaysFooter(got, recorded); err != nil {
		t.Error(err)
	}
	if verbose := footer(replayed, plain, footerOptions{Verbose: true}); !strings.Contains(verbose, "• replayed") {
		t.Errorf("--verbose's replay footer does not say it was replayed: %s", verbose)
	}

	// The mutant replays the recorded run's own footer, as reprinting the recording would.
	mutant := footer(recorded, plain, footerOptions{})
	if isTheReplaysFooter(mutant, recorded) == nil {
		t.Errorf("a replay reprinting the recorded footer passed: %s", mutant)
	}
}

// isTheReplaysFooter says why a footer is not a replay's: the recorded run's time, a file checked fresh,
// or a file rewritten, none of which a replay does.
func isTheReplaysFooter(footerLine string, recorded runSummary) error {
	if strings.Contains(footerLine, footerSeconds(recorded.Total)) {
		return fmt.Errorf("the footer carries the recorded run's time, %s: %s", footerSeconds(recorded.Total), footerLine)
	}
	if strings.Contains(footerLine, " checked") {
		return fmt.Errorf("the footer says files were checked fresh: %s", footerLine)
	}
	if strings.Contains(footerLine, " cohered") {
		return fmt.Errorf("the footer says files were rewritten: %s", footerLine)
	}
	return nil
}
