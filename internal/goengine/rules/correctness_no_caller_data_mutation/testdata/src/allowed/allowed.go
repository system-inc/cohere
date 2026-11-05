// Package allowed holds code the rule must leave alone: writes into the function's own data, a receiver's writes,
// and an out-parameter its function's doc comment declares with a reason.
package allowed

import (
	"io"
	"slices"
)

type Record struct {
	Name  string
	Count int
}

type zeroReader struct {
	served int
}

// rename writes its receiver, which is the type's API.
func (record *Record) rename(name string) {
	record.Name = name
	record.Count++
}

// Read fills the caller's buffer, which is its contract.
//
// Mutates: buffer io.Reader's contract is to fill the caller's buffer with the next bytes
func (reader *zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	reader.served += len(buffer)
	return len(buffer), nil
}

// The tag exempts its parameter in the function's closures too, since they write the same parameter.
//
// Mutates: counts the census fills the caller's tally, one entry per word
func tally(words []string, counts map[string]int) {
	add := func(word string) {
		counts[word]++
	}
	for _, word := range words {
		add(word)
	}
	delete(counts, "")
}

/*
Mutates: values a block comment tag reads the same as a line comment one
*/
func blockTagged(values []int) {
	values[0] = 1
}

// Mutates: target	the reason may follow the parameter after a tab
func tabbed(target *Record) {
	*target = Record{}
}

func local(values []int) []int {
	copied := slices.Clone(values)
	copied[0] = 1
	counts := map[string]int{}
	counts["local"] = len(values)
	record := &Record{}
	record.Name = "local"
	return copied
}

func reads(values []int, counts map[string]int, record *Record) int {
	total := values[0] + counts["key"] + record.Count
	copied := *record
	copied.Count = total
	return copied.Count
}

// A named result is the function's own, to fill as it likes.
func results() (values []int, record *Record) {
	values = make([]int, 1)
	values[0] = 1
	record = &Record{}
	record.Name = "made here"
	return values, record
}

// onesReader implements io.Reader, whose contract is that Read fills the caller's buffer, so its buffer needs no tag.
type onesReader struct{}

var _ io.Reader = onesReader{}

func (reader onesReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 1
	}
	return len(buffer), nil
}
