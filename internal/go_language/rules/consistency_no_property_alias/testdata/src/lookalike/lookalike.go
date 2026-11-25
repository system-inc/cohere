package lookalike

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sync"
)

type settings struct {
	port int
}

type node struct {
	name string
}

type tree struct {
	tree *tree
}

type server struct {
	name     string
	count    int
	items    []string
	values   [4]int
	settings settings
	buffer   bytes.Buffer
	lock     *sync.Mutex
	nodes    map[string]node
	pointer  *settings
	done     chan struct{}
}

func (server *server) run() {}

func (server *server) describe() string { return server.name }

// Read twice: the local may be a snapshot both reads must agree on.
func readTwice(server *server) string {
	name := server.name
	return name + name
}

// Read inside a function literal, which runs on its own schedule.
func inClosure(server *server) func() string {
	name := server.name
	return func() string { return name }
}

// Read inside a goroutine's literal.
func inGoroutine(server *server, output chan<- string) {
	name := server.name
	go func() { output <- name }()
}

// Read inside a loop's body the declaration is outside of: the reach would run every turn.
func inLoopBody(server *server, rounds int) int {
	total := 0
	count := server.count
	for range rounds {
		total += count
	}
	return total
}

// Read in a for loop's condition, which runs every turn.
func inLoopCondition(server *server) int {
	index := 0
	count := server.count
	for ; index < count; index++ {
	}
	return index
}

// Read inside a range's body the declaration is outside of.
func inRangeBody(server *server, words []string) int {
	total := 0
	count := server.count
	for range words {
		total += count
	}
	return total
}

// A call between the declaration and the read could change the field.
func callBetween(server *server) string {
	name := server.name
	server.run()
	return name
}

// A call in the read's own expression, after the read: Go leaves their order unspecified.
func callAfterInExpression(server *server) string {
	name := server.name
	return fmt.Sprint(name, server.describe())
}

// An assignment between, to the root or to anything else.
func assignmentBetween(server *server, other *server) string {
	name := server.name
	server = other
	return name
}

// The take and clear: the local holds what the field no longer does.
func takeAndClear(server *server) string {
	name := server.name
	server.name = ""
	return name
}

// An increment between.
func incrementBetween(server *server) int {
	count := server.count
	server.count++
	return count
}

// A := that reuses an existing name assigns it.
func reusingDefine(server *server, other *server) (string, int) {
	name := server.name
	server, extra := other, 1
	return name, extra
}

// A send and a receive between hand control to another goroutine.
func channelBetween(server *server, output chan<- int) (int, string) {
	count := server.count
	output <- 1
	name := server.name
	<-server.done
	return count, name
}

// A select between.
func selectBetween(server *server) string {
	name := server.name
	select {
	default:
	}
	return name
}

// A range over a channel receives, and one over a function calls it.
func rangeBetween(server *server, sequence func(func(int) bool)) (int, string) {
	count := server.count
	for range server.done {
	}
	name := server.name
	for range sequence {
	}
	return count, name
}

// A copy into a slice may write the field a pointer in the chain points into.
func copyBetween(server *server, values []settings, replacement []settings) int {
	pointer := &values[0]
	port := pointer.port
	copy(values, replacement)
	return port
}

// A write to the local: the copy changes, the field does not.
func writtenThrough(server *server) settings {
	settings := server.settings
	settings.port = 3
	return server.settings
}

// The local's address taken: a pointer to the copy is not a pointer to the field.
func addressTaken(server *server) *string {
	name := server.name
	return &name
}

// A pointer method on a value local takes the copy's address and may write it.
func pointerMethod(server *server) (int, error) {
	buffer := server.buffer
	return buffer.WriteString("x")
}

// A slice of an array local shares the copy's elements, not the field's.
func arraySliced(server *server) []int {
	values := server.values
	return values[:]
}

// A declaration between that shadows the chain's first name: the reach at the read would name the other one.
func shadowed(server *server, other *server) string {
	name := server.name
	{
		server := other
		return name + server.name
	}
}

// A local named for its own root: the reach at the read would name the local, so this known miss is left alone.
func selfNamed(tree *tree) *tree {
	if tree != nil {
		tree := tree.tree
		return tree
	}
	return nil
}

// A goto can run the read again without its declaration.
func withGoto(server *server, output chan<- string) {
	retry := true
	name := server.name
again:
	output <- name
	if retry {
		retry = false
		goto again
	}
}

// A type on the declaration may convert.
func annotated(server *server) any {
	var name any = server.name
	return name
}

// Two names bound at once.
func twoNames(server *server) (string, int) {
	name, count := server.name, server.count
	return name, count
}

// A chain holding a call computes, so the local caches it.
func throughCall(current func() *server) string {
	name := current().name
	return name
}

// A chain holding an index or a dereference is not names and field reads alone.
func throughIndexAndDereference(server *server) (string, int) {
	name := server.nodes["first"].name
	port := (*server.pointer).port
	return name, port
}

// A qualified identifier names another package's variable, not a field.
func qualified() (int, error) {
	ErrUnexpectedEOF := io.ErrUnexpectedEOF
	MaxInt := math.MaxInt
	return MaxInt, ErrUnexpectedEOF
}

// A method value is no field read.
func methodValue(server *server) func() {
	run := server.run
	return run
}

// A lock taken out of a struct is read again to unlock it.
func lockedTwice(server *server) {
	lock := server.lock
	lock.Lock()
	defer lock.Unlock()
}
