// Package parity holds every shape the unusedresult pass judges, for the differential between x/tools' pass and the
// port. A trailing `reported` marks a call the default flags report, and `clean` one they leave alone; the
// configured.go file holds what only other flags report.
package parity

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"iter"
	"maps"
	"slices"
	"sort"
	"time"
)

type contextKey struct{}

func equal(first int, second int) bool { return first == second }

func positive(number int) bool { return number > 0 }

// Every function of the default funcs list, each dropped by a call statement.
func defaultFunctions(ctx context.Context, numbers []int, table map[string]int, sequence iter.Seq[int],
	pairs iter.Seq2[string, int]) {
	context.WithCancel(ctx)                                // reported
	context.WithDeadline(ctx, time.Time{})                 // reported
	context.WithTimeout(ctx, time.Second)                  // reported
	context.WithValue(ctx, contextKey{}, 1)                // reported
	errors.New("")                                         // reported
	fmt.Append(nil, 1)                                     // reported
	fmt.Appendf(nil, "%d", 1)                              // reported
	fmt.Appendln(nil, 1)                                   // reported
	fmt.Errorf("")                                         // reported
	fmt.Sprint(1)                                          // reported
	fmt.Sprintf("%d", 1)                                   // reported
	fmt.Sprintln(1)                                        // reported
	maps.All(table)                                        // reported
	maps.Clone(table)                                      // reported
	maps.Collect(pairs)                                    // reported
	maps.Equal(table, table)                               // reported
	maps.EqualFunc(table, table, equal)                    // reported
	maps.Keys(table)                                       // reported
	maps.Values(table)                                     // reported
	slices.All(numbers)                                    // reported
	slices.AppendSeq(numbers, sequence)                    // reported
	slices.Backward(numbers)                               // reported
	slices.BinarySearch(numbers, 1)                        // reported
	slices.BinarySearchFunc(numbers, 1, cmp.Compare[int])  // reported
	slices.Chunk(numbers, 2)                               // reported
	slices.Clip(numbers)                                   // reported
	slices.Clone(numbers)                                  // reported
	slices.Collect(sequence)                               // reported
	slices.Compact(numbers)                                // reported
	slices.CompactFunc(numbers, equal)                     // reported
	slices.Compare(numbers, numbers)                       // reported
	slices.CompareFunc(numbers, numbers, cmp.Compare[int]) // reported
	slices.Concat(numbers, numbers)                        // reported
	slices.Contains(numbers, 1)                            // reported
	slices.ContainsFunc(numbers, positive)                 // reported
	slices.Delete(numbers, 0, 0)                           // reported
	slices.DeleteFunc(numbers, positive)                   // reported
	slices.Equal(numbers, numbers)                         // reported
	slices.EqualFunc(numbers, numbers, equal)              // reported
	slices.Grow(numbers, 1)                                // reported
	slices.Index(numbers, 1)                               // reported
	slices.IndexFunc(numbers, positive)                    // reported
	slices.Insert(numbers, 0, 1)                           // reported
	slices.IsSorted(numbers)                               // reported
	slices.IsSortedFunc(numbers, cmp.Compare[int])         // reported
	slices.Max(numbers)                                    // reported
	slices.MaxFunc(numbers, cmp.Compare[int])              // reported
	slices.Min(numbers)                                    // reported
	slices.MinFunc(numbers, cmp.Compare[int])              // reported
	slices.Repeat(numbers, 2)                              // reported
	slices.Replace(numbers, 0, 0, 1)                       // reported
	slices.Sorted(sequence)                                // reported
	slices.SortedFunc(sequence, cmp.Compare[int])          // reported
	slices.SortedStableFunc(sequence, cmp.Compare[int])    // reported
	slices.Values(numbers)                                 // reported
	sort.Reverse(sort.IntSlice(numbers))                   // reported
	slices.Sort(numbers)                                   // clean: not listed, and it has no result
	slices.Reverse(numbers)                                // clean: not listed
	sort.Ints(numbers)                                     // clean
	maps.Copy(table, table)                                // clean
	fmt.Println(1)                                         // clean: its results may be dropped
	fmt.Sscan("1", new(int))                               // clean
	errors.Is(nil, nil)                                    // clean: not listed, though it is pure
	context.Background()                                   // clean: not listed
	time.Now()                                             // clean
	slices.IndexFunc[[]int](numbers, positive)             // reported: one type argument
	slices.Contains[[]int, int](numbers, 1)                // reported: both type arguments
	(slices.Max[[]int])(numbers)                           // reported: an instantiation in parentheses
	maps.Keys[map[string]int](table)                       // reported
	context.WithCancelCause(ctx)                           // clean: not listed
	errors.Join(errors.New(""))                            // clean: Join is not listed, and New's result is used
	fmt.Sprint(fmt.Sprint(1))                              // reported once: only the outer result is dropped
	_ = fmt.Sprint(1)                                      // clean
	_, _ = context.WithCancel(ctx)                         // clean
	defer fmt.Sprint(1)                                    // clean: a defer statement is no expression statement
	go fmt.Sprint(1)                                       // clean
	func() { fmt.Sprint(1) }()                             // reported, inside the literal; its own call is clean
	func() string { return fmt.Sprint(1) }()               // clean: a literal's call has no declared callee
	sprint := fmt.Sprint                                   // clean
	sprint(1)                                              // clean: a function value has no declared callee
	format(1)                                              // clean
	println(fmt.Sprint(1))                                 // clean
}

// format is a package-level function value, judged as a value, not as fmt.Sprint.
var format = fmt.Sprint

// Calls through parentheses: the statement's, the callee's, and both. gofmt takes a doubled pair of parentheses
// down to one, so each shape is written with one pair.
func parenthesized(numbers []int) {
	(fmt.Sprint(1))         // reported
	(fmt.Sprint)(1)         // reported
	(errors.New)("")        // reported
	(buffer().String)()     // reported: a method value in parentheses
	((errors.New)(""))      // reported
	(slices.Clone(numbers)) // reported
	(fmt.Println)(1)        // clean
}
