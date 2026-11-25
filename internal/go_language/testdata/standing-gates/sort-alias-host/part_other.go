//go:build !windows

package gate

import "example.test/gate/aliases"

func other(x aliases.Ints) { _ = x.Len() }
