package flagged

import (
	"bufio"
	"log"
	"os"
	process "os"
)

func use(...any) {}

// A buffered writer flushed by a defer, then an exit on the error path: the report never reaches stdout.
func writeReport(lines []string) {
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	for _, line := range lines {
		if _, writeError := writer.WriteString(line); writeError != nil {
			os.Exit(1) // want `This calls .os\.Exit. on a path that has already deferred a call in this function`
		}
	}
	os.Exit(0) // want `os\.Exit`
}

// Every Fatal of the standard log calls os.Exit.
func openAll(path string) {
	file, openError := os.Open(path)
	if openError != nil {
		log.Fatal(openError)
	}
	defer file.Close()
	if _, seekError := file.Seek(0, 0); seekError != nil {
		log.Fatal(seekError) // want `log\.Fatal\b`
	}
	if _, statError := file.Stat(); statError != nil {
		log.Fatalf("reading %s: %v", path, statError) // want `log\.Fatalf`
	}
	log.Fatalln("done") // want `log\.Fatalln`
}

// A *log.Logger's Fatal methods exit the same way, called as a method or through a method expression.
func withLogger(logger *log.Logger, file *os.File, step int) {
	defer file.Close()
	switch step {
	case 0:
		logger.Fatal("stopping") // want `Logger\.Fatal\b`
	case 1:
		logger.Fatalf("stopping %d", 1) // want `Logger\.Fatalf`
	case 2:
		logger.Fatalln("stopping") // want `Logger\.Fatalln`
	default:
		(*log.Logger).Fatal(logger, "again") // want `Logger\.Fatal\b`
	}
}

// A defer on one branch is pending where the branches join.
func maybeDeferred(verbose bool) {
	if verbose {
		defer use("summary")
	}
	os.Exit(0) // want `os\.Exit`
}

// A defer in a loop's body is pending at an exit the next turn meets, though it is written below the exit.
func eachFile(paths []string) {
	for _, path := range paths {
		if path == "" {
			os.Exit(2) // want `os\.Exit`
		}
		file, openError := os.Open(path)
		if openError != nil {
			continue
		}
		defer file.Close()
	}
}

// A function literal is judged on its own defers.
func literal() {
	run := func(file *os.File) {
		defer file.Close()
		os.Exit(1) // want `os\.Exit`
	}
	use(run)
}

// A deferred exit runs before every call deferred ahead of it, and skips them.
func deferredExit(code int) {
	defer use("cleanup")
	defer os.Exit(code) // want `os\.Exit`
}

// A case that defers reaches the exit after the switch.
func command(name string) {
	switch name {
	case "serve":
		defer use("shutdown")
	case "help":
		use("usage")
	}
	os.Exit(0) // want `os\.Exit`
}

// A defer in a select's case reaches the exit after it.
func selected(done chan struct{}) {
	select {
	case <-done:
		defer use("drain")
	default:
	}
	os.Exit(0) // want `os\.Exit`
}

// A generic function, a parenthesized callee and a renamed import resolve to the same os.Exit.
func generic[T any](items []T) {
	defer use(items)
	if len(items) == 0 {
		(os.Exit)(1) // want `os\.Exit`
	}
	process.Exit(1) // want `os\.Exit`
}

// The testing.M shape: the teardown deferred above os.Exit(m.Run()) never runs.
func mainShape(runTests func() int) {
	defer use("teardown")
	os.Exit(runTests()) // want `os\.Exit`
}

// A labeled loop that defers and then breaks out to the exit.
func labeled(rows [][]int) {
outer:
	for _, row := range rows {
		for _, cell := range row {
			if cell < 0 {
				defer use(cell)
				break outer
			}
		}
	}
	os.Exit(0) // want `os\.Exit`
}

// A method's defer counts as a function's does.
type server struct{ log *os.File }

func (server *server) stop() {
	defer server.log.Close()
	os.Exit(0) // want `os\.Exit`
}
