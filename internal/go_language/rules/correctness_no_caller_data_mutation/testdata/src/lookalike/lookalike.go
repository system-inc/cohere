// Package lookalike holds shapes that look like a write into a caller's data and are not: a copy the function was
// passed by value, a name that shadows the parameter, and a delete or clear that is not the builtin.
package lookalike

type Record struct {
	Name  string
	Count int
}

type Cache struct {
	entries map[string]int
}

func (cache *Cache) delete(entries map[string]int) {
	cache.entries = entries
}

// An array and a struct passed by value are the function's own copies.
func copies(grid [4]int, record Record) [4]int {
	grid[0] = 1
	record.Name = "copy"
	record.Count++
	return grid
}

// A generic whose constraint allows only arrays writes into its own copy.
func arrayGeneric[A ~[4]int](grid A) A {
	grid[0] = 1
	return grid
}

// A constraint that allows an array beside a slice may hand the function its own copy, so it is not judged.
func mixedGeneric[S interface{ ~[]int | ~[4]int }](values S) S {
	values[0] = 1
	return values
}

// A := in an inner scope, or a function literal's own parameter, declares a new variable that shadows the parameter.
func shadowed(values []int, record *Record) {
	{
		values := make([]int, 1)
		values[0] = 1
		record := &Record{}
		record.Name = "inner"
	}
	set := func(values []int) {
		values[0] = 1
	}
	set(make([]int, 1))
}

// A function of the package's own named clear, and a method named delete, are not the builtins.
func clear(values []int) int {
	return len(values)
}

func notBuiltins(values []int, entries map[string]int, cache *Cache) int {
	cache.delete(entries)
	return clear(values)
}

// A composite literal's key, a comparison and a send name a field or element without writing it.
func notWrites(record *Record, values []int, sink chan<- int) Record {
	sink <- values[0]
	if record.Name == "" && values[0] == 0 {
		return Record{Name: record.Name}
	}
	return *record
}

// A parameter whose type is an interface or a function is not a slice, map or pointer.
func others(reader interface{ Read([]byte) (int, error) }, visit func([]int)) {
	buffer := make([]byte, 8)
	_, _ = reader.Read(buffer)
	visit([]int{1})
}
