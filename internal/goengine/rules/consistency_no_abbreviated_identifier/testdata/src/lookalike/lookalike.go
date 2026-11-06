// Package lookalike holds abbreviated names that are someone else's, which the rule must leave alone.
package lookalike

import (
	buf "bytes"
	"context"
	"foreign"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"time"
)

// A renamed import is the imported package's name, and a selector into another package, or into a field another
// package declares, is a use of that package's spelling.
func run() (int, error) {
	output := buf.NewBuffer(nil)
	command := exec.Command("go", "version")
	_ = command.Args
	_ = os.Args
	_ = output
	return strconv.Atoi("1")
}

// An embedded field is named for its type, Cmd here, which is another package's name.
type runner struct {
	*exec.Cmd
}

// sort.Interface fixes Len, and the type implements all of it.
type byPosition []int

func (list byPosition) Len() int                  { return len(list) }
func (list byPosition) Less(left, right int) bool { return list[left] < list[right] }
func (list byPosition) Swap(left, right int)      { list[left], list[right] = list[right], list[left] }

var _ sort.Interface = byPosition{}

// context.Context fixes Err, and the type implements all of it.
type quietContext struct{}

func (quietContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (quietContext) Done() <-chan struct{}       { return nil }
func (quietContext) Err() error                  { return nil }
func (quietContext) Value(key any) any           { return nil }

var _ context.Context = quietContext{}

// A generic receiver cannot be asked whether it implements an interface, so a method matching a foreign interface
// method's name and signature, sort.Interface's Len, is taken as fixed by it.
type stack[Item any] []Item

func (items stack[Item]) Len() int { return len(items) }

// An interface method of ours that a method another package declares must satisfy: *bytes.Buffer's Len.
type lengther interface {
	Len() int
}

var _ lengther = &buf.Buffer{}

// foreign.Commander fixes Cmd, and the type implements all of it.
type shell struct{}

func (shell) Cmd() string { return "" }
func (shell) Wait() error { return nil }

var _ foreign.Commander = shell{}

// foreign.Source is generic, and the type implements its instance Source[int], read off Src's int result.
type reader struct{}

func (reader) Src() int     { return 0 }
func (reader) Close() error { return nil }

var _ foreign.Source[int] = reader{}

// foreign.Target is used only as foreign.Copy's constraint, and the type satisfies its instance Target[string].
type copier struct{}

func (copier) Dst() string { return "" }

var _ = foreign.Copy[string, copier]

// foreign.Sequence's Seq names none of its type parameters, so no instance can be read off it, and a method with Seq's
// shape is taken as fixed by it.
type counter struct{}

func (counter) Seq() int { return 0 }

// An interface method of ours that a method of an instance of a generic foreign type must satisfy: Pool[int]'s Conn.
type connector interface {
	Conn() int
}

var _ connector = foreign.Pool[int]{}
