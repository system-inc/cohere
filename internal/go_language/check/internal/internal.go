// cohere:copied golang.org/x/tools@v0.50.0 go/analysis/internal/internal.go
// cohere:until the copies of go/analysis/checker and go/analysis/analysistest
// beside this package both go away, since the one sets ActionPass and the
// other reads it.
//
// cohere: an internal package of x/tools cannot be imported from this module,
// so the two copies share their own copy of the backdoor upstream's share.
// The code is unchanged.

// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package internal

import "golang.org/x/tools/go/analysis"

// This function is set by the checker package to provide
// backdoor access to the private Pass field
// of the *checker.Action type, for use by analysistest.
//
// It may return nil, for example if the action was not
// executed because of a failed dependent.
var ActionPass func(action any) *analysis.Pass
