package allowed

import (
	"maps"
	"sort"
)

type row struct {
	id   int
	name string
}

type registry map[string]int

func (entries registry) total() int { return len(entries) }

type holder struct{ cache map[string]int }

var everyName = map[string]bool{}

func use(value any) {}

// The ranks fixed: the map is read for the result.
func rankRows(rows []row) map[int]int {
	ranks := map[int]int{}
	for index, each := range rows {
		ranks[each.id] = index
	}
	return ranks
}

func readByKey(rows []row) int {
	names := map[int]string{}
	for _, each := range rows {
		names[each.id] = each.name
	}
	return len(names[1])
}

func readCommaOk(key string) bool {
	seen := map[string]bool{}
	seen["a"] = true
	_, found := seen[key]
	return found
}

func ranged() {
	names := make([]string, 2)
	names[0] = "a"
	for _, name := range names {
		use(name)
	}
}

func counted() int {
	names := make([]string, 2)
	names[1] = "b"
	return len(names)
}

func passed() {
	names := map[string]int{}
	names["a"] = 1
	use(names)
}

func copiedOut() map[string]int {
	source := map[string]int{}
	source["a"] = 1
	target := map[string]int{}
	maps.Copy(target, source)
	return target
}

func sliced() []int {
	values := make([]int, 4)
	values[0] = 1
	return values[:2]
}

func addressTaken() *int {
	values := make([]int, 4)
	values[0] = 1
	return &values[0]
}

func addressOfWhole() {
	values := make([]int, 4)
	values[0] = 1
	use(&values)
}

func sorted() {
	values := make([]int, 4)
	values[0] = 3
	sort.Ints(values)
}

// A compound write reads the element first, as the TypeScript and Swift twins read it.
func compound(rows []row) {
	counts := map[string]int{}
	for _, each := range rows {
		counts[each.name] += 1
		counts[each.name]++
	}
}

// A slice only appended to is SA4010's, which holds the append half.
func appendedOnly(rows []row) {
	var names []string
	for _, each := range rows {
		names = append(names, each.name)
	}
}

func appendedAndRead(rows []row) []string {
	names := make([]string, 0, len(rows))
	for _, each := range rows {
		names = append(names, each.name)
	}
	names[0] = "first"
	return names
}

func readOnTheRight() {
	values := map[string]int{"a": 1}
	values["b"] = values["a"]
}

func readInTheIndex() {
	values := make([]int, 4)
	values[values[0]] = 1
}

func readInAppend() {
	values := make([]int, 1)
	values[0] = 1
	values = append(values, values[0])
}

func appendedToItself() {
	values := make([]int, 1)
	values[0] = 1
	values = append(values, values...)
}

func methodCalled() int {
	entries := registry{}
	entries["a"] = 1
	return entries.total()
}

func readInClosure() func() int {
	values := map[string]int{}
	values["a"] = 1
	return func() int { return values["a"] }
}

func storedInStruct() holder {
	cache := map[string]int{}
	cache["a"] = 1
	return holder{cache: cache}
}

func storedInGlobal() {
	names := map[string]bool{}
	names["a"] = true
	everyName = names
}

// A variable given a value that may be shared is declined: the second map may alias the first.
func reassigned(other map[string]int) {
	values := map[string]int{}
	values = other
	values["a"] = 1
}

func appendedFromAnother(other []int) {
	values := make([]int, 1)
	values = append(other, 1)
	values[0] = 2
}

func appendedFromItsSlice() {
	values := make([]int, 4)
	values = append(values[:0], 1)
	values[0] = 2
}

// The caller's data, a named result returned bare, and a package-level map are not locals of this function.
func fillParameter(values map[string]int) {
	values["a"] = 1
}

func namedResult() (values map[string]int) {
	values = map[string]int{}
	values["a"] = 1
	return
}

func packageLevel() {
	everyName["a"] = true
}

// An array's unread element writes are x/tools' unusedwrite, so an array is not judged here.
func array() {
	var counts [3]int
	counts[0] = 1
}

// A type parameter's underlying type is its constraint, so a collection of one is declined.
func typeParameter[S ~[]int](length int) {
	values := make(S, length)
	values[0] = 1
}

// A target in parentheses is declined rather than looked through.
func parenthesized() {
	values := map[string]int{}
	(values)["a"] = 1
}

// A := of one call's several results has no initializer per name to read.
func fromCall() {
	values, count := pair()
	values["a"] = count
}

func pair() (map[string]int, int) { return map[string]int{}, 1 }
