package main

import (
	"fmt"
	"log"
	"os"
)

// package main is the command, and its standard output is the program's interface.
func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: command <name>")
	}
	fmt.Println("hello", os.Args[1])
	fmt.Printf("%d arguments\n", len(os.Args))
	println("done")
	log.Println("finished")
}
