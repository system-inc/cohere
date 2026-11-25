// Package parity holds every branch of assign's judgment and of the fix it offers, with clean cases beside each, and
// the shapes most likely to differ between the port and x/tools' assign. The differential test runs both over each
// and compares their findings and fixes.
package parity

import (
	"math/rand"
	"os"
	"strings"
)

type pair struct{ left, right int }

type Embedded struct{ inner int }

type outer struct {
	Embedded
	pair  pair
	items []int
	table map[string]int
	call  func() int
}

type generic[T any] struct {
	value  T
	values []T
}

type AliasMap = map[string]int

type NamedSlice []int

type AliasSlice = []int

// Only a plain `=` is judged: a definition in a new scope and an operator assignment are not.
func tokens(value int) int {
	{
		value := value
		_ = value
	}
	value += value
	value -= value
	value *= value
	value = value // self-assignment
	return value
}

// A call or receive that yields several values has no pairs to judge.
func cardinality(values map[string]int, channel chan int) (int, bool) {
	first, second := 1, 2
	first, second = twoValues()
	first, _ = values["key"]
	first, _ = <-channel
	_ = second
	present := false
	first, present = values["key"]
	return first, present
}

func twoValues() (int, int) { return 1, 2 }

// Each kind of side that has no effect, assigned to itself, and the same kinds with an effect, which are not.
func noEffects(value *outer, pointer *int, items []int, boxed interface{}, channel chan int, index int) {
	value.pair.left = value.pair.left                                       // self-assignment
	value.items[index+1] = value.items[index+1]                             // self-assignment
	value.items[len(items)] = value.items[len(items)]                       // self-assignment
	value.items[cap(items)-1] = value.items[cap(items)-1]                   // self-assignment
	items[max(index, 1)] = items[max(index, 1)]                             // self-assignment
	items[min(index, 1)] = items[min(index, 1)]                             // self-assignment
	items[int(real(complex(1, 2)))] = items[int(real(complex(1, 2)))]       // self-assignment
	items[int(imag(complex(1, 2)))] = items[int(imag(complex(1, 2)))]       // self-assignment
	items[len(make([]int, 2))] = items[len(make([]int, 2))]                 // self-assignment
	*new(int) = *new(int)                                                   // self-assignment
	*pointer = *pointer                                                     // self-assignment
	(*pointer) = (*pointer)                                                 // self-assignment
	(value.pair.right) = (value.pair.right)                                 // self-assignment
	boxed.(*outer).pair.left = boxed.(*outer).pair.left                     // self-assignment
	items[int(int64(index))] = items[int(int64(index))]                     // self-assignment
	items[len([]int{1, 2})] = items[len([]int{1, 2})]                       // self-assignment
	items[len(map[string]int{"a": 1})] = items[len(map[string]int{"a": 1})] // self-assignment
	items[len(items[1:2])] = items[len(items[1:2])]                         // self-assignment
	items[index&1] = items[index&1]                                         // self-assignment
	items[-index] = items[-index]                                           // self-assignment
	items[len(struct{ a []int }{}.a)] = items[len(struct{ a []int }{}.a)]   // self-assignment
	items[len([...]int{1})] = items[len([...]int{1})]                       // self-assignment
	items[len([]func(){func() {}})] = items[len([]func(){func() {}})]       // self-assignment

	items[index] = items[index] // self-assignment
	items[index] = items[index+0]
	items[index] = items[index] // self-assignment, printed the same
	items[0x1] = items[1]
	value.pair.left = value.pair.right
	(*pointer) = *pointer
	*pointer = (*pointer)

	items[rand.Intn(2)] = items[rand.Intn(2)]
	items[<-channel] = items[<-channel]
	items[copy(items, items)] = items[copy(items, items)]
	items[len(append(items, 1))] = items[len(append(items, 1))]
	items[value.call()] = items[value.call()]
	items[len(func() []int { return items }())] = items[len(func() []int { return items }())]
	items[strings.Index("ab", "b")] = items[strings.Index("ab", "b")]
}

// An element of a map is never judged, since indexing may add its key, however the map is reached or named, but an
// element of a map whose type is a type parameter is, since the parameter's underlying type is its constraint.
type NamedMap map[string]int

func maps[Map ~map[string]int](plain map[string]int, named NamedMap, aliased AliasMap, value *outer, parameter Map) {
	plain["key"] = plain["key"]
	(plain["key"]) = (plain["key"])
	(plain["key"]) = (plain["key"])
	named["key"] = named["key"]
	aliased["key"] = aliased["key"]
	value.table["key"] = value.table["key"]
	parameter["key"] = parameter["key"] // self-assignment, judged as the original judges it
	plain = plain                       // self-assignment: the map itself, not an element
	nested := map[string][]int{}
	nested["key"][0] = nested["key"][0] // self-assignment: a slice element, though reached through a map
}

// Generic values, fields, elements and instantiations.
func generics[T any, S ~[]int](value T, holder *generic[T], slice S, items NamedSlice, aliased AliasSlice) {
	value = value                       // self-assignment
	holder.value = holder.value         // self-assignment
	holder.values[0] = holder.values[0] // self-assignment
	slice[0] = slice[0]                 // self-assignment
	items[0] = items[0]                 // self-assignment
	aliased[0] = aliased[0]             // self-assignment
	instance := generic[int]{}
	instance.value = instance.value // self-assignment
	function := identity[int]
	function = function // self-assignment
	function = identity[int]
	_ = function
	_ = value
}

func identity[T any](value T) T { return value }

// Embedded fields, promoted or spelled out, method values and function fields.
func fields(value *outer, other *outer) {
	value.inner = value.inner                   // self-assignment
	value.Embedded.inner = value.Embedded.inner // self-assignment
	value.Embedded.inner = value.inner
	value.inner = other.inner
	value.Embedded = value.Embedded // self-assignment
	value.call = value.call         // self-assignment
	method := value.Method
	method = method // self-assignment
	_ = method
	*value = *value // self-assignment
	value = value   // self-assignment
	_ = value
}

func (value *outer) Method() int { return value.inner }

// Package variables, qualified and not.
var packageVariable int

func qualified() {
	os.Args = os.Args                 // self-assignment
	os.Args[0] = os.Args[0]           // self-assignment
	packageVariable = packageVariable // self-assignment
	os.Args = os.Environ()
}

// Closures, assigning to a captured variable and to their own.
func closures() func() {
	captured := 1
	return func() {
		captured = captured // self-assignment
		local := 2
		local = local // self-assignment
		func() {
			local = local // self-assignment
		}()
		_ = captured
	}
}

// Runs of self-assignments among other pairs, each run its own pair of edits, keeping the commas between pairs that
// stay.
func runs(first int, second int, third int, fourth int, fifth int) {
	first, second, third = first, 1, third                          // self-assignment of first, third
	first, second, third = 1, second, 3                             // self-assignment of second
	first, second, third, fourth = first, second, 1, fourth         // self-assignment of first, second, fourth
	first, second, third, fourth = 1, second, third, 4              // self-assignment of second, third
	first, second, third, fourth, fifth = first, 2, third, 4, fifth // self-assignment of first, third, fifth
	first, second = second, first
	first, second = first, second                    // self-assignment of both, the whole statement
	first, second, third = first, second, third+0    // self-assignment of first, second
	first, second, third = rand.Int(), second, third // self-assignment of second, third, beside an effect
}
