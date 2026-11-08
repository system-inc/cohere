// Package parity holds the printf-style calls SA1006 judges, flagged and left alone, for the port and the original to
// judge side by side: every function the original names, called directly, through promoted and interface methods and
// through parentheses, formats of every shape, and the calls it leaves alone.
package parity

import (
	"fmt"
	"log"
	"os"
	"testing"
)

type wrapper struct {
	logger  *log.Logger
	loggers []*log.Logger
}

func format() string { return "" }

func pair() (string, int) { return "", 0 }

func flagged(text string, holder wrapper, test *testing.T, benchmark *testing.B, fuzz *testing.F, tb testing.TB) {
	fmt.Printf(text)
	_ = fmt.Sprintf(format())
	_ = fmt.Errorf(text)
	log.Printf(text)
	log.Fatalf(format())
	log.Panicf(text)
	fmt.Fprintf(os.Stderr, text)
	fmt.Fprintf(os.Stderr, (text))
	fmt.Printf((format()))
	holder.logger.Printf(text)
	holder.loggers[0].Printf(text)
	log.Default().Printf(text)
	test.Logf(text)
	benchmark.Errorf(text)
	fuzz.Skipf(text)
	tb.Fatalf(format())
	(fmt.Printf)(text)
	fmt.Printf(func() string { return "" }())
}

func spared(text string, values []string, test *testing.T) {
	fmt.Printf("text")
	fmt.Printf(text, 1)
	fmt.Printf(pair())
	fmt.Printf(values[0])
	fmt.Printf(text + "")
	fmt.Printf(*&text)
	fmt.Print(text)
	fmt.Println(text)
	log.Print(text)
	fmt.Fprintf(os.Stderr, "text")
	fmt.Fprintf(os.Stderr, text, 1)
	fmt.Fprint(os.Stderr, text)
	test.Log(text)
	printf := fmt.Printf
	printf(text)
	fmt.Sprint(fmt.Sprintf)
}
