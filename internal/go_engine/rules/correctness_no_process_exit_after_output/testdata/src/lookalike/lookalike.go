package lookalike

import (
	"log"
	"log/slog"
	"os"
	"runtime"
	"testing"
)

func use(...any) {}

// testing's Fatal stops the test through runtime.Goexit, which runs the deferred calls.
func helper(test *testing.T, file *os.File) {
	defer file.Close()
	test.Fatal("stopping")
}

// runtime.Goexit runs deferred calls before it ends the goroutine.
func goexit(file *os.File) {
	defer file.Close()
	runtime.Goexit()
}

// log's Panic functions panic, and a panic runs deferred calls.
func panics(file *os.File) {
	defer file.Close()
	log.Panicf("stopping %d", 1)
}

// A type of this package with methods named Exit and Fatal is not the standard library's.
type process struct{}

func (process) Exit(code int)        { use(code) }
func (process) Fatal(message string) { use(message) }

func methods(file *os.File) {
	defer file.Close()
	var current process
	current.Exit(1)
	current.Fatal("stopping")
}

// A function of this package named Exit is not os.Exit.
func Exit(code int) { use(code) }

func localExit(file *os.File) {
	defer file.Close()
	Exit(1)
}

// slog's Error logs and returns.
func structured(file *os.File) {
	defer file.Close()
	slog.Error("stopping")
}

// log's Print functions log and return.
func printing(file *os.File) {
	defer file.Close()
	log.Println("stopping")
}
