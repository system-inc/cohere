package nexus

import (
	"sort"
	"strconv"
	"strings"
)

// correctnessNoProcessExitAfterOutputState is what one path carries: the chains of the writes made on
// it that a `catch` could still take back, kept minimal. A chain that is a subset of another is
// forgotten by every `catch` the other is, so the larger one is dropped; the empty chain, `,`, is a
// write no `catch` takes back, and once a path holds it the path has written for good.
type correctnessNoProcessExitAfterOutputState []string

func (state correctnessNoProcessExitAfterOutputState) with(chain string) correctnessNoProcessExitAfterOutputState {
	next := correctnessNoProcessExitAfterOutputState{}
	for _, held := range state {
		if correctnessNoProcessExitAfterOutputChainHolds(chain, held) {
			// Something already held is forgotten only where this one is too.
			return state
		}
		if !correctnessNoProcessExitAfterOutputChainHolds(held, chain) {
			next = append(next, held)
		}
	}
	next = append(next, chain)
	sort.Strings(next)
	return next
}

func (state correctnessNoProcessExitAfterOutputState) entering(try int) correctnessNoProcessExitAfterOutputState {
	member := "," + strconv.Itoa(try) + ","
	next := correctnessNoProcessExitAfterOutputState{}
	for _, held := range state {
		if !strings.Contains(held, member) {
			next = append(next, held)
		}
	}
	return next
}

func (state correctnessNoProcessExitAfterOutputState) key() string {
	return strings.Join(state, "|")
}
