// Package flagged holds every write the rule must report: a direct write at a slice, map or pointer parameter, which
// lands in the caller's data, and a Mutates tag that names no parameter or gives no reason.
package flagged

import "io"

type Record struct {
	Name  string
	Count int
	Embedded
}

type Embedded struct {
	Note string
}

type Buffer []byte

type RecordPointer = *Record

func fillSlice(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

func fillMap(counts map[string]int) {
	counts["assigned"] = 1  // want "writes into data the caller passed in"
	counts["incremented"]++ // want "writes into data the caller passed in"
	counts["added"] += 2    // want "writes into data the caller passed in"
}

func remove(counts map[string]int) {
	delete(counts, "gone") // want "writes into data the caller passed in"
}

func empty(values []int, counts map[string]int) {
	clear(values) // want "writes into data the caller passed in"
	clear(counts) // want "writes into data the caller passed in"
}

func rename(record *Record) {
	record.Name = "renamed"  // want "writes into data the caller passed in"
	record.Count--           // want "writes into data the caller passed in"
	record.Note = "promoted" // want "writes into data the caller passed in"
}

func replace(record *Record) {
	*record = Record{} // want "writes into data the caller passed in"
}

func spelledOut(record *Record, values []int, grid *[4]int) {
	(*record).Name = "renamed" // want "writes into data the caller passed in"
	(values)[0] = 1            // want "writes into data the caller passed in"
	(record).Count = 2         // want "writes into data the caller passed in"
	(*grid)[1] = 1             // want "writes into data the caller passed in"
}

func behindPointer(grid *[4]int) {
	grid[0] = 1 // want "writes into data the caller passed in"
}

func swap(values []int) {
	values[0], values[1] = values[1], values[0] // want "writes into" "writes into"
}

func ranged(values []int, source map[int]int) {
	for values[0], values[1] = range source { // want "writes into" "writes into"
	}
}

func everyKey(counts map[string]int) {
	for key := range counts {
		counts[key] = 0 // want "writes into data the caller passed in"
	}
}

func inClosure(values []int) {
	set := func() {
		values[0] = 1 // want "writes into data the caller passed in"
	}
	set()
}

func named(buffer Buffer) {
	buffer[0] = 0 // want "writes into data the caller passed in"
}

func aliased(record RecordPointer) {
	record.Name = "renamed" // want "writes into data the caller passed in"
}

func fillGeneric[S ~[]E, E any](values S, value E) {
	values[0] = value // want "writes into data the caller passed in"
}

func removeGeneric[M ~map[K]V, K comparable, V any](entries M, key K) {
	delete(entries, key) // want "writes into data the caller passed in"
}

type Slice interface {
	~[]int
}

// A named constraint is read through to its terms, and so is one constraint embedded in another.
func namedConstraint[S Slice, N interface{ Slice }](values S, others N) {
	values[0] = 1 // want "writes into data the caller passed in"
	others[0] = 1 // want "writes into data the caller passed in"
}

// The type set is the intersection of the elements, so one element of slices alone is enough.
func intersected[S interface {
	~[]int | ~[4]int
	~[]int
}](values S) {
	values[0] = 1 // want "writes into data the caller passed in"
}

func storeGeneric[T any](target *T, value T) {
	*target = value // want "writes into data the caller passed in"
}

func unicodeName(größen []int) {
	größen[0] = 1 // want "writes into data the caller passed in"
}

// copyInto writes its receiver, which is the type's API, and its parameter, which is the caller's.
func (record *Record) copyInto(target *Record) {
	record.Count = 1
	target.Count = record.Count // want "writes into data the caller passed in"
}

// A tag with no reason exempts nothing, and is reported where it is written.
//
// want +1 "gives no reason"
// Mutates: values
func bareTag(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

// want +1 "gives no reason"
/* Mutates: values */
func bareBlockTag(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

// A tag naming no parameter of its function, as one left behind by a rename does, exempts nothing.
//
// want +1 "names no parameter"
// Mutates: entries the loader fills the caller's slice in place
func renamedTag(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

// A tag on the receiver names no parameter: the receiver's writes are never judged.
//
// want +1 "names no parameter"
// Mutates: record the record is the type's own
func (record *Record) receiverTag(other *Record) {
	other.Count = record.Count // want "writes into data the caller passed in"
}

// want +1 "names no parameter"
// Mutates:
func emptyTag(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

// A tag for one parameter exempts that one, and the other is still the caller's.
//
// Mutates: target the caller hands in the record to fill
func partialTag(target *Record, source *Record) {
	target.Name = source.Name
	source.Name = "" // want "writes into data the caller passed in"
}

// A sentence that mentions the marker is no tag: it says Mutates: values the loader fills it.
func mentioned(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

var unrelated = 0 // Mutates: values a trailing comment on the line above belongs to that line

func trailing(values []int) {
	values[0] = unrelated // want "writes into data the caller passed in"
}

// Mutates: values a doc comment a blank line away is no doc comment

func detached(values []int) {
	values[0] = 1 // want "writes into data the caller passed in"
}

// A method named Read whose results are not io.Reader's implements nothing, and a function named Read has no receiver
// to implement it with, so neither has a buffer by contract.
type countingReader struct {
	source io.Reader
}

func (reader *countingReader) Read(buffer []byte) int {
	buffer[0] = 0 // want "writes into data the caller passed in"
	return 1
}

func Read(buffer []byte) (int, error) {
	buffer[0] = 0 // want "writes into data the caller passed in"
	return 1, nil
}
