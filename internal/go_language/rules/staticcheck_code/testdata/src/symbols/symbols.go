// Package symbols uses a symbol of each kind pattern's Symbol tells apart, plain, parenthesized and instantiated, and
// names that resemble them: functions and methods, builtins, types and aliases at package scope and in blocks,
// variables and constants at package scope and below it, fields and parameters.
package symbols

import (
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"unsafe"
)

type record struct {
	field int
	sync.WaitGroup
}

func (record) method()         {}
func (*record) pointerMethod() {}

type alias = record

type waitGroup = sync.WaitGroup

type number = int

type twice = alias

type slice = []int

type List[T any] struct{ items []T }

// genericAlias is an alias with type parameters of its own, and integers one of its instances, so a type name's
// brackets are met on an alias as on a named type.
type genericAlias[T any] = List[T]

type integers = genericAlias[int]

type pair[K comparable, V any] struct {
	key   K
	value V
}

var table = []int{1}

var lists = map[string]int{}

const limit = 3

func local() {}

func identity[T any](value T) T { return value }

func both[K comparable, V any](key K, value V) {}

func functions(value record, pointer *record, stringer fmt.Stringer, group *sync.WaitGroup) {
	local()
	(local)()
	(local)()
	strings.Repeat("a", 2)
	(strings.Repeat)("a", 2)
	value.method()
	pointer.pointerMethod()
	pointer.method()
	(*record).pointerMethod(pointer)
	record.method(value)
	_ = stringer.String()
	group.Add(1)
	value.Add(1)
	(&value).Wait()
	fmt.Println(math.Pi, os.Args)
}

func builtins(values []int) {
	_ = len(values)
	_ = (len)(values)
	values = append(values, 1)
	_ = cap(values)
	{
		len := func(_ []int) int { return 0 }
		_ = len(values)
	}
}

func typesAndAliases(parameter int) {
	var named record
	var aliased alias
	var doubled twice
	var basic number
	var grouped waitGroup
	var sliced slice
	var integer int
	var failure error
	var anything any
	var generic List[int]
	var paired pair[int, string]
	type inner struct{}
	var block inner
	_ = record{}
	_ = alias{}
	_ = twice{}
	_ = number(1)
	_ = waitGroup{}
	_ = slice{}
	_ = int(parameter)
	_ = unsafe.Pointer(&parameter)
	_ = string(rune(parameter))
	_ = List[int]{}
	_ = genericAlias[int]{}
	_ = (genericAlias[string])(List[string]{})
	var counted integers
	_ = counted
	_ = (*List[int])(nil)
	_ = pair[int, string]{}
	_ = inner{}
	_, _, _, _, _, _, _, _, _, _, _, _ = named, aliased, doubled, basic, grouped, sliced, integer, failure, anything,
		generic, paired, block
}

func variablesAndConstants(parameter []int) {
	local := []int{2}
	_ = table
	_ = (table)
	_ = table[0]
	_ = lists["a"]
	_ = limit
	_ = local
	_ = local[0]
	_ = parameter[0]
	_ = math.Pi
	_ = os.Args
	_ = os.Args[0]
	var value record
	_ = value.field
}

func instantiations() {
	_ = identity[int](1)
	_ = (identity[int])(1)
	_ = identity(2)
	both[int, string](1, "a")
	(both[int, string])(1, "a")
	f := identity[string]
	_ = f
}
