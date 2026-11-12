package main

import (
	"fmt"
	"os"
	"time"

	"github.com/system-inc/cohere/internal/streams"
)

// tokenRun is a token's run and what it has borrowed, rebalanced before each of its go commands (#fvbwfvh).
// Go's -p and `GOMAXPROCS` are fixed when a process starts, so a run's share can only change between its
// commands: one that started on a busy pool with one token widens once the pool goes idle, and one holding
// borrowed tokens gives them back at its next command when anyone is waiting.
type tokenRun struct {
	directory string
	shape     poolShape
	held      *heldSlot
	what      string
	borrowed  []*heldSlot
	stepped   bool
}

// step gives back what the run borrowed, borrows again from the pool as it is now (borrowTokens: all but one
// free token, and none while anyone waits), says when that changed the run's share, and returns the
// environment the next command runs in. name names the command in what it says.
func (run *tokenRun) step(name string) []string {
	before := len(run.borrowed)
	run.giveBack()
	run.borrowed = borrowTokens(run.directory, run.shape, run.held.number, run.what)
	wide := run.shape.widened(1 + len(run.borrowed))
	subject := "this run"
	if name != "" {
		subject = name
	}
	switch {
	case !run.stepped && len(run.borrowed) > 0:
		fmt.Fprintf(streams.Errors,
			"cohere-dev: the pool is idle, so %s borrows %s: %d packages at a time on %d threads\n",
			subject, tokenNumbers(run.borrowed), wide.packages, wide.threads)
	case run.stepped && len(run.borrowed) > before:
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: the pool went idle, so %s borrows %s: %d packages at a time on %d threads\n",
			subject,
			tokenNumbers(run.borrowed),
			wide.packages,
			wide.threads,
		)
	case run.stepped && len(run.borrowed) < before:
		why := "the pool is busy now"
		if quietNow, _ := quietHolds(run.directory, time.Now()); quietNow {
			why = "a quiet window opened"
		}
		fmt.Fprintf(
			streams.Errors,
			"cohere-dev: %s, so %s gives back %d borrowed tokens and runs %d packages at a time on %d threads\n",
			why,
			subject,
			before-len(run.borrowed),
			wide.packages,
			wide.threads,
		)
	}
	run.stepped = true
	return budgetEnvironment(os.Environ(), run.held.number, wide)
}

// giveBack releases every borrowed token.
func (run *tokenRun) giveBack() {
	for _, token := range run.borrowed {
		token.release()
	}
	run.borrowed = nil
}
