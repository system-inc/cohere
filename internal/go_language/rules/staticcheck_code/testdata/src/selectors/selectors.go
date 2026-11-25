// Package selectors holds selectors of every kind: fields direct, promoted and through pointers, methods on values,
// pointers, interfaces and embedded types, method values and expressions, members of imported packages, and members
// of generic types, so the helpers naming a selection or finding a reference meet each.
package selectors

import (
	"bytes"
	"fmt"
	stdhttp "net/http"
	"strings"
	"sync"
)

type point struct{ x, y int }

func (p point) sum() int { return p.x + p.y }

func (p *point) move(by int) { p.x += by }

type shape struct {
	point
	*sync.Mutex
	name   string
	corner *point
}

type pointAlias = point

type pointPointer *point

type namer interface{ Name() string }

type fullNamer interface {
	namer
	fmt.Stringer
}

type pair[First, Second any] struct {
	first  First
	second Second
}

func (p pair[First, Second]) swapped() pair[Second, First] {
	return pair[Second, First]{p.second, p.first}
}

func selectors(s shape, sp *shape, request *stdhttp.Request, buffer bytes.Buffer, named fullNamer) int {
	var p point
	var alias pointAlias
	var pp pointPointer = &p
	pointer := &p
	doublePointer := &pointer
	total := p.x + p.y + pointer.x + (*doublePointer).y + alias.x + pp.y
	total += s.x + s.point.y + sp.x + sp.corner.x + s.corner.y + len(s.name) + len(sp.name)
	total += p.sum() + pointer.sum() + s.sum() + sp.sum()
	p.move(1)
	pointer.move(2)
	s.move(3)
	sp.move(4)
	s.Lock()
	sp.Unlock()
	sum := p.sum
	move := pointer.move
	move(sum())
	total += point.sum(p) + pointAlias.sum(alias)
	(*point).move(pointer, 5)
	_ = request.Header
	_ = request.URL.Path
	_ = stdhttp.MethodGet
	_ = stdhttp.StatusOK
	_ = stdhttp.Get
	_ = buffer.Len()
	_ = (&buffer).String()
	_ = named.Name()
	_ = named.String()
	_ = strings.ToUpper(fmt.Sprint(total))
	paired := pair[int, string]{1, "one"}
	_ = paired.first
	_ = paired.swapped().second
	_ = pair[string, int].swapped
	anonymous := struct{ inner struct{ deep int } }{}
	total += anonymous.inner.deep
	return total
}
