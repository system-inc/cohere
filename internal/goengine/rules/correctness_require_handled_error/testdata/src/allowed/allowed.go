// Package allowed holds calls the rule must leave alone: each handles its error, says why it drops it, or is a write
// exempt by its writer's type.
package allowed

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"hash"
	"hash/crc32"
	"hash/maphash"
	"os"
	"strconv"
	"strings"
	"sync"
)

func save() error { return nil }

func handled(file *os.File, text string) (int, error) {
	if closeError := file.Close(); closeError != nil {
		return 0, closeError
	}
	value, parseError := strconv.Atoi(text)
	if parseError != nil {
		return 0, fmt.Errorf("parsing %q: %w", text, parseError)
	}
	// The blank takes the number, not the error, which is handled.
	_, secondError := strconv.Atoi(text)
	if secondError != nil {
		return 0, secondError
	}
	return value, save()
}

func reasoned(file *os.File) {
	// Ignored: the file was only read, so a failed close loses nothing.
	_ = file.Close()

	// reasoned closes the file a second time to show a marker on a later line of the comment above.
	// Ignored: a second close fails by design and changes nothing.
	file.Close()

	defer file.Close() // Ignored: the file was only read, so a failed close loses nothing.

	/* Ignored: a block comment may say it too. */
	save()

	go save() /* Ignored: a reason at the end of the line, in a block comment. */

	// A statement over several lines takes its reason at the end of its last line.
	_ = fmt.Errorf(
		"%s",
		"text",
	) // Ignored: the value is built only to show the shape.

	// Ignored: the number is a best-effort hint, and zero is a fine answer.
	hint, _ := strconv.Atoi("12")
	_ = hint
}

// Ignored: the package-level parse cannot fail on a constant.
var packageParsed, _ = strconv.Atoi("1")

// The writes documented never to fail need no reason.
func neverFailing(data []byte) string {
	var builder strings.Builder
	builder.Write(data)
	builder.WriteByte('a')
	builder.WriteRune('b')
	builder.WriteString("c")
	_, _ = builder.WriteString("d")
	defer builder.WriteString("e")

	var buffer bytes.Buffer
	buffer.Write(data)
	buffer.WriteByte('a')
	buffer.WriteRune('b')
	buffer.WriteString("c")
	_ = buffer.WriteByte('d')

	fmt.Fprint(&builder, "data")
	fmt.Fprintf(&buffer, "%s", "data")
	_, _ = fmt.Fprintln(&builder, "data")

	pointer := &builder
	pointer.WriteString("f")
	fmt.Fprint(pointer, "g")
	return builder.String() + buffer.String()
}

// A bufio.Writer keeps a failed write's error and hands it back from every later write and from Flush, so its
// writes need no reason as long as the Flush is checked.
func buffered(writer *bufio.Writer) error {
	writer.Write([]byte("data"))
	writer.WriteString("data")
	writer.WriteByte('a')
	writer.WriteRune('b')
	fmt.Fprintf(writer, "%s", "data")
	_, _ = fmt.Fprintln(writer, "data")
	return writer.Flush()
}

// An embedded Builder written through its embedder is still the Builder's own write.
type document struct{ strings.Builder }

func embedded() string {
	var page document
	page.WriteString("line")
	return page.String()
}

// A hash.Hash's Write never returns an error, and the interface says so for every implementation.
func hashes(data []byte) {
	hasher := sha256.New()
	hasher.Write(data)
	_, _ = hasher.Write(data)
	fmt.Fprintf(hasher, "%d", 1)
	hash.Hash.Write(hasher, data)

	checksum := crc32.NewIEEE()
	checksum.Write(data)

	// A maphash.Hash value is addressable, so its Write is the pointer's, which implements hash.Hash.
	var seeded maphash.Hash
	seeded.Write(data)
}

// Calls that return no error, builtins and conversions drop nothing the rule is about.
func noError(group *sync.WaitGroup, mapping map[string]int, channel chan int, problem error) {
	defer group.Done()
	go func() {}()
	strings.ToUpper("text")
	delete(mapping, "key")
	copy(make([]int, 1), []int{1})
	defer recover()
	_ = problem
	_, found := mapping["key"]
	_, received := <-channel
	_, isFailure := problem.(interface{ Temporary() bool })
	_, _, _ = found, received, isFailure
}
