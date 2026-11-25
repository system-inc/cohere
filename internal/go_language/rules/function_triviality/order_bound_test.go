package function_triviality_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_language/rules/function_triviality"
)

// Triviality declares its order bound, and it bounds nothing, known: each answer is one walk from the asked function
// with a seen set of its own, reading no other answer (#h0k644s), so a reader of the host's answers never lints a
// package whole for triviality's sake.
func TestTheKindDeclaresItBoundsNoOrder(test *testing.T) {
	test.Parallel()
	if function_triviality.Kind.OrderBound == nil {
		test.Fatal("the kind declares no order bound, so a reader takes every package it answers of as unknown")
	}
	if bound, known := function_triviality.Kind.OrderBound(nil, nil); !known || len(bound) != 0 {
		test.Errorf("the kind's order bound is known %t and names %v, want known and nothing", known, bound)
	}
}
