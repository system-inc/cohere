// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation_test

import (
	"fmt"
	"testing"
)

// TestDominatorOrder is go/ir's, building the package in memory where go/ir's loads it with go/packages.
func TestDominatorOrder(test *testing.T) {
	const source = `package p

func f(cond bool) {
	// (Print operands match BasicBlock IDs.)
	print(0)
	if cond {
		print(1)
	} else {
		print(2)
	}
	print(3)
}
`
	irPackage, _ := buildPackage(test, source, nil)
	functionF := irPackage.Function("f")

	if got, want := fmt.Sprint(functionF.DominatorPreorder()), "[0 1 2 3]"; got != want {
		test.Errorf("DominatorPreorder: got %v, want %s", got, want)
	}
	if got, want := fmt.Sprint(functionF.DominatorPostorder()), "[1 2 3 0]"; got != want {
		test.Errorf("DominatorPostorder: got %v, want %s", got, want)
	}
}
