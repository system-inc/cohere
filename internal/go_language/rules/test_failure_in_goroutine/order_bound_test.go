package test_failure_in_goroutine

import (
	"testing"
)

// SA2002's stopping calls declare their order bound, and it bounds nothing, known: a function's stopping calls are
// read from its own instructions alone, following no call (#h0k644s), so a reader of the host's answers never lints a
// package whole for their sake.
func TestTheKindDeclaresItBoundsNoOrder(test *testing.T) {
	test.Parallel()
	if Kind.OrderBound == nil {
		test.Fatal("the kind declares no order bound, so a reader takes every package it answers of as unknown")
	}
	if bound, known := Kind.OrderBound(nil, nil); !known || len(bound) != 0 {
		test.Errorf("the kind's order bound is known %t and names %v, want known and nothing", known, bound)
	}
}
