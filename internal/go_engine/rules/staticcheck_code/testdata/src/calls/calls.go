// Package calls holds a wide spread of calls, each naming its callee a different way, for the helpers that name a
// call's callee: functions, methods by value, pointer, alias, embedding and interface, builtins, conversions,
// function values and closures, and calls of a builtin of package unsafe.
package calls

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

type counter struct{ count int }

func (c counter) value() int { return c.count }

func (c *counter) increment() { c.count++ }

type embedding struct {
	counter
	*sync.Mutex
	callback func() int
}

type valuer interface{ value() int }

type wider interface {
	valuer
	fmt.Stringer
}

type counterAlias = counter

type counterPointerAlias = *counter

type named func() int

func plain() int { return 1 }

func returnsFunction() func() int { return plain }

var initialized = plain()

func calls(w wider, buffer *bytes.Buffer, builder strings.Builder, values []int, text string) {
	var c counter
	_ = plain()
	_ = (plain)()
	_ = (plain)()
	_ = c.value()
	c.increment()
	(&c).increment()
	_ = (c.value)()
	_ = counter.value(c)
	(*counter).increment(&c)
	var alias counterAlias
	_ = alias.value()
	var pointerAlias counterPointerAlias = &c
	pointerAlias.increment()
	e := embedding{Mutex: &sync.Mutex{}, callback: plain}
	_ = e.value()
	e.increment()
	e.Lock()
	e.Unlock()
	_ = e.callback()
	_ = w.value()
	_ = w.String()
	var v valuer = c
	_ = v.value()
	buffer.WriteString(text)
	_, _ = builder.WriteString(text)
	fmt.Println(text)
	(fmt.Println)(text)
	_ = fmt.Sprint(len(values), cap(values))
	values = append(values, 1)
	_ = make([]int, 1)
	_ = new(int)
	copy(values, values)
	delete(map[int]int{}, 1)
	print(text)
	println()
	_ = []byte(text)
	_ = string(rune(1))
	_ = int(1.0)
	_ = (*counter)(nil)
	_ = named(plain)()
	f := plain
	_ = f()
	_ = returnsFunction()()
	_ = func() int { return 2 }()
	functions := []func() int{plain}
	_ = functions[0]()
	_ = map[string]func() int{"plain": plain}["plain"]()
	_ = unsafe.Sizeof(c)
	_ = strings.ToUpper(strings.TrimSpace(text))
	len := func(int) int { return 0 }
	_ = len(1)
	defer recover()
	go plain()
	_ = initialized
}

func panics() {
	panic(fmt.Sprintf("%d", 1))
}
