// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// assert panics with message when x is false.
func assert(x bool, message string) {
	if !x {
		// Invariant: callers assert only what holds by construction: buildFunction and done run only on
		// top-level functions, and the type of a ~x term is its own underlying type.
		panic(message)
	}
}
