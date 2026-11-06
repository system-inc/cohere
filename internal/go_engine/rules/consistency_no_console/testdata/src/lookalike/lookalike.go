package lookalike

import (
	"strings"

	"lookalike/fmt"
)

// Another package called fmt is not the standard library's, and its Println is its own.
func collect(name string) []any {
	return fmt.Println("collected", name)
}

// A printer's print parameter is a closure the caller built, the shape cohere's formatters use.
func printSelection(print func(name string, value any) string) string {
	return print("selectionSet", nil) + print("name", nil)
}

// A local println shadows the builtin.
func shadowed(builder *strings.Builder) {
	println := func(text string) { builder.WriteString(text + "\n") }
	println("into the builder")
}

// A method named Println, and a field of function type named Print, belong to their own types.
type document struct {
	lines []string
	Print func(string)
}

func (document *document) Println(line string) {
	document.lines = append(document.lines, line)
}

func useDocument(document *document) {
	document.Println("a line")
	document.Print = document.Println
	document.Print("another line")
}

// A local value named fmt, and the words fmt.Println( in a string or a comment, are not the function.
func mentions() string {
	fmt := document{}
	fmt.Println("not the standard library")
	return "call fmt.Println(value) or log.Fatal(reason) only in package main"
}

// A function this package declares named print is its own.
func print(values ...string) string {
	return strings.Join(values, " ")
}

func usePrint() string {
	return print("joined", "words")
}
