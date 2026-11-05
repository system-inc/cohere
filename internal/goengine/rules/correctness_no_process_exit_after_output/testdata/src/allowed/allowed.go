package allowed

import (
	"bufio"
	"log"
	"os"
)

func use(...any) {}

// The fix the message asks for: run holds the defers and returns a code, and main exits once they have run.
func run() int {
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	if _, writeError := writer.WriteString("report\n"); writeError != nil {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run())
}

// No defer anywhere in the function.
func noDefer(path string) {
	if _, statError := os.Stat(path); statError != nil {
		log.Fatal(statError)
	}
	os.Exit(0)
}

// The exit comes before the defer, so nothing is pending when it runs.
func exitBeforeDefer(path string) {
	file, openError := os.Open(path)
	if openError != nil {
		log.Fatalf("opening %s: %v", path, openError)
	}
	defer file.Close()
	use(file)
}

// A defer on a branch that returns never reaches the exit after it.
func deferThenReturn(flag bool) {
	if flag {
		defer use("returning")
		return
	}
	os.Exit(1)
}

// A defer on a branch that panics never reaches the exit after it.
func deferThenPanic(flag bool) {
	if flag {
		defer use("panicking")
		panic("stopping")
	}
	os.Exit(1)
}

// fatal always exits, so the branch that defers and then calls it never reaches the exit below.
func fatal(message string) {
	log.Print(message)
	os.Exit(1)
}

func deferThenHelper(flag bool) {
	if flag {
		defer use("helper")
		fatal("stopping")
	}
	os.Exit(1)
}

// A defer nobody can reach is never pending, though its block leads to the exit.
func deadDefer(flag bool) {
	if flag {
		os.Exit(2)
		defer use("dead")
	}
	os.Exit(1)
}

// An exit inside a function literal with no defer of its own is the literal's, not the function's around it.
func literalExit(file *os.File) {
	defer file.Close()
	stop := func() {
		os.Exit(1)
	}
	use(stop)
}

// The idiom that exits last on purpose: deferred first, it runs after every other deferred call.
func exitLast(code int) {
	defer func() {
		os.Exit(code)
	}()
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
}

// A defer inside a function literal is the literal's, so it is not pending at the exit around it.
func deferInLiteral() {
	cleanup := func(file *os.File) {
		defer file.Close()
	}
	use(cleanup)
	os.Exit(0)
}

// An exit started on another goroutine runs at a moment this function's graph cannot place, so it is not judged.
func goroutineExit(file *os.File) {
	defer file.Close()
	go os.Exit(1)
}

// An exit through a helper is not judged: only a direct call to the standard library's exit is.
func helperExit(file *os.File) {
	defer file.Close()
	fatal("stopping")
}

// An exit through a function value has no static callee, so it is not judged.
func exitThroughValue(file *os.File) {
	exit := os.Exit
	defer file.Close()
	exit(1)
}
