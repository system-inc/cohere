package odd_slice_argument_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules/odd_slice_argument"
)

// SA5012's marks declare their order bound, and it bounds nothing, known: the marks are the definition's least set,
// which no order of judging changes (#8d9r2rh, demandWalk's doc), so a reader of the host's answers never lints a
// package whole for the marks' sake. The cycle order tests pin that the marks themselves are the same in every order.
func TestTheKindDeclaresItBoundsNoOrder(test *testing.T) {
	test.Parallel()
	if odd_slice_argument.Kind.OrderBound == nil {
		test.Fatal("the kind declares no order bound, so a reader takes every package it answers of as unknown")
	}
	if bound, known := odd_slice_argument.Kind.OrderBound(nil, nil); !known || len(bound) != 0 {
		test.Errorf("the kind's order bound is known %t and names %v, want known and nothing", known, bound)
	}
}
