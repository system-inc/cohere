package gate

import "example.test/gate/aliases"

func target(x []int) { x = aliases.Ints(x) }
