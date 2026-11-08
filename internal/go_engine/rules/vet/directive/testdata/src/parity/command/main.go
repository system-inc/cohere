// Package main's header is where //go:debug belongs.
//go:debug panicnil=1
//go:debug http2client=0

//go:debug http2server=0
package main //go:debug panicnil=1 adjoins the clause, past the header: only valid before package declaration

//go:debug panicnil=1

func main() {}
