package flagged

// A labeled for that a nested loop continues.
func firstPair(rows [][]int, target int) (int, int) {
	found := -1
outer: // want "`outer` is a label, and a jump to it lands where a reader is not looking"
	for row := 0; row < len(rows); row++ {
		for column := range rows[row] {
			if rows[row][column] == target {
				found = column
				break outer
			}
			if rows[row][column] < 0 {
				continue outer
			}
		}
	}
	return found, 0
}

// A labeled range.
func anyNegative(values []int) bool {
	negative := false
scan: // want "`scan` is a label"
	for _, value := range values {
		if value < 0 {
			negative = true
			break scan
		}
	}
	return negative
}

// The common Go shape: a for around a select whose case breaks out of both.
func drain(messages <-chan string, done <-chan struct{}) []string {
	received := []string{}
loop: // want "`loop` is a label"
	for {
		select {
		case message := <-messages:
			received = append(received, message)
		case <-done:
			break loop
		}
	}
	return received
}

// A labeled select, left from inside a case by its own label.
func tryReceive(messages <-chan string) string {
	received := ""
waiting: // want "`waiting` is a label"
	select {
	case message := <-messages:
		if message == "" {
			break waiting
		}
		received = message
	default:
	}
	return received
}

// A labeled switch, which a plain break would leave too, so the label adds a jump and nothing else.
func classify(value int) string {
	kind := "other"
choice: // want "`choice` is a label"
	switch {
	case value < 0:
		kind = "negative"
		break choice
	case value == 0:
		kind = "zero"
	}
	return kind
}

// Labels stacked on one loop are each a label: the inner one names the loop, the outer one is a goto target.
func stacked(values []int) int {
	total := 0
first: // want "`first` is a label"
second: // want "`second` is a label"
	for _, value := range values {
		if value < 0 {
			break second
		}
		if value == 0 {
			continue second
		}
		total += value
	}
	if total == 0 && len(values) > 0 {
		values = values[1:]
		goto first
	}
	return total
}
