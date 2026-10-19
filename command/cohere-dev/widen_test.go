//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// steppedGate is a land fixture whose go stands in for the gate's commands: each logs the share it was
// started with, and go vet holds until the test lets it go, so the test can change the pool between the
// gate's first command and its second.
type steppedGate struct {
	fixture        *landFixture
	log, release   string
	slots, waiting string
}

func newSteppedGate(t *testing.T) steppedGate {
	t.Helper()
	fixture := newLandFixture(t)
	bin := t.TempDir()
	gate := steppedGate{fixture: fixture, log: filepath.Join(bin, "log"), release: filepath.Join(bin, "release")}
	stand := "#!/bin/sh\necho \"$1 flags=$GOFLAGS threads=$GOMAXPROCS\" >> '" + gate.log + "'\n" +
		"[ \"$1\" = vet ] && while [ ! -e '" + gate.release + "' ]; do sleep 0.05; done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}
	fixture.environment = append(fixture.environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		tokensVariable+"=4", packagesVariable+"=2", threadsVariable+"=2")
	for _, variable := range fixture.environment {
		if home, found := strings.CutPrefix(variable, homeVariable+"="); found {
			gate.slots = filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
		}
	}
	return gate
}

func (gate steppedGate) line(t *testing.T, command string) string {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
		contents, _ := os.ReadFile(gate.log)
		for _, line := range strings.Split(string(contents), "\n") {
			if strings.HasPrefix(line, command+" ") {
				return line
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("waited a minute for go %s:\n%s", command, contents)
		}
	}
}

// A gate that took its token on a full pool runs go vet on its own share, and once the pool goes idle its
// tests run wider, borrowing all but one free token (#fvbwfvh). With the share fixed when the run took its
// token, the tests ran at the narrow share with three tokens idle.
func TestAGateWidensWhenThePoolGoesIdleBetweenItsCommands(t *testing.T) {
	t.Parallel()
	gate := newSteppedGate(t)
	held := []*heldSlot{}
	for range 3 {
		token, err := takeSlot(gate.slots, 4, "the test's run, filling the pool")
		if err != nil || token == nil {
			t.Fatalf("filling the pool: %v", err)
		}
		held = append(held, token)
	}
	landing := exec.Command(gate.fixture.wrapper, "land")
	landing.Dir, landing.Env = gate.fixture.worktree("a"), gate.fixture.environment
	var output strings.Builder
	landing.Stdout, landing.Stderr = &output, &output
	if err := landing.Start(); err != nil {
		t.Fatal(err)
	}
	if line := gate.line(t, "vet"); !strings.Contains(line, "flags=-p=2 -buildvcs=false threads=2") {
		t.Errorf("go vet on a full pool ran at %q, want the pool's own share", line)
	}
	for _, token := range held {
		token.release()
	}
	if err := os.WriteFile(gate.release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := landing.Wait(); err != nil {
		t.Fatalf("the landing failed: %v\n%s", err, &output)
	}
	if line := gate.line(t, "test"); !strings.Contains(line, "flags=-p=4 -buildvcs=false threads=3") {
		t.Errorf("the tests, once the pool went idle, ran at %q, want three tokens' share, 4 packages on 3 threads", line)
	}
	if !strings.Contains(output.String(), "the pool went idle, so the test run borrows") {
		t.Errorf("the gate did not say its tests widened:\n%s", &output)
	}
}

// A gate that borrowed on an idle pool gives the tokens back at its next command once someone waits, so the
// waiter starts then rather than after the whole gate, and the gate's tests run on its own share (#fvbwfvh).
func TestAGateGivesBorrowedTokensBackToAWaiterBetweenItsCommands(t *testing.T) {
	t.Parallel()
	gate := newSteppedGate(t)
	landing := exec.Command(gate.fixture.wrapper, "land")
	landing.Dir, landing.Env = gate.fixture.worktree("a"), gate.fixture.environment
	var output strings.Builder
	landing.Stdout, landing.Stderr = &output, &output
	if err := landing.Start(); err != nil {
		t.Fatal(err)
	}
	if line := gate.line(t, "vet"); !strings.Contains(line, "flags=-p=4 -buildvcs=false threads=3") {
		t.Errorf("go vet on an idle pool ran at %q, want three tokens' share", line)
	}
	// The one token borrowing left free goes to the test, so the run after it has to wait.
	last, err := takeSlot(gate.slots, 4, "the test's run, taking the token left free")
	if err != nil || last == nil {
		t.Fatalf("taking the free token: %v", err)
	}
	defer last.release()
	done := filepath.Join(filepath.Dir(gate.log), "waiter-ran")
	waiter := exec.Command(gate.fixture.wrapper, "exec", "--", "touch", done)
	waiter.Env = gate.fixture.environment
	if err := waiter.Start(); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Minute); len(waiters(gate.slots)) == 0; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the waiter never joined the line")
		}
	}
	if err := os.WriteFile(gate.release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := landing.Wait(); err != nil {
		t.Fatalf("the landing failed: %v\n%s", err, &output)
	}
	if err := waiter.Wait(); err != nil {
		t.Fatalf("the waiter failed: %v", err)
	}
	if _, err := os.Stat(done); err != nil {
		t.Error("the waiter never ran")
	}
	if line := gate.line(t, "test"); !strings.Contains(line, "flags=-p=2 -buildvcs=false threads=2") {
		t.Errorf("the tests, with someone waiting, ran at %q, want the gate's own share", line)
	}
	if !strings.Contains(output.String(), "the pool is busy now, so the test run gives back 2 borrowed tokens") {
		t.Errorf("the gate did not say it gave its borrowed tokens back:\n%s", &output)
	}
}
