package allowed

// A break without a label leaves the innermost loop, which the reader can see.
func firstNegative(values []int) int {
	found := -1
	for index, value := range values {
		if value < 0 {
			found = index
			break
		}
	}
	return found
}

// A continue without a label goes back to the loop it stands in.
func positiveTotal(values []int) int {
	total := 0
	for _, value := range values {
		if value <= 0 {
			continue
		}
		total += value
	}
	return total
}

// The for around a select, moved into a function of its own, returns where it leaves.
func drain(messages <-chan string, done <-chan struct{}) []string {
	received := []string{}
	for {
		select {
		case message := <-messages:
			received = append(received, message)
		case <-done:
			return received
		}
	}
}

// A nested search in a function literal returns from the literal instead of breaking out of both loops.
func firstPair(rows [][]int, target int) (int, int) {
	find := func() (int, int) {
		for row := range rows {
			for column := range rows[row] {
				if rows[row][column] == target {
					return row, column
				}
			}
		}
		return -1, -1
	}
	return find()
}

// A named boolean the loop tests says in the open what a label would have hidden.
func anyNegativeRow(rows [][]int) bool {
	foundNegative := false
	for row := 0; row < len(rows) && !foundNegative; row++ {
		for _, value := range rows[row] {
			if value < 0 {
				foundNegative = true
				break
			}
		}
	}
	return foundNegative
}

// A break inside a select leaves only the select, with no label involved.
func tryReceive(messages <-chan string) string {
	received := ""
	select {
	case message := <-messages:
		if message == "" {
			break
		}
		received = message
	default:
	}
	return received
}
