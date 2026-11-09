// Package names declares and calls functions of every shape a full name spells: package functions, methods with
// value and pointer receivers, generic functions and methods of generic types, interface methods, embedded and
// promoted methods, methods of an aliased type, and calls into other packages.
package names

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"text/template"
	"time"
)

type record struct{ sync.Mutex }

func (record) value()    {}
func (*record) pointer() {}

type alias = record

type List[T any] struct{ items []T }

func (list *List[T]) Push(item T) { list.items = append(list.items, item) }
func (list List[T]) Length() int   { return len(list.items) }

type speaker interface{ speak() string }

func identity[T any](value T) T { return value }

func local() {}

func uses(value record, pointer *record, aliased alias, list *List[int], speaking speaker, writer io.Writer,
	buffer *bytes.Buffer, header http.Header) {
	local()
	value.value()
	pointer.pointer()
	pointer.Lock()
	aliased.value()
	list.Push(identity(1))
	_ = list.Length()
	_ = speaking.speak()
	_, _ = writer.Write(nil)
	_, _ = buffer.WriteString("a")
	header.Set("a", "b")
	_ = http.CanonicalHeaderKey("a")
	_ = strings.Contains("a", "b")
	fmt.Println(fmt.Sprintf("%d", 1))
	time.Sleep(time.Until(time.Now()) + time.Since(time.Now()))
	_ = exec.Command("a")
	_ = template.Must(template.New("a").Parse("b"))
	var group sync.WaitGroup
	group.Add(1)
}
