//go:build !windows

package gate

import "sort"

func other(x sort.IntSlice) { _ = x.Len() }
