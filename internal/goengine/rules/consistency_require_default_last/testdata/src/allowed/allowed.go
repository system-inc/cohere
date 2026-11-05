package allowed

// expressionLast keeps the default at the bottom of its switch.
func expressionLast(count int) string {
	switch count {
	case 0:
		return "none"
	case 1:
		return "one"
	default:
		return "many"
	}
}

// onlyDefault has a default and nothing else, so it is the last clause.
func onlyDefault(count int) int {
	switch {
	default:
		return count
	}
}

// noDefault has no default to place.
func noDefault(count int) string {
	switch count {
	case 0:
		return "none"
	}
	return "some"
}

// emptySwitch has no clauses at all.
func emptySwitch(count int) {
	switch count {
	}
}

// typeLast keeps the default at the bottom of its type switch.
func typeLast(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case int, nil:
		return "int or nil"
	default:
		return "unknown"
	}
}

// typeNoDefault has no default to place.
func typeNoDefault(value any) string {
	switch value.(type) {
	case string:
		return "string"
	}
	return "other"
}

// selectLast keeps the default at the bottom of its select.
func selectLast(ready chan int) int {
	select {
	case value := <-ready:
		return value
	default:
		return 0
	}
}

// selectNoDefault waits, with no default to place.
func selectNoDefault(ready chan int, done chan struct{}) int {
	select {
	case value := <-ready:
		return value
	case <-done:
		return 0
	}
}

// emptySelect has no clauses at all, and blocks forever.
func emptySelect() {
	select {}
}
