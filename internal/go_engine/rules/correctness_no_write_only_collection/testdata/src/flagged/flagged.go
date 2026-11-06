package flagged

type row struct {
	id   int
	name string
}

type registry map[string]int

func work(index int) error { return nil }

// The ranks are kept for the result and never copied in: the shape the TypeScript twin was found on.
func rankRows(rows []row) int {
	ranks := map[int]int{} // want "only ever added to"
	for index, each := range rows {
		ranks[each.id] = index
	}
	return len(rows)
}

func byIndex(rows []row) {
	names := make([]string, len(rows)) // want "only ever added to"
	for index, each := range rows {
		names[index] = each.name
	}
}

func deletedAndCleared() {
	pending := map[int]string{1: "a", 2: "b"} // want "only ever added to"
	delete(pending, 1)
	defer clear(pending)
}

func clearedSlice() {
	buffer := make([]byte, 64) // want "only ever added to"
	clear(buffer)
}

// An append alone is SA4010's, and beside an index write SA4010 stands down, so this rule reports it.
func appendedAndIndexed(rows []row) {
	names := make([]string, 0, len(rows)) // want "only ever added to"
	for _, each := range rows {
		names = append(names, each.name)
	}
	names[0] = "first"
}

func declaredWithVar(rows []row) {
	var seen = make(map[int]bool) // want "only ever added to"
	var counts map[string]int     // want "only ever added to"
	for _, each := range rows {
		seen[each.id] = true
		counts[each.name] = each.id
	}
}

func severalTargets() {
	left, right := map[string]int{}, map[string]int{} // want "only ever added to"
	left["a"], left["b"] = 1, 2
	right["c"] = 3
	_ = right["c"]
}

func namedType() {
	entries := registry{} // want "only ever added to"
	entries["a"] = 1
}

func inClosures(count int) {
	results := make([]error, count) // want "only ever added to"
	for index := 0; index < count; index++ {
		func() {
			results[index] = work(index)
		}()
	}
}

func generic[T comparable](values []T) int {
	seen := map[T]bool{} // want "only ever added to"
	for _, value := range values {
		seen[value] = true
	}
	return len(values)
}

func inIfInitializer(rows []row) {
	if cache := (map[int]string{}); len(rows) > 0 { // want "only ever added to"
		cache[rows[0].id] = rows[0].name
	}
}

// Shadowing: the outer map is only written, and the inner one of the same name is read. Only the outer is reported.
func shadowed() int {
	ids := map[string]bool{} // want "only ever added to"
	ids["a"] = true
	{
		ids := map[string]bool{"b": true}
		return len(ids)
	}
}

// A function literal at package level has a body like any other, and its locals are judged the same.
var handler = func(rows []row) {
	byId := map[int]row{} // want "only ever added to"
	for _, each := range rows {
		byId[each.id] = each
	}
}
