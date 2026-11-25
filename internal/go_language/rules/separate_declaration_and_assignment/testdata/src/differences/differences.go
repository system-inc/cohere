package differences

// clauses holds the declarations the original misses: a switch's, a type switch's and a select's clause holds a list
// of statements of its own, no block, which the original never walks. Each finding is the port's alone, and a block
// nested in a clause, which both walk, is both's. Each of the port's fixes is the one the original offers in a block,
// the declaration merged with the assignment after it, which reads the same in a clause.
func clauses(choice int, unknown interface{}, channel chan int) {
	switch choice {
	case 1:
		// The original walks blocks only, and a case clause is none.
		var switched int // port finds: should merge variable declaration with assignment on next line // port fixes: var switched int = 1
		switched = 1
		_ = switched
	default:
		// The same of the default clause.
		var defaulted string // port finds: should merge variable declaration with assignment on next line // port fixes: var defaulted string = "default"
		defaulted = "default"
		_ = defaulted
	}

	switch typed := unknown.(type) {
	case int:
		// The same of a type switch's clause, whose value is the clause's own.
		var doubled int // port finds: should merge variable declaration with assignment on next line // port fixes: var doubled int = typed * 2
		doubled = typed * 2
		_ = doubled
	}

	select {
	case received := <-channel:
		// The same of a select's clause.
		var selected int // port finds: should merge variable declaration with assignment on next line // port fixes: var selected int = received
		selected = received
		_ = selected
	default:
	}

	// Each clause's count runs over that clause alone: the first clause's variable is assigned once there, so it is
	// found, while the next clause's, spelled alike, is assigned twice in its own clause and left alone.
	switch choice {
	case 2:
		var shared int // port finds: should merge variable declaration with assignment on next line // port fixes: var shared int = 1
		shared = 1
		_ = shared
	case 3:
		var shared int
		shared = 1
		shared = 2
		_ = shared
	}

	switch {
	case choice > 1:
		// Assigned again in the clause, so neither reports it, the port counting over the clause.
		var again int
		again = 1
		again = 2
		_ = again

		// Assigned again in a block nested in the clause, which the port's count reaches.
		var nested int
		nested = 1
		if choice > 2 {
			nested = 2
		}
		_ = nested

		// A block nested in a clause is a block, which both walk.
		{
			var blocked int
			blocked = 1
			_ = blocked
		}
	}
}

// shadowed holds a variable whose name another scope declares too, each counted by its own object: the inner one is
// assigned once, so both report it, though the outer one, spelled alike, is assigned twice.
func shadowed() {
	var name int
	name = 1
	name = 2
	_ = name
	{
		var name int
		name = 3
		_ = name
	}
}
