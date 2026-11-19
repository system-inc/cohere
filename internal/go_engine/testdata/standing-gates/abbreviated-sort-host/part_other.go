//go:build !windows

package gate

import "sort"

var _ sort.Interface = Items{}
