// Package parity holds every branch of scannererr's judgment, with clean cases beside each, and the shapes most
// likely to set the port apart from the original. Its findings carry no wants: the test compares the two analyzers'
// findings with each other.
package parity

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
	"sync"

	scanning "bufio"
)

func use(any) {}

// Found: Scan in a for statement's condition.
func scanInCondition(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		use(scanner.Text())
	}
}

// Found: Scan inside a range statement.
func scanInRange(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for range 3 {
		if !scanner.Scan() {
			return
		}
	}
}

// Found: Scan inside a range over a channel, nested in a switch.
func scanInRangeOverChannel(reader io.Reader, lines chan int) {
	scanner := bufio.NewScanner(reader)
	for line := range lines {
		switch line {
		case 0:
			scanner.Scan()
		}
	}
}

// Found: the message names the last Scan in a loop, in source order.
func twoScanLoops(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
	for scanner.Scan() {
	}
}

// Found: a Scan outside any loop is not counted, the one in a loop after it is.
func scanOutsideThenInside(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	for scanner.Scan() {
	}
	scanner.Scan()
}

// Clean: a Scan outside any loop.
func scanOnce(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Scan()
	use(scanner.Text())
}

// Clean: no Scan at all.
func neverScanned(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(nil, 10)
}

// Found: other methods called, Buffer, Split, Bytes, Text, are not escapes.
func otherMethods(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 10), 100)
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		use(scanner.Bytes())
		use(scanner.Text())
	}
}

// Clean: Err is called, before the loop.
func errBefore(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	if scanner.Err() != nil {
		return
	}
	for scanner.Scan() {
	}
}

// Clean: Err is called in a deferred call.
func errDeferred(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	defer scanner.Err()
	for scanner.Scan() {
	}
}

// Clean: Err is called inside a closure.
func errInClosure(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	check := func() error { return scanner.Err() }
	for scanner.Scan() {
	}
	use(check)
}

// Clean: Err as a method value is a use other than a call, an escape.
func errMethodValue(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	check := scanner.Err
	for scanner.Scan() {
	}
	use(check)
}

// Clean: Scan as a method value is an escape too.
func scanMethodValue(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scan := scanner.Scan
	for scan() {
	}
}

// Clean: the scanner passed to a function.
func passedOn(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
	use(scanner)
}

// Clean: the scanner returned.
func returned(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
	return scanner
}

// Clean: the scanner's address taken.
func addressTaken(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
	use(&scanner)
}

// Clean: a parenthesized receiver sits in parentheses, not a selector, so it is an escape.
func parenthesizedReceiver(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for (scanner).Scan() {
	}
}

// Clean: a method expression takes the scanner as an argument.
func methodExpression(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for (*bufio.Scanner).Scan(scanner) {
	}
}

// Clean: the scanner reassigned, whose left side is a use.
func reassigned(first io.Reader, second io.Reader) {
	scanner := bufio.NewScanner(first)
	for scanner.Scan() {
	}
	scanner = bufio.NewScanner(second)
	for scanner.Scan() {
	}
}

// Clean: assigned with `=` to a variable declared before.
func assignedLater(reader io.Reader) {
	var scanner *bufio.Scanner
	scanner = bufio.NewScanner(reader)
	for scanner.Scan() {
	}
}

// Found: declared with var.
func declaredWithVar(reader io.Reader) {
	var scanner = bufio.NewScanner(reader)
	for scanner.Scan() {
	}
}

// Found: declared with var and a type.
func declaredWithType(reader io.Reader) {
	var scanner *bufio.Scanner = bufio.NewScanner(reader)
	for scanner.Scan() {
	}
}

// Found: declared second in a var with two names, and second on the left of a short declaration.
func declaredSecond(reader io.Reader) {
	var count, scanner = 0, bufio.NewScanner(reader)
	for scanner.Scan() {
		count++
	}
	other, second := 0, bufio.NewScanner(reader)
	for second.Scan() {
		other++
	}
	use(count + other)
}

// Clean: assigned to a field.
type holder struct {
	scanner *bufio.Scanner
}

func assignedToField(reader io.Reader) {
	held := holder{}
	held.scanner = bufio.NewScanner(reader)
	for held.scanner.Scan() {
	}
}

// Clean: in a composite literal, and embedded.
type embedding struct {
	*bufio.Scanner
}

func inCompositeLiteral(reader io.Reader) {
	embedded := embedding{bufio.NewScanner(reader)}
	for embedded.Scan() {
	}
	held := holder{scanner: bufio.NewScanner(reader)}
	for held.scanner.Scan() {
	}
}

// Clean: assigned to the blank identifier.
func blank(reader io.Reader) {
	_ = bufio.NewScanner(reader)
	var _ = bufio.NewScanner(reader)
}

// Clean: the call is parenthesized, so it is not the value assigned.
func parenthesizedCall(reader io.Reader) {
	scanner := (bufio.NewScanner(reader))
	for scanner.Scan() {
	}
}

// Found: the function is parenthesized, the call is not.
func parenthesizedFunction(reader io.Reader) {
	scanner := (bufio.NewScanner)(reader)
	for scanner.Scan() {
	}
}

// Found: bufio imported under a name of its own.
func renamedImport(reader io.Reader) {
	scanner := scanning.NewScanner(reader)
	for scanner.Scan() {
	}
}

// Clean: NewScanner called through a variable, whose callee is the variable.
func throughVariable(reader io.Reader) {
	newScanner := bufio.NewScanner
	scanner := newScanner(reader)
	for scanner.Scan() {
	}
}

// Clean: declared in the loop's own initializer, so the loop begins before the scanner.
func declaredInLoopInitializer(reader io.Reader) {
	for scanner := bufio.NewScanner(reader); scanner.Scan(); {
	}
}

// Clean: declared inside the loop that holds its Scan.
func declaredInsideLoop(reader io.Reader) {
	for {
		scanner := bufio.NewScanner(reader)
		if !scanner.Scan() {
			return
		}
	}
}

// Found: declared inside an outer loop, scanned in an inner one.
func declaredInOuterLoop(readers []io.Reader) {
	for _, reader := range readers {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
		}
	}
}

// Found: Scan in a closure inside a loop, since the loop encloses it through the literal.
func closureInLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for {
		func() {
			scanner.Scan()
		}()
	}
}

// Found: a loop inside a closure.
func loopInClosure(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	run := func() {
		for scanner.Scan() {
		}
	}
	run()
}

// Clean: Scan in a closure, in no loop.
func closureWithoutLoop(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scan := func() bool { return scanner.Scan() }
	use(scan)
}

// Found: Scan in a go statement and a labeled loop.
func goStatement(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	var group sync.WaitGroup
outer:
	for {
		group.Go(func() { go scanner.Scan() })
		break outer
	}
}

// Found and clean: an inner scanner shadows the outer one, and each is judged by its own uses.
func shadowed(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
		}
		_ = scanner.Err()
	}
}

// Clean: readers in memory.
func readersInMemory(text string, buffer *bytes.Buffer) {
	first := bufio.NewScanner(strings.NewReader(text))
	for first.Scan() {
	}
	second := bufio.NewScanner(bytes.NewReader(nil))
	for second.Scan() {
	}
	third := bufio.NewScanner(bytes.NewBufferString(text))
	for third.Scan() {
	}
	fourth := bufio.NewScanner(&bytes.Buffer{})
	for fourth.Scan() {
	}
	fifth := bufio.NewScanner(new(strings.Reader))
	for fifth.Scan() {
	}
	sixth := bufio.NewScanner(buffer)
	for sixth.Scan() {
	}
	seventh := bufio.NewScanner((strings.NewReader(text)))
	for seventh.Scan() {
	}
}

// stringReader and stringReaderPointer are aliases, which the judgment sees through.
type (
	stringReader        = strings.Reader
	stringReaderPointer = *strings.Reader
)

// Clean: readers in memory, named through aliases.
func readersThroughAliases(pointer stringReaderPointer) {
	first := bufio.NewScanner(pointer)
	for first.Scan() {
	}
	second := bufio.NewScanner(&stringReader{})
	for second.Scan() {
	}
}

// wrappedReader embeds a reader in memory, and is not one of the types judged infallible.
type wrappedReader struct {
	*strings.Reader
}

// Found: readers that can fail, or are not known not to.
func fallibleReaders(text string, reader io.Reader) {
	first := bufio.NewScanner(os.Stdin)
	for first.Scan() {
	}
	second := bufio.NewScanner(io.LimitReader(strings.NewReader(text), 10))
	for second.Scan() {
	}
	third := bufio.NewScanner(wrappedReader{strings.NewReader(text)})
	for third.Scan() {
	}
	fourth := bufio.NewScanner(&wrappedReader{})
	for fourth.Scan() {
	}
	var inMemory io.Reader = strings.NewReader(text)
	fifth := bufio.NewScanner(inMemory)
	for fifth.Scan() {
	}
	sixth := bufio.NewScanner(nil)
	for sixth.Scan() {
	}
	seventh := bufio.NewScanner(bufio.NewReader(reader))
	for seventh.Scan() {
	}
}

// Found: the reader's type is a type parameter, even one whose type set is a reader in memory.
func generic[Reader io.Reader](reader Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
}

func genericInMemory[Reader interface {
	*strings.Reader
	io.Reader
}](reader Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
	}
}

// container is generic, and its method's scanner is judged as any other.
type container[Element any] struct {
	elements []Element
}

// Found: in a method of a generic type.
func (holding container[Element]) scan(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for range holding.elements {
		scanner.Scan()
	}
}

// Found: package-level scanners, scanned in loops after their declaration.
var packageScanner = bufio.NewScanner(os.Stdin)

func scanPackageScanner() {
	for packageScanner.Scan() {
	}
}

// Clean: a package-level scanner whose Err is called.
var checkedPackageScanner = bufio.NewScanner(os.Stdin)

func scanCheckedPackageScanner() error {
	for checkedPackageScanner.Scan() {
	}
	return checkedPackageScanner.Err()
}

// Clean here: a package-level scanner scanned in a loop that comes before its declaration in this file. Its loop in
// before.go is judged by position across files, and the loader parses a package's files concurrently, so which file
// has the later positions can change from run to run, and with it whether laterScanner is found. Both analyzers
// read the same positions in one checker, so they agree either way.
func scanLaterScanner() {
	for laterScanner.Scan() {
	}
}

var laterScanner = bufio.NewScanner(os.Stdin)
