// Package parity holds values of each kind of type sort's functions could be given, a slice, an array, a map, a
// pointer, a string, a struct, a function, a channel, a type parameter and an interface holding a value or a nil,
// each named and not, and the shapes staticcheck's representation builds the call in more than one way.
package parity

import "sort"

type numbers []int

type definedNumbers numbers

type aliasedNumbers = []int

type pair [2]int

type table map[string]int

type record struct{ value int }

type sorter interface{ Len() int }

var global = []string{"b", "a"}

var sortedGlobal = sort.SliceIsSorted(global, func(i int, j int) bool { return global[i] < global[j] })

func less(int, int) bool { return false }

func slices(list numbers, defined definedNumbers, aliased aliasedNumbers, bytes []byte, pointers []*int) {
	sort.Slice(list, less)
	sort.Slice(defined, less)
	sort.Slice(aliased, less)
	sort.SliceStable(bytes, less)
	sort.SliceIsSorted(pointers, less)
	sort.Slice(list[1:], less)
	sort.Slice([]string{"a"}, less)
	sort.Slice(global, less)
	sort.Slice(make([]int, 3), less)
	var nilSlice []int
	sort.Slice(nilSlice, less)
	sort.Slice([]int(nil), less)
}

func notSlices(array [3]int, named pair, mapped table, keyed map[int]int, pointer *[]int, text string, value record) {
	sort.Slice(array, less)
	sort.Slice(named, less)
	sort.Slice(mapped, less)
	sort.SliceStable(keyed, less)
	sort.SliceIsSorted(pointer, less)
	sort.Slice(text, less)
	sort.Slice(value, less)
	sort.Slice(struct{}{}, less)
	sort.Slice(less, less)
	sort.Slice(make(chan int), less)
	sort.Slice(&array, less)
	sort.Slice(3.5, less)
	sort.Slice(true, less)
	sort.Slice('a', less)
	sort.Slice("text", less)
	sort.Slice(array[:], less)
}

func interfaces(empty any, named sorter, condition bool, list []int) {
	sort.Slice(empty, less)
	sort.Slice(named, less)
	sort.Slice(nil, less)
	sort.SliceStable(nil, less)
	sort.SliceIsSorted(nil, less)
	sort.Slice(any(nil), less)
	sort.Slice(sorter(nil), less)
	var held any = 4
	sort.Slice(held, less)
	var heldList any = list
	sort.Slice(heldList, less)
	var phi any
	if condition {
		phi = list
	} else {
		phi = nil
	}
	sort.Slice(phi, less)
	var nilOrNumber any
	if condition {
		nilOrNumber = 1
	}
	sort.Slice(nilOrNumber, less)
	var interfaceNil any
	sort.Slice(interfaceNil, less)
	sort.Slice(error(nil), less)
	sort.Slice(any(3), less)
	sort.Slice(any(list), less)
}

func callShapes(array [2]int, list []int) {
	defer sort.Slice(array, less)
	go sort.SliceStable(array, less)
	func() {
		sort.SliceIsSorted(array, less)
	}()
	sorting := sort.Slice
	sorting(array, less)
	sort.Slice(
		array,
		less,
	)
	if sort.SliceIsSorted(array, less) {
		return
	}
	sort.Slice(list, func(i int, j int) bool {
		sort.Slice(array, less)
		return list[i] < list[j]
	})
	_ = sortedGlobal
}

func generic[T any, S ~[]int, A ~[2]int](value T, list S, array A) {
	sort.Slice(value, less)
	sort.Slice(list, less)
	sort.Slice(array, less)
	sort.Slice(&value, less)
}

func instantiated() {
	generic(1, []int{}, [2]int{})
	generic([]int{}, []int{}, [2]int{})
}
