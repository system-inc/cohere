package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A run widened by borrowed tokens splits their cores between packages and threads as evenly as they divide,
// packages the larger, and a run held to one token keeps the pool's own shape, whatever it is.
func TestAWidenedShapeSplitsTheCoresEvenly(t *testing.T) {
	t.Parallel()
	shape := poolShape{tokens: 4, packages: 2, threads: 2}
	for tokens, want := range map[int]poolShape{
		1: {tokens: 4, packages: 2, threads: 2},
		2: {tokens: 4, packages: 4, threads: 2},
		3: {tokens: 4, packages: 4, threads: 3},
		4: {tokens: 4, packages: 4, threads: 4},
	} {
		if got := shape.widened(tokens); got != want {
			t.Errorf("widened to %d tokens is %+v, want %+v", tokens, got, want)
		}
	}
	odd := poolShape{tokens: 2, packages: 4, threads: 3}
	if got := odd.widened(1); got != odd {
		t.Errorf("one token's shape changed: %+v, want the pool's own %+v", got, odd)
	}
}

// A run that finds the pool idle borrows every other free token but one, runs that much wider, and names the
// borrowed tokens in status; the token it leaves lets the next run start at once, at the pool's own shape.
// Without borrowing the first run ran at 2 packages on 2 threads with three tokens idle (2026-10-19).
func TestALoneRunBorrowsAllButOneFreeToken(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("tokens are a Unix lock")
	}
	wrapper := filepath.Join(t.TempDir(), "cohere-dev")
	if output, err := exec.Command("go", "build", "-o", wrapper, ".").CombinedOutput(); err != nil {
		t.Fatalf("building: %v\n%s", err, output)
	}
	// A go that logs its share as it starts, and holds the first run until the test releases it.
	bin := t.TempDir()
	log := filepath.Join(bin, "log")
	release := filepath.Join(bin, "release")
	stand := "#!/bin/sh\necho \"$COHERE_DEV_TEST_RUN token=$" + heldTokenVariable + " flags=$GOFLAGS threads=$GOMAXPROCS cores=$" +
		coresVariable + "\" >> '" + log + "'\n" +
		"[ \"$COHERE_DEV_TEST_RUN\" = first ] && while [ ! -e '" + release + "' ]; do sleep 0.05; done\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(stand), 0o755); err != nil {
		t.Fatal(err)
	}
	home := privatePool(t)
	start := func(name string) (*exec.Cmd, *strings.Builder) {
		var output strings.Builder
		command := exec.Command(wrapper, "build", "./command/cohere")
		command.Env = append(outsideThePool(os.Environ()), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			homeVariable+"="+home, tokensVariable+"=4", packagesVariable+"=2", threadsVariable+"=2", "COHERE_DEV_TEST_RUN="+name)
		command.Stdout, command.Stderr = &output, &output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		return command, &output
	}
	logged := func(name string) string {
		contents, _ := os.ReadFile(log)
		for _, line := range strings.Split(string(contents), "\n") {
			if strings.HasPrefix(line, name+" ") {
				return line
			}
		}
		return ""
	}
	waitFor := func(name string) string {
		t.Helper()
		for deadline := time.Now().Add(time.Minute); ; time.Sleep(20 * time.Millisecond) {
			if line := logged(name); line != "" {
				return line
			}
			if time.Now().After(deadline) {
				t.Fatalf("the %s run never reached go", name)
			}
		}
	}

	first, firstOutput := start("first")
	if line := waitFor("first"); !strings.Contains(line, "flags=-p=4 threads=3 cores=12") {
		t.Errorf("a lone run on an idle four-token pool ran with %q, want three tokens' share: -p=4, 3 threads, 12 cores", line)
	}
	status := exec.Command(wrapper, "status")
	status.Env = append(outsideThePool(os.Environ()), homeVariable+"="+home, tokensVariable+"=4")
	output, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("cohere-dev status: %v\n%s", err, output)
	}
	if borrowed := strings.Count(string(output), "borrowed by token 1's run"); borrowed != 2 || strings.Count(string(output), "token 4: free") != 1 {
		t.Errorf("status shows %d borrowed tokens, want 2 borrowed and 1 free:\n%s", borrowed, output)
	}

	// The token left free: the next run starts while the first still runs, at the pool's own shape.
	second, secondOutput := start("second")
	if line := waitFor("second"); !strings.Contains(line, "flags=-p=2 threads=2 cores=4") {
		t.Errorf("the run after a borrower ran with %q, want one token's share", line)
	}
	if err := second.Wait(); err != nil {
		t.Errorf("the second run failed: %v\n%s", err, secondOutput)
	}
	if strings.Contains(secondOutput.String(), "waiting for a token") {
		t.Errorf("the second run waited behind a borrower:\n%s", secondOutput)
	}
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Errorf("the first run failed: %v\n%s", err, firstOutput)
	}
	if !strings.Contains(firstOutput.String(), "borrows tokens 2 and 3") {
		t.Errorf("the borrowing run did not say what it borrowed:\n%s", firstOutput)
	}
	slots := filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots")
	for token := 1; token <= 4; token++ {
		if holder := heldBy(slots, token); holder != "" {
			t.Errorf("token %d is still held after every run ended: %s", token, holder)
		}
	}
}

// Nothing is borrowed while anyone waits in line, or when only one token is free: those tokens are the line's.
func TestNothingIsBorrowedWithAWaiterOrOneFreeToken(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("tokens are a Unix lock")
	}
	shape := poolShape{tokens: 4, packages: 2, threads: 2}

	waiting := t.TempDir()
	own, err := takeSlot(waiting, 4, "the run that would borrow")
	if err != nil || own == nil {
		t.Fatalf("taking a token: %v", err)
	}
	defer own.release()
	ticket, err := joinQueue(waiting)
	if err != nil {
		t.Fatal(err)
	}
	defer ticket.leave()
	if borrowed := borrowTokens(waiting, shape, own.number, "a run"); len(borrowed) != 0 {
		t.Errorf("a run borrowed %d tokens with a waiter in line", len(borrowed))
		for _, token := range borrowed {
			token.release()
		}
	}

	crowded := t.TempDir()
	held := []*heldSlot{}
	for range 3 {
		token, err := takeSlot(crowded, 4, "a run")
		if err != nil || token == nil {
			t.Fatalf("taking a token: %v", err)
		}
		held = append(held, token)
	}
	defer func() {
		for _, token := range held {
			token.release()
		}
	}()
	if borrowed := borrowTokens(crowded, shape, held[0].number, "a run"); len(borrowed) != 0 {
		t.Errorf("a run borrowed the one free token, %d of them, leaving none for the next run", len(borrowed))
		for _, token := range borrowed {
			token.release()
		}
	}
}
