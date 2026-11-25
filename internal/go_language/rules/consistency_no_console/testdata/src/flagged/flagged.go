package flagged

import (
	format "fmt"
	"log"
)

// Each of fmt's three writes to standard output.
func reportProgress(done int, total int) {
	format.Print("progress ")              // want `Do not call 'fmt\.Print'\. Outside package main`
	format.Printf("%d of %d", done, total) // want `Do not call 'fmt\.Printf'`
	format.Println()                       // want `Do not call 'fmt\.Println'`
}

// The builtins write to standard error.
func trace(step string) {
	print(step)              // want `Do not call 'print'`
	println("étape ✓", step) // want `Do not call 'println'`
}

// The standard logger's functions, the ones that end the process or unwind it included.
func logEverything(reason string, broken bool) {
	log.Print(reason)        // want `Do not call 'log\.Print'`
	log.Printf("%s", reason) // want `Do not call 'log\.Printf'`
	log.Println(reason)      // want `Do not call 'log\.Println'`
	if broken {
		log.Panic(reason) // want `Do not call 'log\.Panic'`
	}
	if broken {
		log.Panicf("%s", reason) // want `Do not call 'log\.Panicf'`
	}
	if broken {
		log.Panicln(reason) // want `Do not call 'log\.Panicln'`
	}
	if broken {
		log.Fatal(reason) // want `Do not call 'log\.Fatal'`
	}
	if broken {
		log.Fatalf("%s", reason) // want `Do not call 'log\.Fatalf'`
	}
	if broken {
		log.Fatalln(reason) // want `Do not call 'log\.Fatalln'`
	}
}

func apply(write func(...any) (int, error), value any) {
	_, _ = write(value)
}

// Handed on as a value or kept in a variable, the function still writes to standard output when it is called.
func aliased(value any) {
	apply(format.Println, value) // want `Do not call 'fmt\.Println'`
	write := format.Print        // want `Do not call 'fmt\.Print'`
	_, _ = write(value)
	(format.Println)(value) // want `Do not call 'fmt\.Println'`
}

// A generic function's body is judged as any other.
func show[Value any](value Value) {
	format.Println(value) // want `Do not call 'fmt\.Println'`
}

// A method's body too.
type reporter struct{ name string }

func (reporter *reporter) announce() {
	log.Printf("%s started", reporter.name) // want `Do not call 'log\.Printf'`
}
