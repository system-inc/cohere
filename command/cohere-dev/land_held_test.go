//go:build unix

package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A landing a quiet window holds at its gate's token says it was held, not that its gate failed, and exits 75, try
// again later, so a script tells a held landing from a red one (#vq5b7fs). Before, it read "the gate failed, so
// nothing landed", which reads as a red test to anyone skimming the land line. Both tokens of the fixture's pool
// are held, so the landing queues for one; the window opens while it waits; the tokens go free, and the wall refuses
// it as it takes one, before its gate runs anything.
func TestALandingAQuietWindowHoldsSaysHeldNotFailed(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	home := ""
	for _, variable := range fixture.environment {
		if value, found := strings.CutPrefix(variable, homeVariable+"="); found {
			home = value
		}
	}
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	held := []*heldSlot{}
	release := func() {
		for _, token := range held {
			token.release()
		}
		held = nil
	}
	defer release()
	for range 2 {
		token, takeSlotError := takeSlot(slots, 2, "a gate running when the window opened")
		if takeSlotError != nil || token == nil {
			test.Fatalf("taking a token for the test: %v", takeSlotError)
		}
		held = append(held, token)
	}
	landing := exec.Command(fixture.wrapper, "land")
	landing.Dir, landing.Env = fixture.worktree("a"), fixture.environment
	var output strings.Builder
	landing.Stdout, landing.Stderr = &output, &output
	if startError := landing.Start(); startError != nil {
		test.Fatal(startError)
	}
	for deadline := time.Now().Add(time.Minute); len(waiters(slots)) == 0; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			// Ignored: the landing is stopped because the test is failing; its error says nothing more.
			_ = landing.Process.Kill()
			test.Fatalf("waited a minute for the landing to queue for a token:\n%s", output.String())
		}
	}
	if code := quietOn(
		slots,
		[]string{"--for", "10m", "a measurement"},
		time.Now(),
		discarded(),
		discarded(),
	); code != 0 {
		test.Fatalf("opening a quiet window exited %d", code)
	}
	release()
	waitError := landing.Wait()
	var exited *exec.ExitError
	if !errors.As(waitError, &exited) || exited.ExitCode() != quietRefusedExitCode {
		test.Errorf("the held landing ended with %v, want exit %d:\n%s", waitError, quietRefusedExitCode,
			output.String())
	}
	if !strings.Contains(output.String(), "held by the quiet window, so nothing landed") ||
		strings.Contains(output.String(), "the gate failed") {
		test.Errorf("the held landing did not say it was held, or said its gate failed:\n%s", output.String())
	}
	if gates := readMaybeMissing(test, fixture.log); len(gates) > 0 {
		test.Errorf("the gate ran inside the window:\n%s", gates)
	}
}
