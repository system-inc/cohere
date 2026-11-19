package return_nil_ness_test

import (
	"go/types"
	"maps"
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// requireOrderBound asks the nilness of fixture's function named asked, once, and fails unless the run replayed
// replays times, and the order bound of the package, over every function the resolver then holds an answer about, is
// known and names exactly want, in order.
func requireOrderBound(test *testing.T, fixture string, asked string, replays int64, want []string) {
	test.Helper()
	kept := askOnce(test, fixture, asked)
	if fallbacks := kept.counts.Fallbacks.Load(); fallbacks != replays {
		test.Errorf("asking of %s replayed %d times, want %d", asked, fallbacks, replays)
	}
	bound, known := return_nil_ness.OrderBound(kept.home, slices.Collect(maps.Values(kept.functions)))
	if !known {
		test.Fatalf("asked of %s, the order bound of %s is not known, though the run walked every answer it holds",
			asked, fixture)
	}
	var names []string
	for _, function := range bound {
		names = append(names, function.Name())
	}
	if !slices.Equal(names, want) {
		test.Errorf("asked of %s, the order bound of %s names %v, want %v", asked, fixture, names, want)
	}
}

// CycleOuter and CycleInner call each other, and CycleOuter, which allocates, says more than the defaults, so which
// one the push enters first decides what CycleInner returns. Asked of either, the order bound names both.
func TestOrderBoundNamesACycleAboveItsFloor(test *testing.T) {
	test.Parallel()
	for _, asked := range []string{"CycleOuter", "CycleInner"} {
		test.Run(asked, func(test *testing.T) {
			test.Parallel()
			requireOrderBound(test, "differences", asked, 1, []string{"CycleOuter", "CycleInner"})
		})
	}
}

// Ping and Pong call each other and both end at their floor, so every read of either answers the defaults in every
// order. Asked of either, the order bound names neither.
func TestOrderBoundNamesNoCycleAtItsFloor(test *testing.T) {
	test.Parallel()
	for _, asked := range []string{"Ping", "Pong"} {
		test.Run(asked, func(test *testing.T) {
			test.Parallel()
			requireOrderBound(test, "ordered/floored", asked, 1, nil)
		})
	}
}

// Chain calls only itself, and is never nil. It reads itself begun in every order, so though it says more than the
// defaults, the order bound does not name it, and no replay ran.
func TestOrderBoundNamesNoFunctionCallingOnlyItself(test *testing.T) {
	test.Parallel()
	requireOrderBound(test, "ordered/alone", "Chain", 0, nil)
}

// A function read through a stand-in, as the replay reads it, and saying more than the defaults is named: Make,
// generic, read through Make[int] by Use, declared before it, and Fresh, a method, read through its bound method
// wrapper by Wrapped. The functions reading them are not named: what they read may differ, which the bound says by
// naming what they read.
func TestOrderBoundNamesAStandInAboveItsFloor(test *testing.T) {
	test.Parallel()
	for _, read := range []struct {
		fixture string
		asked   string
		want    string
	}{
		{fixture: "ordered/instantiated", asked: "Use", want: "Make"},
		{fixture: "ordered/wrapped", asked: "Wrapped", want: "Fresh"},
	} {
		test.Run(read.fixture, func(test *testing.T) {
			test.Parallel()
			requireOrderBound(test, read.fixture, read.asked, 1, []string{read.want})
		})
	}
}

// Pass, generic and at its floor, read through Pass[int] by Loop inside the replay a question of Loop makes, answers
// the defaults whether judged or not, and Loop and Back, the cycle that made the replay, end at their floor too, so the
// order bound names none of them.
func TestOrderBoundNamesNoStandInAtItsFloor(test *testing.T) {
	test.Parallel()
	requireOrderBound(test, "ordered/passed", "Loop", 1, nil)
}

// The order bound is known only when the kind's state holds a verdict of every function the caller holds an answer
// about. Asked of Inner over the unbuilt fixture, the replay stops at Inner, so it never judges After: over the answers
// the resolver holds the bound is known, and names Outer and Inner, a cycle with Outer above its floor; with After
// among them, as an answer from the store would put it, the bound is not known.
func TestOrderBoundIsNotKnownWithoutAVerdictOfEveryAnswer(test *testing.T) {
	test.Parallel()
	requireOrderBound(test, "unbuilt", "Inner", 1, []string{"Outer", "Inner"})
	kept := askOnce(test, "unbuilt", "Inner")
	after, isFunction := kept.home.Types.Scope().Lookup("After").(*types.Func)
	if !isFunction {
		test.Fatal("the unbuilt fixture declares no function After")
	}
	held := append(slices.Collect(maps.Values(kept.functions)), after)
	if bound, known := return_nil_ness.OrderBound(kept.home, held); known {
		test.Errorf("with After, never judged, among the answers held, the order bound is known, naming %v", bound)
	}
}

// The kind declares its order bound to whoever reads its answers (fact_resolver.Kind.OrderBound), as OrderBound
// computes it: asked of CycleOuter, a cycle above its floor, the kind's hook names CycleOuter and CycleInner.
func TestTheKindDeclaresItsOrderBound(test *testing.T) {
	test.Parallel()
	if return_nil_ness.Kind.OrderBound == nil {
		test.Fatal("the kind declares no order bound, so a reader takes every package it answers of as unknown")
	}
	kept := askOnce(test, "differences", "CycleOuter")
	bound, known := return_nil_ness.Kind.OrderBound(kept.home, slices.Collect(maps.Values(kept.functions)))
	var names []string
	for _, function := range bound {
		names = append(names, function.Name())
	}
	if want := []string{"CycleOuter", "CycleInner"}; !known || !slices.Equal(names, want) {
		test.Errorf("the kind's order bound is known %t and names %v, want known and %v", known, names, want)
	}
}
