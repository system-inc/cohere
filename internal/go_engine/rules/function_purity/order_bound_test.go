package function_purity_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules/function_purity"
)

// Purity declares its order bound, and it bounds nothing, known: its answers are the definition's least set, which no
// order of judging changes (#zva1zrc), so a reader of the host's answers never lints a package whole for purity's sake.
// TestPurityNeverTurnsOnWhatElseThePackageDeclares and TestAnInstanceAnswersAsItsDeclaredFunctionInEitherOrder pin that
// the verdicts themselves are the same in either order.
func TestTheKindDeclaresItBoundsNoOrder(test *testing.T) {
	test.Parallel()
	if function_purity.Kind.OrderBound == nil {
		test.Fatal("the kind declares no order bound, so a reader takes every package it answers of as unknown")
	}
	if bound, known := function_purity.Kind.OrderBound(nil, nil); !known || len(bound) != 0 {
		test.Errorf("the kind's order bound is known %t and names %v, want known and nothing", known, bound)
	}
}
